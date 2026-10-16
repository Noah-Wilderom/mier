// Package typesafe contains the bounded, read-only TypeSafe System One client.
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}
type Usage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}
type Client struct {
	key      string
	endpoint string
	http     *http.Client
}

var modelName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,199}$`)

func ValidModel(model string) bool { return modelName.MatchString(model) }
func New(key string) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return nil, errors.New("Enter a valid TypeSafe API key")
	}
	return &Client{key: key, endpoint: "https://api.typesafe.ai", http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("TypeSafe redirect refused") }}}, nil
}

// NewWithHTTPClient injects a trusted transport while retaining the fixed API
// origin. Used by native integration tests; the app has no custom endpoint UI.
func NewWithHTTPClient(key string, transport *http.Client) (*Client, error) {
	c, err := New(key)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		copy := *transport
		copy.CheckRedirect = c.http.CheckRedirect
		c.http = &copy
	}
	return c, nil
}
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return errors.New("Could not encode the TypeSafe request")
		}
	}
	if len(body) > 96*1024 {
		return errors.New("Prediction context exceeds the 96 KiB request limit; select a smaller scope")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("Could not create the TypeSafe request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("TypeSafe could not be reached. Check your connection and retry")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		switch res.StatusCode {
		case 401, 403:
			return errors.New("TypeSafe rejected this API key or model access. Reconnect in Settings")
		case 429:
			return errors.New("TypeSafe rate limit reached. Wait before retrying; no automatic retry was made")
		case 422:
			return errors.New("TypeSafe rejected the model or assessment format. Check the selected model in Settings")
		}
		return fmt.Errorf("TypeSafe request failed (HTTP %d). Retry later", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return errors.New("TypeSafe returned an unreadable or oversized response")
	}
	if json.Unmarshal(data, output) != nil {
		return errors.New("TypeSafe returned an invalid response")
	}
	return nil
}
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var result struct {
		Models []Model `json:"models"`
	}
	if err := c.request(ctx, http.MethodGet, "/v1/models", nil, &result); err != nil {
		return nil, err
	}
	if len(result.Models) == 0 || len(result.Models) > 100 {
		return nil, errors.New("TypeSafe returned an invalid model catalog")
	}
	for _, m := range result.Models {
		if !ValidModel(m.Name) || len(m.Description) > 2000 {
			return nil, errors.New("TypeSafe returned an invalid model catalog")
		}
	}
	return result.Models, nil
}
func (c *Client) Evaluate(ctx context.Context, request Request) (Response, error) {
	if !ValidModel(request.Model) || len(request.Questions) == 0 || len(request.Questions) > 200 {
		return Response{}, errors.New("Choose a model and between 1 and 200 assessment questions")
	}
	var result Response
	if err := c.request(ctx, http.MethodPost, "/v1/systemone", request, &result); err != nil {
		return Response{}, err
	}
	if err := Validate(request, result); err != nil {
		return Response{}, err
	}
	return result, nil
}
func probability(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
func Validate(request Request, result Response) error {
	invalid := errors.New("TypeSafe returned answers that do not match this assessment. No suggestions were applied")
	if !ValidModel(result.Model) || len(result.Answers) != len(request.Questions) || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil || *result.Usage.InputTokens < 0 || *result.Usage.OutputTokens < 0 {
		return invalid
	}
	for name, q := range request.Questions {
		a, ok := result.Answers[name]
		if !ok || a.Type != q.Type {
			return invalid
		}
		if q.Type == "noul" {
			if a.Noul == nil || !probability(*a.Noul) {
				return invalid
			}
			continue
		}
		if a.Confidence == nil || !probability(*a.Confidence) {
			return invalid
		}
		keys := map[string]bool{}
		switch q.Type {
		case "choice":
			options, ok := q.Criteria.(map[string]string)
			if !ok {
				return invalid
			}
			for k := range options {
				keys[k] = true
			}
			if !keys[a.Choice] {
				return invalid
			}
		case "score":
			levels, ok := q.Criteria.([]string)
			if !ok || a.Score == nil || math.IsNaN(*a.Score) || *a.Score < 0 || *a.Score > float64(len(levels)-1) {
				return invalid
			}
			for i := range levels {
				keys[fmt.Sprint(i)] = true
			}
		default:
			return invalid
		}
		if len(a.Probabilities) != len(keys) {
			return invalid
		}
		sum, expected := 0.0, 0.0
		for k, p := range a.Probabilities {
			if !keys[k] || !probability(p) {
				return invalid
			}
			sum += p
			if q.Type == "score" {
				var level int
				fmt.Sscan(k, &level)
				expected += float64(level) * p
			}
			if q.Type == "choice" && p > a.Probabilities[a.Choice]+0.001 {
				return invalid
			}
		}
		if math.Abs(sum-1) > 0.025 || q.Type == "score" && math.Abs(expected-*a.Score) > 0.05 {
			return invalid
		}
	}
	return nil
}
