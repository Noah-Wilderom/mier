package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func num(n float64) *float64 { return &n }
func count(n int64) *int64   { return &n }
func validRequest() Request {
	return Request{Model: "jev-1.13.0", State: map[string]string{"patch": "source"}, Questions: map[string]Question{"support": {Type: "noul", Instructions: "Is it supported?"}, "route": {Type: "choice", Criteria: map[string]string{"manual": "Inspect", "agent": "Investigate"}}, "attention": {Type: "score", Criteria: []string{"low", "normal", "high"}}}}
}
func validResponse() Response {
	return Response{Model: "jev-1.13.0", Answers: map[string]Answer{"support": {Type: "noul", Noul: num(0)}, "route": {Type: "choice", Choice: "manual", Confidence: num(.5), Probabilities: map[string]float64{"manual": .75, "agent": .25}}, "attention": {Type: "score", Score: num(1.6), Confidence: num(.4), Probabilities: map[string]float64{"0": .1, "1": .2, "2": .7}}}, Usage: Usage{InputTokens: count(200), OutputTokens: count(0)}}
}
func TestValidatesCompleteTypedAnswersAndReportedZero(t *testing.T) {
	if err := Validate(validRequest(), validResponse()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Response){"missing": func(r *Response) { delete(r.Answers, "support") }, "extra": func(r *Response) { r.Answers["extra"] = Answer{Type: "noul", Noul: num(.5)} }, "probability": func(r *Response) { r.Answers["support"] = Answer{Type: "noul", Noul: num(2)} }, "null": func(r *Response) { r.Answers["support"] = Answer{Type: "noul"} }, "wrong option": func(r *Response) { a := r.Answers["route"]; a.Choice = "publish"; r.Answers["route"] = a }, "sum": func(r *Response) { r.Answers["route"].Probabilities["agent"] = .9 }, "score": func(r *Response) { a := r.Answers["attention"]; a.Score = num(.1); r.Answers["attention"] = a }, "usage": func(r *Response) { r.Usage.InputTokens = nil }, "type": func(r *Response) { r.Answers["route"] = Answer{Type: "noul", Noul: num(.8)} }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validResponse()
			mutate(&r)
			if Validate(validRequest(), r) == nil {
				t.Fatal("accepted malformed result")
			}
		})
	}
}
func TestSystemOneBatchesNamedQuestionsAndRedactsFailures(t *testing.T) {
	status := 200
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("missing authorization")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/systemone" {
			t.Error("wrong route")
		}
		var request Request
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Questions) != 3 {
			t.Error("questions were not batched")
		}
		w.WriteHeader(status)
		if status == 200 {
			json.NewEncoder(w).Encode(validResponse())
		} else {
			w.Write([]byte(`{"error":"private-key secret source"}`))
		}
	}))
	defer server.Close()
	c, _ := New("private-key")
	c.endpoint = server.URL
	if _, err := c.Evaluate(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	status = 429
	if _, err := c.Evaluate(context.Background(), validRequest()); err == nil || strings.Contains(err.Error(), "private-key") {
		t.Fatal("unsafe error", err)
	}
	if calls != 2 {
		t.Fatal("unexpected retries", calls)
	}
}
func TestCancellationAndResponseLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c, _ := New("private-key")
	c.endpoint = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Models(ctx); err == nil {
		t.Fatal("cancelled call succeeded")
	}
	request := validRequest()
	request.State = strings.Repeat("s", 100*1024)
	if _, err := c.Evaluate(context.Background(), request); err == nil {
		t.Fatal("oversized source accepted")
	}
}
func TestCredentialRedirectIsRefused(t *testing.T) {
	forwarded := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer server.Close()
	c, _ := New("private-key")
	c.endpoint = server.URL
	if _, err := c.Models(context.Background()); err == nil || forwarded {
		t.Fatal("credential redirect followed")
	}
}
