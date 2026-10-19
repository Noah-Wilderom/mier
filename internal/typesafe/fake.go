package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const FakeModel = "jev-local-test"

const fakeKey = "mier-local-test-only"

// NewFake exercises the normal JSON client and answer validation without a
// network connection or a real API key. Answers are fixtures, not predictions.
func NewFake(scenario string) (*Client, error) {
	handler, err := FakeHandler(scenario)
	if err != nil {
		return nil, err
	}
	client, _ := NewWithHTTPClient(fakeKey, &http.Client{Transport: fakeTransport{handler}})
	client.endpoint = "http://jev.local.invalid"
	return client, nil
}

// FakeHandler can also run behind httptest.NewServer to verify the HTTP wire
// protocol. It accepts only synthetic credentials and never logs request bodies.
func FakeHandler(scenario string) (http.Handler, error) {
	switch scenario {
	case "", "success", "rate-limit", "unavailable", "invalid-response", "slow":
	default:
		return nil, fmt.Errorf("Unknown local Jev scenario %q. Use success, rate-limit, unavailable, invalid-response or slow", scenario)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+fakeKey {
			http.Error(w, `{"detail":"Use the local test credential"}`, http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]any{"models": []Model{{Name: FakeModel, Description: "Local deterministic fixtures; no inference or charge", ReleaseDate: "2026-10-19"}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		var input struct {
			Model     string          `json:"model"`
			State     json.RawMessage `json:"state"`
			Questions map[string]struct {
				Type     string          `json:"type"`
				Criteria json.RawMessage `json:"criteria"`
			} `json:"questions"`
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 96*1024+1))
		if err != nil || len(data) > 96*1024 || json.Unmarshal(data, &input) != nil || input.Model != FakeModel || len(input.State) == 0 || string(input.State) == "null" || len(input.Questions) == 0 || len(input.Questions) > 200 {
			http.Error(w, `{"detail":"Invalid local assessment request"}`, http.StatusUnprocessableEntity)
			return
		}
		delay := 150 * time.Millisecond
		if scenario == "slow" {
			delay = 2 * time.Second
		}
		if err := fakeWait(r.Context(), delay); err != nil {
			return
		}
		switch scenario {
		case "rate-limit":
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"detail":"Synthetic rate limit"}`)
			return
		case "unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"detail":"Synthetic provider outage"}`)
			return
		case "invalid-response":
			io.WriteString(w, `{"model":"jev-local-test","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`)
			return
		}
		answers := map[string]any{}
		for name, question := range input.Questions {
			switch question.Type {
			case "noul":
				answers[name] = map[string]any{"type": "noul", "noul": .5}
			case "choice":
				var options map[string]string
				if json.Unmarshal(question.Criteria, &options) != nil || len(options) == 0 || len(options) > 255 {
					http.Error(w, `{"detail":"Invalid choice criteria"}`, http.StatusUnprocessableEntity)
					return
				}
				keys := make([]string, 0, len(options))
				probabilities := map[string]float64{}
				for key := range options {
					keys = append(keys, key)
					probabilities[key] = 0
				}
				sort.Strings(keys)
				choice := keys[0]
				for _, preferred := range []string{"needs_evidence", "verify", "manual", "unknown", "general"} {
					if _, ok := options[preferred]; ok {
						choice = preferred
						break
					}
				}
				probabilities[choice] = 1
				answers[name] = map[string]any{"type": "choice", "choice": choice, "confidence": 0, "probabilities": probabilities}
			case "score":
				var levels []string
				if json.Unmarshal(question.Criteria, &levels) != nil || len(levels) == 0 || len(levels) > 10 {
					http.Error(w, `{"detail":"Invalid score criteria"}`, http.StatusUnprocessableEntity)
					return
				}
				probabilities, legend := map[string]float64{}, map[string]string{}
				for i, level := range levels {
					key := strconv.Itoa(i)
					legend[key] = level
					probabilities[key] = 0
				}
				probabilities["0"] = 1
				score := 0.0
				if len(levels) > 1 {
					probabilities["0"], probabilities[strconv.Itoa(len(levels)-1)] = .5, .5
					score = float64(len(levels)-1) / 2
				}
				answers[name] = map[string]any{"type": "score", "score": score, "confidence": 0, "legend": legend, "probabilities": probabilities}
			default:
				http.Error(w, `{"detail":"Unsupported local question type"}`, http.StatusUnprocessableEntity)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": FakeModel, "answers": answers, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}})
	}), nil
}

func fakeWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

type fakeTransport struct{ handler http.Handler }

func (t fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		defer request.Body.Close()
	}
	writer := &fakeResponseWriter{header: http.Header{}, status: http.StatusOK}
	t.handler.ServeHTTP(writer, request)
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: writer.status, Header: writer.header, Body: io.NopCloser(bytes.NewReader(writer.body.Bytes())), Request: request}, nil
}

type fakeResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *fakeResponseWriter) Header() http.Header            { return w.header }
func (w *fakeResponseWriter) WriteHeader(status int)         { w.status = status }
func (w *fakeResponseWriter) Write(data []byte) (int, error) { return w.body.Write(data) }
