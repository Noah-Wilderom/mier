package main

import (
	"context"
	"io"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorktreeChecksRequireApprovalRejectStaleSourceAndPersistEvidence(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	commands := NewCommandService(s.repositories)
	commands.configPath = filepath.Join(t.TempDir(), "commands.json")
	s.commands = commands
	defer commands.ServiceShutdown()
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, bridge *reviewagent.Bridge, _ *reviewagent.Action) error {
		session, err := actionTestSession(ctx, bridge)
		if err != nil {
			return err
		}
		defer session.Close()
		if err = actionTestCall(ctx, session, "get_task_context", reviewagent.Empty{}, nil); err != nil {
			return err
		}
		return actionTestCall(ctx, session, "finish_task", reviewagent.ActionProposal{Text: "No edits required. Checks have not run."}, nil)
	}
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex", ClientID: "verification"})
	if err != nil {
		t.Fatal(err)
	}
	completed := awaitAction(t, s, started.Run.ID)
	if completed.Worktree == nil {
		t.Fatal("worktree missing")
	}
	// executeAction releases its reservation after its terminal snapshot is visible.
	s.wg.Wait()
	worktree := *completed.Worktree
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	command := CustomCommand{Name: "Git check", Executable: binary, Arguments: []string{"--version"}, TimeoutSeconds: 10}
	approval, err := s.PrepareWorktreeCheck(context.Background(), worktree.ID, command)
	if err != nil {
		t.Fatal(err)
	}
	if latest, _ := commands.Latest(worktree.Path); latest != nil {
		t.Fatal("preparing approval executed a command")
	}
	// An edit between the review dialog and execution invalidates that approval.
	if err = os.WriteFile(filepath.Join(worktree.Path, "source.ts"), []byte("const value = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := s.StartWorktreeCheck(context.Background(), approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	checks, err := s.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Run.Status != "failed" || !strings.Contains(checks[0].Run.Error, "after approval") || checks[0].Run.Stdout != "" {
		t.Fatalf("stale approval executed: %#v", checks)
	}
	if _, err = s.StartWorktreeCheck(context.Background(), approval.ID); err == nil {
		t.Fatal("single-use approval reused")
	}
	approval, err = s.PrepareWorktreeCheck(context.Background(), worktree.ID, command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartWorktreeCheck(context.Background(), approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	checks, err = s.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	var passed AIWorktreeCheck
	for _, check := range checks {
		if check.ID == second.ID {
			passed = check
		}
	}
	if passed.Run.ExitCode != 0 || passed.Run.Status != "completed" || passed.Stale || !strings.Contains(passed.Run.Stdout, "git version") {
		t.Fatalf("approved check: %#v", passed)
	}
	if first.ID == second.ID || actionTestGit(t, root, "rev-parse", "HEAD") != head || actionTestGit(t, root, "status", "--porcelain") != "" {
		t.Fatal("verification changed original checkout")
	}
	restarted := NewAIService(nil, nil)
	restarted.configPath = s.configPath
	restarted.repositories = s.repositories
	restarted.commands = commands
	defer restarted.ServiceShutdown()
	checks, err = restarted.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil || len(checks) != 2 {
		t.Fatalf("verification history lost: %#v %v", checks, err)
	}
	if _, err = restarted.StartWorktreeCheck(context.Background(), approval.ID); err == nil {
		t.Fatal("restored an execution capability")
	}
	os.WriteFile(filepath.Join(worktree.Path, "source.ts"), []byte("const value = 3\n"), 0600)
	checks, err = restarted.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if !check.Stale {
			t.Fatal("old evidence remained current after source changed")
		}
	}
	// User-triggered cancellation keeps the stream and publishes its final state.
	entered := make(chan struct{})
	commands.run = func(ctx context.Context, _ string, _ gitclient.UserCommandRequest, stdout, stderr io.Writer) (int, error) {
		stdout.Write([]byte("test started\n"))
		close(entered)
		<-ctx.Done()
		return -1, ctx.Err()
	}
	approval, err = s.PrepareWorktreeCheck(context.Background(), worktree.ID, command)
	if err != nil {
		t.Fatal(err)
	}
	running, err := s.StartWorktreeCheck(context.Background(), approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("check did not start")
	}
	live, err := s.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, check := range live {
		if check.ID == running.ID && strings.Contains(check.Run.Stdout, "test started") {
			found = true
		}
	}
	if !found {
		t.Fatal("live output unavailable")
	}
	if err = s.CancelWorktreeCheck(running.ID); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	live, err = s.WorktreeChecks(context.Background(), worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range live {
		if check.ID == running.ID && check.Run.Status != "canceled" {
			t.Fatal("check not canceled")
		}
	}
}
