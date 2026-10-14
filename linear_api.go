package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type linearAPIError struct{ status int }

func (e *linearAPIError) Error() string {
	switch e.status {
	case 401:
		return "Linear authorization expired. Authorize again in Settings."
	case 403:
		return "Your Linear account does not have permission to view this information."
	case 404:
		return "This Linear ticket was not found, or your account cannot view it."
	case 429:
		return "Linear is limiting requests. Wait a moment before retrying."
	default:
		return "Linear could not complete the request. Check your connection and retry."
	}
}

func validLinearWorkspace(workspace LinearWorkspace) bool {
	u, err := url.Parse(workspace.URL)
	return err == nil && u.Scheme == "https" && u.Host == "linear.app" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && jiraSiteID.MatchString(strings.TrimPrefix(u.Path, "/")) && jiraSiteID.MatchString(workspace.ID) && workspace.Name != "" && len(workspace.Name) <= 1024
}

func (s *LinearService) linearRequest(ctx context.Context, path, token string, body any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	cooldown := s.retryAt.After(time.Now())
	s.mu.Unlock()
	if cooldown {
		return nil, &linearAPIError{status: 429}
	}
	var data []byte
	contentType := "application/json"
	if values, ok := body.(url.Values); ok {
		data, contentType = []byte(values.Encode()), "application/x-www-form-urlencoded"
	} else {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("Linear request could not be created")
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.linear.app"+path, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Linear request could not be created")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Linear could not be reached. Check your connection and retry.")
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("Linear response could not be read")
	}
	if len(data) > 2*1024*1024 {
		return nil, errors.New("Linear returned too much data to display. Open the ticket in Linear.")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
		if err != nil || seconds < 1 || seconds > 3600 {
			seconds = 30
		}
		s.mu.Lock()
		s.retryAt = time.Now().Add(time.Duration(seconds) * time.Second)
		s.mu.Unlock()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return data, &linearAPIError{status: response.StatusCode}
	}
	return data, nil
}

func (s *LinearService) linearGraphQLRaw(ctx context.Context, token, query string, variables any, out any) error {
	data, requestErr := s.linearRequest(ctx, "/graphql", token, map[string]any{"query": query, "variables": variables})
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Extensions struct{ Code, Type string } `json:"extensions"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		if requestErr != nil {
			return requestErr
		}
		return errors.New("Linear returned an invalid response")
	}
	if len(envelope.Errors) > 0 {
		// Provider error messages may contain request data. Map documented codes
		// to bounded messages instead of forwarding untrusted response bodies.
		for _, item := range envelope.Errors {
			code := strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(item.Extensions.Code + " " + item.Extensions.Type))
			switch {
			case strings.Contains(code, "RATELIMITED") || strings.Contains(code, "RATE_LIMITED"):
				s.mu.Lock()
				if !s.retryAt.After(time.Now()) {
					s.retryAt = time.Now().Add(30 * time.Second)
				}
				s.mu.Unlock()
				return &linearAPIError{status: 429}
			case strings.Contains(code, "AUTHENTICATION_ERROR") || strings.Contains(code, "UNAUTHENTICATED"):
				return &linearAPIError{status: 401}
			case strings.Contains(code, "FORBIDDEN") || strings.Contains(code, "PERMISSION"):
				return &linearAPIError{status: 403}
			case strings.Contains(code, "NOT_FOUND"):
				return &linearAPIError{status: 404}
			}
		}
		if requestErr != nil {
			return requestErr
		}
		return errors.New("Linear could not read this ticket information. Retry or open it in Linear.")
	}
	if requestErr != nil {
		return requestErr
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return errors.New("Linear returned an invalid response")
	}
	return nil
}

func (s *LinearService) linearGraphQL(ctx context.Context, query string, variables any, out any) error {
	token, err := s.linearAccessToken(ctx, "")
	if err != nil {
		return err
	}
	err = s.linearGraphQLRaw(ctx, token, query, variables, out)
	var failure *linearAPIError
	if errors.As(err, &failure) && failure.status == 401 {
		token, err = s.linearAccessToken(ctx, token)
		if err != nil {
			return err
		}
		return s.linearGraphQLRaw(ctx, token, query, variables, out)
	}
	return err
}
