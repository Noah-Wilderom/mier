package main

import (
	"fmt"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewConversationRestoresWithoutCapabilitiesAndScopesFollowups(t *testing.T) {
	service := NewAIService(nil, nil)
	service.configPath = filepath.Join(t.TempDir(), "ai.json")
	defer service.ServiceShutdown()
	request := AIReviewRequest{Host: "github.com", Owner: "owner", Repository: "repo", PullRequest: 12, HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Instructions: "Inspect the retry logic"}
	scope := reviewagent.Context{Key: "repo#12", HeadSHA: request.HeadSHA, Focus: request.Instructions, Files: []reviewagent.File{{Path: "app.go", Patch: "@@ -1 +1 @@\n-old\n+new\n"}}}
	run := reviewagent.NewRun("codex", "fixture", scope, nil)
	run.MarkContextRead()
	if _, err := run.AddFinding(reviewagent.Finding{Path: "app.go", Line: 1, Side: "new", Severity: "bug", Body: "Retry executes the write twice."}); err != nil {
		t.Fatal(err)
	}
	run.Finish("completed", "One retry issue needs review", "")
	if err := service.persistAITask(&aiTask{run: run, reviewRequest: &request}); err != nil {
		t.Fatal(err)
	}
	restarted := NewAIService(nil, nil)
	restarted.configPath = service.configPath
	defer restarted.ServiceShutdown()
	restored, err := restarted.GetReview(run.Snapshot().ID)
	if err != nil || len(restored.Findings) != 1 || restored.Instructions != request.Instructions {
		t.Fatalf("restore: %#v %v", restored, err)
	}
	if _, err = restarted.GetMCPConnection(restored.ID); err == nil {
		t.Fatal("restoring history recreated an MCP capability")
	}
	request.PreviousReviewID = restored.ID
	request.HeadSHA = strings.Repeat("c", 40)
	prior, err := restarted.previousReviewContext(request)
	if err != nil || len(prior) != 1 || prior[0].HeadSHA == request.HeadSHA || len(prior[0].Findings) != 1 {
		t.Fatalf("follow-up history: %#v %v", prior, err)
	}
	request.PullRequest = 13
	if _, err = restarted.previousReviewContext(request); err == nil {
		t.Fatal("cross-PR conversation accepted")
	}
	interrupted := reviewagent.NewRun("codex", "fixture", scope, nil)
	if err = service.persistAITask(&aiTask{run: interrupted, reviewRequest: &request}); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.tasks[interrupted.Snapshot().ID] = &aiTask{run: interrupted, reviewRequest: &request}
	service.mu.Unlock()
	live, err := service.ReviewHistory(AIReviewHistoryRequest{Host: request.Host, Owner: request.Owner, Repository: request.Repository, PullRequest: request.PullRequest})
	if err != nil || len(live) != 1 || live[0].State == "cancelled" {
		t.Fatalf("live review was treated as interrupted: %#v %v", live, err)
	}
	service.mu.Lock()
	delete(service.tasks, interrupted.Snapshot().ID)
	service.mu.Unlock()
	stopped, err := restarted.GetReview(interrupted.Snapshot().ID)
	if err != nil || stopped.State != "cancelled" || !strings.Contains(stopped.Error, "closed") {
		t.Fatalf("interrupted state: %#v %v", stopped, err)
	}
}

func TestHistoryIgnoresIncompleteAtomicFilesAndKeepsBoundedLatestContext(t *testing.T) {
	s := NewAIService(nil, nil)
	s.configPath = filepath.Join(t.TempDir(), "ai.json")
	defer s.ServiceShutdown()
	request := AIReviewRequest{Host: "github.com", Owner: "owner", Repository: "repo", PullRequest: 12, HeadSHA: strings.Repeat("a", 40)}
	run := reviewagent.NewRun("codex", "fixture", reviewagent.Context{HeadSHA: request.HeadSHA, Files: []reviewagent.File{{Path: "app.go", Patch: "@@ -1 +1 @@\n-old\n+new\n"}}}, nil)
	run.MarkContextRead()
	for i := 0; i < 50; i++ {
		if _, err := run.AddFinding(reviewagent.Finding{Path: "app.go", Line: 1, Side: "new", Severity: "bug", Body: fmt.Sprintf("%d %s", i, strings.Repeat("a", 11000))}); err != nil {
			t.Fatal(err)
		}
	}
	run.Finish("completed", "Detailed review", "")
	if err := s.persistAITask(&aiTask{run: run, reviewRequest: &request}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.historyDirectory(), ".mier-settings-incomplete.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	history, err := s.ReviewHistory(AIReviewHistoryRequest{Host: request.Host, Owner: request.Owner, Repository: request.Repository, PullRequest: 12})
	if err != nil || len(history) != 1 {
		t.Fatalf("atomic staging file leaked into history: %d %v", len(history), err)
	}
	request.PreviousReviewID = run.Snapshot().ID
	previous, err := s.previousReviewContext(request)
	if err != nil || len(previous) != 1 || !previous[0].Truncated || previous[0].TotalFindings != 50 || len(previous[0].Findings) == 0 {
		t.Fatalf("latest context was dropped: %#v %v", previous, err)
	}
}

func TestDelayedHistorySaveCannotOverwriteTheCompletedReview(t *testing.T) {
	s := NewAIService(nil, nil)
	s.configPath = filepath.Join(t.TempDir(), "ai.json")
	defer s.ServiceShutdown()
	request := AIReviewRequest{Host: "github.com", Owner: "owner", Repository: "repo", PullRequest: 12}
	run := reviewagent.NewRun("codex", "fixture", reviewagent.Context{}, nil)
	late := aiHistoryRecord{Review: &request, Run: run.Snapshot()}
	run.Finish("completed", "The completed review must survive a delayed poll save", "")
	if err := s.persistAIHistoryRecord(aiHistoryRecord{Review: &request, Run: run.Snapshot()}); err != nil {
		t.Fatal(err)
	}
	if err := s.persistAIHistoryRecord(late); err != nil {
		t.Fatal(err)
	}
	restarted := NewAIService(nil, nil)
	restarted.configPath = s.configPath
	defer restarted.ServiceShutdown()
	restored, err := restarted.GetReview(run.Snapshot().ID)
	if err != nil || restored.State != "completed" || restored.Summary != run.Snapshot().Summary {
		t.Fatalf("newer history was overwritten: %#v %v", restored, err)
	}
}
