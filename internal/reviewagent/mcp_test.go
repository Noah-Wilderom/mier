package reviewagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func reviewFixture() Context {
	return Context{Key: "github.com/owner/repo#12@head", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Files: []File{{Path: "source.ts", Patch: "@@ -2,2 +2,3 @@\n context\n-old\n+new\n+added\n"}}, Warnings: []string{}}
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(clone)
}
func connectBridge(t *testing.T, bridge *Bridge) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "review-test", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: bridge.URL, HTTPClient: &http.Client{Transport: authTransport{bridge.Token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestReviewMCPUsesRealTransportAndOnlyAcceptsCapturedAnchors(t *testing.T) {
	run := NewRun("external", "", reviewFixture(), func(ctx context.Context, path, side string, start, count int) (Source, error) {
		return Source{Path: path, Revision: strings.Repeat("a", 40), StartLine: start, Lines: []string{"source"}, TotalLines: 1}, nil
	})
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	session := connectBridge(t, bridge)
	ctx := context.Background()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 4 {
		t.Fatal("unexpected tool surface", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "add_review_comment" || tool.Name == "finish_review" {
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
				t.Fatalf("Draft tool %s must advertise additive, closed-world behavior", tool.Name)
			}
		}
	}
	contextResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_review_context", Arguments: Empty{}})
	if err != nil || contextResult.IsError {
		t.Fatal(contextResult, err)
	}
	valid := Finding{Path: "source.ts", Line: 4, Side: "new", Body: "This new statement changes behavior.", Severity: "bug"}
	for i := 0; i < 2; i++ {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_review_comment", Arguments: valid})
		if err != nil || result.IsError {
			t.Fatal(result, err)
		}
	}
	if len(run.Snapshot().Findings) != 1 {
		t.Fatal("duplicate comment was added")
	}
	for _, bad := range []Finding{{Path: "other.ts", Line: 4, Side: "new", Body: "bad", Severity: "bug"}, {Path: "source.ts", Line: 300, Side: "new", Body: "bad", Severity: "bug"}, {Path: "source.ts", Line: 4, Side: "old", Body: "bad", Severity: "bug"}} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_review_comment", Arguments: bad})
		if err != nil || !result.IsError {
			t.Fatal("invalid anchor accepted", result, err)
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "finish_review", Arguments: SummaryInput{Summary: "One actionable concern; tests were not run."}})
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	if run.Snapshot().State != "completed" {
		t.Fatal("external review not completed")
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "add_review_comment", Arguments: valid})
	if err != nil || !result.IsError {
		t.Fatal("finished review accepted a comment", result, err)
	}
}
func TestMCPBridgeRejectsUntrustedRequests(t *testing.T) {
	run := NewRun("external", "", reviewFixture(), nil)
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	for _, example := range []struct {
		token, origin, host string
		status              int
	}{{"", "", "", 401}, {bridge.Token, "https://untrusted.invalid", "", 403}, {bridge.Token, "", "evil.invalid", 403}} {
		request, _ := http.NewRequest(http.MethodPost, bridge.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Authorization", "Bearer "+example.token)
		if example.origin != "" {
			request.Header.Set("Origin", example.origin)
		}
		if example.host != "" {
			request.Host = example.host
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != example.status {
			t.Fatalf("got %d, want %d", response.StatusCode, example.status)
		}
	}
}
func TestReviewCancellationConcurrencyAndSourceBounds(t *testing.T) {
	run := NewRun("codex", "", reviewFixture(), func(context.Context, string, string, int, int) (Source, error) {
		return Source{Lines: []string{strings.Repeat("a", 20000)}, TotalLines: 1}, nil
	})
	source, err := run.Read(context.Background(), "source.ts", "new", 1, 1)
	if err != nil || !source.Truncated || len(source.Lines[0]) > 4200 {
		t.Fatal(source, err)
	}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = run.AddFinding(Finding{Path: "source.ts", Line: 3, Side: "new", Body: "same finding", Severity: "warning"})
		}()
	}
	group.Wait()
	if len(run.Snapshot().Findings) != 1 {
		t.Fatal("concurrent retries duplicated a finding")
	}
	run.Cancel()
	if _, err := run.AddFinding(Finding{Path: "source.ts", Line: 3, Side: "new", Body: "late", Severity: "warning"}); err == nil {
		t.Fatal("cancelled run accepted an update")
	}
	if _, err := json.Marshal(run.Snapshot()); err != nil {
		t.Fatal(err)
	}
}
