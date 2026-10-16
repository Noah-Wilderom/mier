package reviewagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	if os.Getenv("MIER_FAKE_CODEX") == "1" {
		fakeCodex()
		return
	}
	os.Exit(m.Run())
}
func fakeCodex() {
	for _, arg := range os.Args {
		if arg == "list" {
			fmt.Println(`[{"name":"unrelated-user-server","enabled":true}]`)
			return
		}
	}
	if !strings.Contains(strings.Join(os.Args, " "), "features.shell_tool=false") {
		os.Exit(11)
	}
	var bridgeURL, token string
	scanner := bufio.NewScanner(os.Stdin)
	write := func(value any) { data, _ := json.Marshal(value); fmt.Println(string(data)) }
	for scanner.Scan() {
		var message struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result map[string]any  `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(12)
		}
		response := any(map[string]any{})
		switch message.Method {
		case "initialize":
			response = map[string]string{"userAgent": "test"}
		case "initialized":
			continue
		case "account/read":
			response = map[string]any{"account": map[string]string{"type": "chatgpt", "email": "test@example.invalid", "planType": "test"}}
		case "model/list":
			response = map[string]any{"data": []map[string]any{{"model": "model-from-provider", "displayName": "Provider model", "isDefault": true, "supportedReasoningEfforts": []map[string]string{{"reasoningEffort": "low"}, {"reasoningEffort": "medium"}, {"reasoningEffort": "high"}}}}}
		case "thread/start":
			var value struct {
				Sandbox               string
				DeveloperInstructions string
				Config                struct {
					Servers map[string]struct {
						Enabled      bool
						URL          string
						Headers      map[string]string `json:"http_headers"`
						EnabledTools []string          `json:"enabled_tools"`
					} `json:"mcp_servers"`
				}
			}
			if json.Unmarshal(message.Params, &value) != nil || value.Sandbox != "read-only" || value.Config.Servers["unrelated-user-server"].Enabled {
				os.Exit(13)
			}
			if os.Getenv("MIER_FAKE_CODEX_ACTION") != "" {
				if strings.Join(value.Config.Servers["mier"].EnabledTools, ",") != "get_task_context,read_task_file,finish_task" || value.DeveloperInstructions != ActionInstructions {
					os.Exit(19)
				}
			}
			bridgeURL = value.Config.Servers["mier"].URL
			token = strings.TrimPrefix(value.Config.Servers["mier"].Headers["Authorization"], "Bearer ")
			response = map[string]any{"thread": map[string]string{"id": "thread-1"}}
		case "turn/start":
			if expected := os.Getenv("MIER_FAKE_CODEX_EFFORT"); expected != "" {
				var params struct {
					Effort string `json:"effort"`
				}
				if json.Unmarshal(message.Params, &params) != nil || params.Effort != expected {
					os.Exit(18)
				}
			}
			write(map[string]any{"id": message.ID, "result": map[string]any{"turn": map[string]string{"id": "turn-1"}}})
			if os.Getenv("MIER_FAKE_CODEX_HANG") == "1" {
				continue
			}
			client := mcp.NewClient(&mcp.Implementation{Name: "fake-provider", Version: "1"}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: bridgeURL, HTTPClient: &http.Client{Transport: authTransport{token}}}, nil)
			if err != nil {
				os.Exit(14)
			}
			calls := []*mcp.CallToolParams{{Name: "get_review_context", Arguments: Empty{}}, {Name: "read_review_ticket", Arguments: ReadTicketInput{Provider: "jira", Key: "ENG-7"}}, {Name: "add_review_comment", Arguments: Finding{Path: "source.ts", Side: "new", Line: 3, Body: "Check the new behavior.", Severity: "bug"}}, {Name: "finish_review", Arguments: SummaryInput{Summary: "One suggestion; no tests were run."}}}
			if os.Getenv("MIER_FAKE_CODEX_ACTION") != "" {
				calls = []*mcp.CallToolParams{{Name: "get_task_context", Arguments: Empty{}}, {Name: "finish_task", Arguments: ActionProposal{Text: "feat: describe captured change"}}}
			}
			for _, call := range calls {
				result, err := session.CallTool(ctx, call)
				if err != nil || result.IsError {
					os.Exit(15)
				}
			}
			session.Close()
			cancel()
			// An unexpected write approval must be rejected by the host.
			write(map[string]any{"id": "approval-1", "method": "item/fileChange/requestApproval", "params": map[string]any{}})
			continue
		case "":
			if message.ID == "approval-1" && message.Result["decision"] == "decline" {
				for i := 0; i < 2; i++ {
					write(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "tokenUsage": map[string]any{"total": map[string]int64{"inputTokens": 1000, "cachedInputTokens": 400, "outputTokens": 200, "reasoningOutputTokens": 50, "totalTokens": 1200}, "last": map[string]int64{"totalTokens": 800}, "modelContextWindow": 128000}}})
				}
				write(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"status": "completed"}}})
				continue
			}
			os.Exit(16)
		default:
			os.Exit(17)
		}
		write(map[string]any{"id": message.ID, "result": response})
	}
}

func TestCodexActionHarnessUsesTaskInstructionsAndScopedTools(t *testing.T) {
	t.Setenv("MIER_FAKE_CODEX", "1")
	t.Setenv("MIER_FAKE_CODEX_ACTION", "commit")
	action := actionFixture(t, "commit", nil)
	bridge, err := StartActionBridge(action)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	binary, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = RunCodex(ctx, binary, t.TempDir(), "test", "", "Draft commit", bridge, action.Run); err != nil {
		t.Fatal(err)
	}
	if action.Proposal() == nil || action.Proposal().Text != "feat: describe captured change" || len(action.Run.Snapshot().Findings) != 0 || action.Run.Snapshot().ActivityTotals.ToolCalls != 2 {
		t.Fatal("task harness did not use the dedicated tools")
	}
}
func TestCodexHarnessUsesMCPAndRejectsWriteApproval(t *testing.T) {
	t.Setenv("MIER_FAKE_CODEX", "1")
	t.Setenv("MIER_FAKE_CODEX_EFFORT", "high")
	binary, _ := os.Executable()
	scope := reviewFixture()
	scope.Intensity = "high"
	scope.Tickets = []TicketReference{{Provider: "jira", Key: "ENG-7"}}
	run := NewRun("codex", "model-from-provider", scope, nil, func(ctx context.Context, provider, key string) (TicketContent, error) {
		return TicketContent{Provider: provider, Key: key, Description: "Linked ticket requirement"}, nil
	})
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := RunCodex(ctx, binary, t.TempDir(), "test", "model-from-provider", "Review", bridge, run); err != nil {
		t.Fatal(err)
	}
	if !run.HasContext() || len(run.Snapshot().Findings) != 1 || run.Snapshot().Summary == "" {
		t.Fatal("native review tools were not used")
	}
	if usage := run.Snapshot().Usage; usage.TotalTokens == nil || *usage.TotalTokens != 1200 {
		t.Fatal("provider usage notifications were missed or counted twice", usage)
	}
	if run.Snapshot().ActivityTotals.ToolCalls != 4 || run.Snapshot().ActivityTotals.TicketReads != 1 {
		t.Fatal("executed MCP calls were not tracked")
	}
}
func TestCodexCancellationTerminatesOwnedProcess(t *testing.T) {
	t.Setenv("MIER_FAKE_CODEX", "1")
	t.Setenv("MIER_FAKE_CODEX_HANG", "1")
	binary, _ := os.Executable()
	run := NewRun("codex", "", reviewFixture(), nil)
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := RunCodex(ctx, binary, t.TempDir(), "test", "", "Review", bridge, run); err == nil {
		t.Fatal("cancelled review succeeded")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("cancellation did not stop the provider promptly")
	}
}
