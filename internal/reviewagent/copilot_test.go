package reviewagent

import (
	"context"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// Exercise the same real MCP transport that SDK tool handlers use, without
// requiring credentials, inference, or a provider process in CI.
func TestCopilotToolsForwardBoundedReviewCapabilities(t *testing.T) {
	scope := reviewFixture()
	scope.Tickets = []TicketReference{{Provider: "linear", Key: "ENG-7"}}
	run := NewRun("copilot", "", scope, nil, func(ctx context.Context, provider, key string) (TicketContent, error) {
		return TicketContent{Provider: provider, Key: key, Description: "Linked ticket requirement"}, nil
	})
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tools, session, err := copilotReviewTools(ctx, bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if len(tools) != 5 {
		t.Fatal("unexpected tool count")
	}
	byName := map[string]copilot.Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	result, err := byName["get_review_context"].Handler(copilot.ToolInvocation{Arguments: map[string]any{}})
	if err != nil || result.ResultType != "success" || !run.HasContext() {
		t.Fatal("context was not delivered", result, err)
	}
	result, err = byName["read_review_ticket"].Handler(copilot.ToolInvocation{Arguments: map[string]any{"provider": "linear", "key": "ENG-7"}})
	if err != nil || result.ResultType != "success" || run.Snapshot().ActivityTotals.TicketReads != 1 {
		t.Fatal("Copilot did not receive linked ticket context", result, err)
	}
	add := byName["add_review_comment"]
	if add.Parameters == nil || !add.SkipPermission || add.Defer != copilot.ToolDeferNever {
		t.Fatal("review tool must be declared explicitly and ready")
	}
	args := map[string]any{"path": "source.ts", "line": 4, "side": "new", "body": "A concrete concern.", "severity": "bug"}
	for i := 0; i < 2; i++ {
		if result, err := add.Handler(copilot.ToolInvocation{Arguments: args}); err != nil || result.ResultType != "success" {
			t.Fatal(result, err)
		}
	}
	if len(run.Snapshot().Findings) != 1 {
		t.Fatal("duplicate or missing draft")
	}
	args["line"] = 500
	if result, err := add.Handler(copilot.ToolInvocation{Arguments: args}); err != nil || result.ResultType == "success" {
		t.Fatal("invalid anchor accepted", result, err)
	}
	cancel()
	if _, err := byName["get_review_context"].Handler(copilot.ToolInvocation{Arguments: map[string]any{}}); err == nil {
		t.Fatal("cancelled review accepted tool execution")
	}
}

func TestCopilotRejectsUnregisteredPermissions(t *testing.T) {
	decision, err := rejectReviewPermission(nil, copilot.PermissionInvocation{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decision.(*rpc.PermissionDecisionReject); !ok {
		t.Fatal("unregistered permission approved")
	}
}
