package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/reviewagent"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type actionTestTransport struct{ token string }

func (a actionTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(request)
}
func actionTestSession(ctx context.Context, bridge *reviewagent.Bridge) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "native-action-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: bridge.URL, HTTPClient: &http.Client{Transport: actionTestTransport{bridge.Token}}}, nil)
}
func actionTestCall(ctx context.Context, session *mcp.ClientSession, name string, input, output any) error {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("%s: %v", name, result.Content)
	}
	if output != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, output)
	}
	return nil
}
func actionTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
func actionServiceFixture(t *testing.T) (*AIService, string, string) {
	t.Helper()
	root := t.TempDir()
	actionTestGit(t, root, "init", "-b", "main")
	actionTestGit(t, root, "config", "user.name", "Action test")
	actionTestGit(t, root, "config", "user.email", "action@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actionTestGit(t, root, "add", "source.ts")
	actionTestGit(t, root, "commit", "-m", "feat: initial source")
	s := NewAIService(nil, nil)
	s.configPath = filepath.Join(t.TempDir(), "ai.json")
	s.directory = t.TempDir()
	s.repositories = NewRepositoryService()
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	return s, root, actionTestGit(t, root, "rev-parse", "HEAD")
}
func awaitAction(t *testing.T, s *AIService, id string) AIActionSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		value, err := s.GetAction(id)
		if err != nil {
			t.Fatal(err)
		}
		if value.Run.State != "preparing" && value.Run.State != "running" {
			s.wg.Wait()
			value, _ = s.GetAction(id)
			return value
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return AIActionSnapshot{}
}

func TestNativeAICommitDraftRequiresExplicitUseAndRejectsMovedStage(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actionTestGit(t, root, "add", "source.ts")
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, bridge *reviewagent.Bridge, _ *reviewagent.Action) error {
		session, err := actionTestSession(ctx, bridge)
		if err != nil {
			return err
		}
		defer session.Close()
		for _, call := range []struct {
			name  string
			input any
		}{{"get_task_context", reviewagent.Empty{}}, {"read_task_file", reviewagent.ReadFileInput{Path: "source.ts", Side: "new", StartLine: 1, Count: 1}}, {"finish_task", reviewagent.ActionProposal{Text: "feat: update source value"}}} {
			if err := actionTestCall(ctx, session, call.name, call.input, nil); err != nil {
				return err
			}
		}
		return nil
	}
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "commit", ExpectedHead: head, Provider: "codex", Intensity: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	completed := awaitAction(t, s, started.Run.ID)
	if completed.Run.State != "completed" || completed.Proposal == nil || completed.Proposal.Text != "feat: update source value" || completed.Run.ActivityTotals.FileReads != 1 {
		t.Fatal(completed)
	}
	if err = s.ValidateAction(context.Background(), started.Run.ID); err != nil {
		t.Fatal(err)
	}
	if actionTestGit(t, root, "rev-parse", "HEAD") != head || actionTestGit(t, root, "diff", "--cached", "--name-only") != "source.ts" {
		t.Fatal("draft mutated Git state")
	}
	if err = os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actionTestGit(t, root, "add", "source.ts")
	if err = s.ValidateAction(context.Background(), started.Run.ID); err == nil {
		t.Fatal("stale draft accepted")
	}
	encoded, _ := json.Marshal(completed)
	if strings.Contains(string(encoded), "Bearer") || strings.Contains(string(encoded), "IndexBlobs") {
		t.Fatal("internal task data exposed")
	}
}

func TestNativeAgentUsesPersistedWorktreeAndKeepsEditsWhenStopped(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	written := make(chan struct{})
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, bridge *reviewagent.Bridge, _ *reviewagent.Action) error {
		session, err := actionTestSession(ctx, bridge)
		if err != nil {
			return err
		}
		defer session.Close()
		if err = actionTestCall(ctx, session, "get_task_context", reviewagent.Empty{}, nil); err != nil {
			return err
		}
		var file reviewagent.WorkFile
		if err = actionTestCall(ctx, session, "read_worktree_file", struct {
			Path string `json:"path"`
		}{"source.ts"}, &file); err != nil {
			return err
		}
		if err = actionTestCall(ctx, session, "write_worktree_file", reviewagent.WriteWorkFile{Path: file.Path, ExpectedHash: file.Hash, Content: "const value = 4\n"}, nil); err != nil {
			return err
		}
		close(written)
		<-ctx.Done()
		return ctx.Err()
	}
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex", Instructions: "Change value to four", ClientID: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Worktree == nil || started.Worktree.Path == root {
		t.Fatal("no isolated worktree", started)
	}
	select {
	case <-written:
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not edit source")
	}
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err == nil {
		t.Fatal("active worktree removed")
	}
	if err = s.CancelAction(started.Run.ID); err != nil {
		t.Fatal(err)
	}
	finished := awaitAction(t, s, started.Run.ID)
	if finished.Writes != 1 || finished.Run.State != "cancelled" {
		t.Fatal(finished)
	}
	data, _ := os.ReadFile(filepath.Join(root, "source.ts"))
	if string(data) != "const value = 1\n" || actionTestGit(t, root, "status", "--porcelain") != "" {
		t.Fatal("agent changed original checkout")
	}
	worktree := started.Worktree.Path
	data, _ = os.ReadFile(filepath.Join(worktree, "source.ts"))
	if string(data) != "const value = 4\n" || actionTestGit(t, worktree, "diff", "--cached", "--name-only") != "" {
		t.Fatal("edits missing or staged automatically")
	}
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err == nil {
		t.Fatal("dirty worktree removed")
	}
	restarted := NewAIService(nil, nil)
	restarted.configPath = s.configPath
	restarted.repositories = s.repositories
	items, err := restarted.AgentWorktrees(root)
	if err != nil || len(items) != 1 || items[0].Path != worktree {
		t.Fatal("worktree lost across restart", items, err)
	}
	// A manually committed agent result may be followed by another scoped task.
	actionTestGit(t, worktree, "add", "source.ts")
	actionTestGit(t, worktree, "commit", "-m", "feat: accept agent value")
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, bridge *reviewagent.Bridge, _ *reviewagent.Action) error {
		session, err := actionTestSession(ctx, bridge)
		if err != nil {
			return err
		}
		defer session.Close()
		if err = actionTestCall(ctx, session, "get_task_context", reviewagent.Empty{}, nil); err != nil {
			return err
		}
		return actionTestCall(ctx, session, "finish_task", reviewagent.ActionProposal{Text: "Existing edits inspected; no tests were run."}, nil)
	}
	continued, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", WorktreeID: started.Worktree.ID, Provider: "codex", Instructions: "Inspect the accepted changes"})
	if err != nil {
		t.Fatal(err)
	}
	if continued.Worktree.Path != worktree || awaitAction(t, s, continued.Run.ID).Run.State != "completed" {
		t.Fatal("follow-up failed")
	}
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clean worktree not removed", err)
	}
	if !strings.Contains(actionTestGit(t, root, "branch", "--list", "mier/agent/*"), started.Worktree.Branch) {
		t.Fatal("removal lost accepted branch")
	}
}

func TestNativeAIRequiresTypedResultAndRedactsErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, root, head := actionServiceFixture(t)
			s.actionRunner = func(context.Context, AIActionRequest, *reviewagent.Bridge, *reviewagent.Action) error {
				if fail {
					return errors.New("Bearer ghp_fixtureSecret12345678")
				}
				return nil
			}
			started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			value := awaitAction(t, s, started.Run.ID)
			if value.Run.State != "failed" || value.Run.Error == "" || strings.Contains(value.Run.Error, "fixtureSecret") {
				t.Fatal(value.Run)
			}
		})
	}
}

func TestNativeAgentCancellationRetainsOwnershipUntilProviderExits(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, _ *reviewagent.Bridge, _ *reviewagent.Action) error {
		close(entered)
		<-release
		return ctx.Err()
	}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err = s.CancelAction(started.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err == nil {
		t.Fatal("worktree removed while provider still owns it")
	}
	if _, err = s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", WorktreeID: started.Worktree.ID, Provider: "codex"}); err == nil {
		t.Fatal("follow-up overlapped a cancelled provider")
	}
	once.Do(func() { close(release) })
	s.wg.Wait()
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAIActionSharesTheFourTaskLimit(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	s.actionRunner = func(context.Context, AIActionRequest, *reviewagent.Bridge, *reviewagent.Action) error { return nil }
	s.active = map[string]string{"one": "1", "two": "2", "three": "3", "four": "4"}
	if _, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex"}); err == nil || !strings.Contains(err.Error(), "Four") {
		t.Fatal("task limit ignored", err)
	}
	items, err := s.AgentWorktrees(root)
	if err != nil || len(items) != 0 {
		t.Fatal("task limit created a worktree", items, err)
	}
}

func TestNativeAgentCanForgetAWorktreeRemovedByAnotherClient(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	s.actionRunner = func(context.Context, AIActionRequest, *reviewagent.Bridge, *reviewagent.Action) error { return nil }
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: root, Kind: "code", ExpectedHead: head, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	awaitAction(t, s, started.Run.ID)
	actionTestGit(t, root, "worktree", "remove", started.Worktree.Path)
	if err = s.RemoveAgentWorktree(context.Background(), started.Worktree.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.AgentWorktrees(root)
	if err != nil || len(items) != 0 {
		t.Fatal("missing directory record was not forgotten", items, err)
	}
	if actionTestGit(t, root, "rev-parse", started.Worktree.Branch) != head {
		t.Fatal("forgetting a removed worktree lost its branch")
	}
}

func TestNativeAgentRestoresThroughRepositoryPathAliases(t *testing.T) {
	s, root, head := actionServiceFixture(t)
	alias := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("Directory symlinks unavailable: %v", err)
	}
	s.actionRunner = func(ctx context.Context, _ AIActionRequest, bridge *reviewagent.Bridge, _ *reviewagent.Action) error {
		session, err := actionTestSession(ctx, bridge)
		if err != nil {
			return err
		}
		defer session.Close()
		if err = actionTestCall(ctx, session, "get_task_context", reviewagent.Empty{}, nil); err != nil {
			return err
		}
		return actionTestCall(ctx, session, "finish_task", reviewagent.ActionProposal{Text: "Inspected the selected worktree; no edits were needed."}, nil)
	}
	started, err := s.StartAction(context.Background(), AIActionRequest{Root: alias, Kind: "code", ExpectedHead: head, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if awaitAction(t, s, started.Run.ID).Run.State != "completed" {
		t.Fatal("initial aliased task failed")
	}
	for _, path := range []string{root, alias} {
		items, err := s.AgentWorktrees(path)
		if err != nil || len(items) != 1 {
			t.Fatal("worktree hidden by a repository alias", path, items, err)
		}
	}
	continued, err := s.StartAction(context.Background(), AIActionRequest{Root: alias, Kind: "code", WorktreeID: started.Worktree.ID, Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if awaitAction(t, s, continued.Run.ID).Run.State != "completed" {
		t.Fatal("aliased follow-up failed")
	}
}
