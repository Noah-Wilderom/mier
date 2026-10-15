package main

import (
	"context"
	"encoding/json"
	"errors"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIIntensityValidationPrecedesProviderAccess(t *testing.T) {
	s := &AIService{}
	if _, err := s.StartReview(context.Background(), AIReviewRequest{Provider: "codex", Intensity: "extreme"}); err == nil || !strings.Contains(err.Error(), "intensity") {
		t.Fatal("invalid intensity reached provider setup", err)
	}
	if err := s.SavePreferences(AIPreferences{Provider: "codex", Intensity: "extreme"}); err == nil {
		t.Fatal("unsupported preference saved")
	}
}

func TestAIConnectionCanBeRestoredAndRevokedWithoutExposingProviderSessions(t *testing.T) {
	s := &AIService{tasks: map[string]*aiTask{}}
	run := reviewagent.NewRun("external", "", reviewagent.Context{Key: "fixture"}, nil)
	bridge, err := reviewagent.StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	id := run.Snapshot().ID
	s.tasks[id] = &aiTask{run: run, bridge: bridge}
	connection, err := s.GetMCPConnection(id)
	if err != nil || connection.Token != bridge.Token || connection.URL != bridge.URL {
		t.Fatal("connection did not restore")
	}
	snapshot, err := s.GetReview(id)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), bridge.Token) {
		t.Fatal("polling exposed bearer")
	}
	if err := s.CancelReview(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMCPConnection(id); err == nil {
		t.Fatal("cancelled connection restored")
	}
	internal := reviewagent.NewRun("codex", "", reviewagent.Context{}, nil)
	s.tasks["builtin"] = &aiTask{run: internal, bridge: bridge}
	if _, err := s.GetMCPConnection("builtin"); err == nil {
		t.Fatal("provider MCP bearer exposed")
	}
}

func TestAILoginCompletionIsCorrelatedAndRedactsProviderErrors(t *testing.T) {
	s := &AIService{loginStates: map[string]AILoginStatus{"current": {State: "waiting"}}}
	s.authNotification("account/login/completed", json.RawMessage(`{"loginId":"old","success":true}`))
	status, _ := s.GetCodexLoginStatus("current")
	if status.State != "waiting" {
		t.Fatal("old completion changed current login")
	}
	s.authNotification("account/login/completed", json.RawMessage(`{"loginId":"current","success":false,"error":"Invalid Bearer ghp_fixtureSecret12345678"}`))
	status, err := s.GetCodexLoginStatus("current")
	if err != nil || status.State != "failed" || strings.Contains(status.Error, "fixtureSecret") {
		t.Fatal("failure missing or secret exposed", status, err)
	}
	if _, err := s.GetCodexLoginStatus("missing"); err == nil {
		t.Fatal("unknown login accepted")
	}
}

func TestAISettingsRejectCommandArgumentsAndRecoverDamagedPreferences(t *testing.T) {
	s := &AIService{configPath: filepath.Join(t.TempDir(), "ai.json"), preferencesErr: errors.New("damaged")}
	if err := s.SavePreferences(AIPreferences{Provider: "codex", CodexPath: "codex --yolo"}); err == nil {
		t.Fatal("relative command accepted")
	}
	if err := s.SavePreferences(AIPreferences{Provider: "copilot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPreferences(); err != nil {
		t.Fatal("preferences did not recover", err)
	}
	data, err := os.ReadFile(s.configPath)
	if err != nil || !strings.Contains(string(data), `"copilot"`) {
		t.Fatal("preferences not saved", err)
	}
}
