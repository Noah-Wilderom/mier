package typesafe

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFakeProviderExercisesTheHTTPClientAndAllAnswerTypes(t *testing.T) {
	handler, err := FakeHandler("success")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, _ := New(fakeKey)
	client.endpoint = server.URL
	models, err := client.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].Name != FakeModel {
		t.Fatal(models, err)
	}
	request := validRequest()
	request.Model = models[0].Name
	first, err := client.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if *first.Answers["support"].Noul != .5 || first.Answers["route"].Choice != "manual" || *first.Answers["attention"].Score != 1 || *first.Usage.InputTokens != 0 || *first.Usage.OutputTokens != 0 {
		t.Fatal("unexpected synthetic answers", first)
	}
	second, err := client.Evaluate(context.Background(), request)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("fixture answers are not deterministic", first, second, err)
	}
	request.Model = "jev-1.13.0"
	if _, err := client.Evaluate(context.Background(), request); err == nil {
		t.Fatal("unavailable model was silently accepted")
	}
	client.key = "a-real-key-must-not-be-used-here"
	if _, err := client.Models(context.Background()); err == nil || strings.Contains(err.Error(), client.key) {
		t.Fatal("fake accepted real credentials or leaked them", err)
	}
}

func TestFakeProviderFailureScenariosAndCancellation(t *testing.T) {
	for scenario, message := range map[string]string{"rate-limit": "rate limit", "unavailable": "503", "invalid-response": "answers that do not match"} {
		t.Run(scenario, func(t *testing.T) {
			client, err := NewFake(scenario)
			if err != nil {
				t.Fatal(err)
			}
			request := validRequest()
			request.Model = FakeModel
			if _, err := client.Evaluate(context.Background(), request); err == nil || !strings.Contains(err.Error(), message) {
				t.Fatal("expected sanitized failure", err)
			}
		})
	}
	client, _ := NewFake("slow")
	request := validRequest()
	request.Model = FakeModel
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.Evaluate(ctx, request); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("local provider did not honor cancellation", err)
	}
	if _, err := NewFake("typo"); err == nil {
		t.Fatal("unknown scenario silently fell back to success")
	}
}
