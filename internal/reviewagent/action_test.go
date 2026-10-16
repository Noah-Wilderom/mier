package reviewagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func actionFixture(t *testing.T, kind string, worktree *WorktreeTools) *Action {
	t.Helper()
	action, err := NewAction("codex", "fixture-model", reviewFixture(), ActionContext{Kind: kind, Input: "Captured Git changes", Instructions: "Explain what changed"}, func(_ context.Context, path, side string, start, count int) (Source, error) {
		return Source{Path: path, Revision: strings.Repeat("a", 40), StartLine: start, Lines: []string{"captured source"}, TotalLines: 1}, nil
	}, worktree)
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func TestAIActionBridgeProducesTypedProposalWithoutReviewOrWriteTools(t *testing.T) {
	action := actionFixture(t, "commit", nil)
	bridge, err := StartActionBridge(action)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	session := connectBridge(t, bridge)
	ctx := context.Background()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 3 {
		t.Fatal("unexpected action tools", tools, err)
	}
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "review") || strings.Contains(tool.Name, "write") {
			t.Fatal("review or coding permissions leaked into drafting")
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "finish_task", Arguments: ActionProposal{Text: "feat: describe staged change"}})
	if err != nil || !result.IsError || action.Proposal() != nil {
		t.Fatal("proposal accepted before reading context", result, err)
	}
	for _, tool := range []mcp.CallToolParams{{Name: "get_task_context", Arguments: Empty{}}, {Name: "read_task_file", Arguments: ReadFileInput{Path: "source.ts", Side: "new", StartLine: 1, Count: 1}}, {Name: "finish_task", Arguments: ActionProposal{Text: "feat: describe staged change"}}} {
		result, err = session.CallTool(ctx, &tool)
		if err != nil || result.IsError {
			t.Fatal(tool.Name, result, err)
		}
	}
	proposal := action.Proposal()
	if proposal == nil || proposal.Text != "feat: describe staged change" || len(action.Run.Snapshot().Findings) != 0 {
		t.Fatal("proposal was not isolated from review findings")
	}
	if action.Run.Snapshot().ActivityTotals.FileReads != 1 {
		t.Fatal("source read telemetry missing")
	}
	action.Run.Finish("completed", "", "")
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "finish_task", Arguments: ActionProposal{Text: "changed"}})
	if err != nil || !result.IsError || action.Proposal().Text != "feat: describe staged change" {
		t.Fatal("finished action accepted a new proposal")
	}
}

func TestAIActionProposalKindsRemainExplicitAndBounded(t *testing.T) {
	for _, kind := range []string{"commit", "pull_request", "explain", "conflict", "code"} {
		t.Run(kind, func(t *testing.T) {
			var worktree *WorktreeTools
			if kind == "code" {
				worktree = &WorktreeTools{List: func(context.Context, WorktreeListInput) (WorktreeFilePage, error) { return WorktreeFilePage{}, nil }, Read: func(context.Context, string) (WorkFile, error) { return WorkFile{}, nil }, Write: func(context.Context, WriteWorkFile) (WorkFile, error) { return WorkFile{}, nil }}
			}
			action := actionFixture(t, kind, worktree)
			action.Run.MarkContextRead()
			if err := action.finish(ActionProposal{}); err == nil {
				t.Fatal("empty proposal accepted")
			}
			good := ActionProposal{Text: "Explain captured behavior; checks were not run."}
			if kind == "pull_request" {
				good = ActionProposal{Title: "Describe committed behavior", Body: "A precise description."}
			}
			if kind == "conflict" {
				content := "resolved source\n"
				good = ActionProposal{Text: "Preserve both paths.", Content: &content}
			}
			if err := action.finish(good); err != nil {
				t.Fatal(err)
			}
			if err := action.finish(ActionProposal{Text: strings.Repeat("x", 65537), Title: "Bad\ntitle"}); err == nil {
				t.Fatal("invalid typed proposal accepted")
			}
		})
	}
}

func TestAIWorktreeBridgeUsesDedicatedScopedToolsAndTracksExecutedWrites(t *testing.T) {
	stored := WorkFile{Path: "source.ts", Content: "before\n", Hash: "first", Exists: true}
	writes := 0
	worktree := &WorktreeTools{List: func(context.Context, WorktreeListInput) (WorktreeFilePage, error) {
		return WorktreeFilePage{Paths: []string{"source.ts"}, Total: 1}, nil
	}, Read: func(_ context.Context, path string) (WorkFile, error) { return stored, nil }, Write: func(_ context.Context, input WriteWorkFile) (WorkFile, error) {
		writes++
		stored.Content = input.Content
		stored.Hash = "second"
		return stored, nil
	}}
	action := actionFixture(t, "code", worktree)
	bridge, err := StartActionBridge(action)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	session := connectBridge(t, bridge)
	ctx := context.Background()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 6 {
		t.Fatal("coding tool surface", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "write_worktree_file" && (tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
			t.Fatal("file writes must advertise their side effect")
		}
	}
	for _, tool := range []mcp.CallToolParams{{Name: "get_task_context", Arguments: Empty{}}, {Name: "list_worktree_files", Arguments: Empty{}}, {Name: "read_worktree_file", Arguments: map[string]string{"path": "source.ts"}}, {Name: "write_worktree_file", Arguments: WriteWorkFile{Path: "source.ts", ExpectedHash: "first", Content: "after\n"}}, {Name: "finish_task", Arguments: ActionProposal{Text: "Edited source.ts in the worktree; checks were not run."}}} {
		result, err := session.CallTool(ctx, &tool)
		if err != nil || result.IsError {
			t.Fatal(tool.Name, result, err)
		}
	}
	if writes != 1 || action.Writes() != 1 || action.Run.Snapshot().ActivityTotals.FileReads != 1 {
		t.Fatal("executed worktree activity missing")
	}
	if totals := action.Run.Snapshot().ActivityTotals; totals.LinesRead != 1 || totals.BytesRead != len("before\n") {
		t.Fatal("whole-file reads counted an extra trailing line or omitted newline bytes", totals)
	}
	data, _ := json.Marshal(action.Run.Snapshot().Activity)
	if strings.Contains(string(data), "after") || strings.Contains(string(data), "first") {
		t.Fatal("file contents or input hashes leaked into telemetry")
	}
	permissionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sdkTools, sdkSession, err := copilotReviewTools(permissionCtx, bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer sdkSession.Close()
	if len(sdkTools) != 6 {
		t.Fatal("Copilot did not use the dedicated action allowlist")
	}
	for _, tool := range sdkTools {
		if tool.Name == "add_review_comment" {
			t.Fatal("native review writes leaked into coding action")
		}
	}
	action.Run.Finish("cancelled", "", "")
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "write_worktree_file", Arguments: WriteWorkFile{Path: "source.ts", ExpectedHash: "second", Content: "must not write"}})
	if err != nil || !result.IsError || writes != 1 {
		t.Fatal("stopped action edited its worktree", result, err)
	}
}
