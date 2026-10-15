package reviewagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTicketMCPReadsOnlyCapturedReferencesAndReportsActivity(t *testing.T) {
	scope := reviewFixture()
	scope.Tickets = []TicketReference{{Provider: "jira", Key: "ENG-7", Sources: []string{"title"}}, {Provider: "linear", Key: "ENG-7", Sources: []string{"manual"}}}
	var calls atomic.Int32
	run := NewRun("external", "", scope, nil, func(ctx context.Context, provider, key string) (TicketContent, error) {
		calls.Add(1)
		return TicketContent{Provider: provider, Key: key, Description: "Review requirement", Comments: []TicketComment{{Body: "Clarification"}}}, nil
	})
	// Neither the input nor the tool result can change the ticket allowlist.
	scope.Tickets[0].Key = "OTHER-1"
	run.Context().Tickets[0].Key = "OTHER-2"
	run.Context().Tickets[0].Sources[0] = "other"
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	session := connectBridge(t, bridge)
	ctx := context.Background()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "read_review_ticket" {
			found = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatal("ticket tool must advertise read-only behavior")
			}
		}
	}
	if !found {
		t.Fatal("missing ticket tool")
	}
	for _, provider := range []string{"jira", "linear", "jira"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_review_ticket", Arguments: ReadTicketInput{Provider: provider, Key: "ENG-7"}})
		if err != nil || result.IsError {
			t.Fatal("linked ticket could not be read", result, err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(data), "Review requirement") || !strings.Contains(string(data), provider) {
			t.Fatal("ticket content missing", string(data))
		}
	}
	for _, input := range []ReadTicketInput{{Provider: "jira", Key: "OTHER-1"}, {Provider: "other", Key: "ENG-7"}, {Provider: "linear", Key: "../credentials"}} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_review_ticket", Arguments: input})
		if err != nil || !result.IsError {
			t.Fatal("out-of-scope ticket accepted", result, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("invalid requests reached ticket reader")
	}
	snapshot := run.Snapshot()
	if snapshot.ActivityTotals.TicketReads != 3 || snapshot.ActivityTotals.TicketsRead != 2 || snapshot.ActivityTotals.FileReads != 0 || snapshot.ActivityTotals.TicketBytesRead != 3*len("Review requirementClarification") {
		t.Fatal("incorrect ticket activity", snapshot.ActivityTotals)
	}
	if snapshot.Activity[0].TicketProvider != "jira" || snapshot.Activity[0].TicketKey != "ENG-7" || snapshot.Activity[5].TicketKey != "" {
		t.Fatal("invalid ticket locations retained", snapshot.Activity)
	}
	data, _ := json.Marshal(snapshot)
	if strings.Contains(string(data), "Review requirement") || strings.Contains(string(data), "../credentials") {
		t.Fatal("tool content or rejected arguments leaked into telemetry")
	}
	run.Cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_review_ticket", Arguments: ReadTicketInput{Provider: "jira", Key: "ENG-7"}})
	if err != nil || !result.IsError || calls.Load() != 3 {
		t.Fatal("cancelled review accepted ticket access", result, err)
	}
}

func TestTicketReadBoundsAndCancellationDiscardLateContent(t *testing.T) {
	scope := reviewFixture()
	scope.Tickets = []TicketReference{{Provider: "linear", Key: "ENG-7"}}
	var calls atomic.Int32
	run := NewRun("codex", "", scope, nil, func(ctx context.Context, provider, key string) (TicketContent, error) {
		calls.Add(1)
		return TicketContent{Provider: provider, Key: key}, nil
	})
	for i := 0; i < MaxTicketReads; i++ {
		if _, err := run.ReadTicket(context.Background(), "linear", "ENG-7"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run.ReadTicket(context.Background(), "linear", "ENG-7"); err == nil || calls.Load() != MaxTicketReads {
		t.Fatal("ticket read limit not enforced")
	}
	for _, content := range []TicketContent{{Provider: "jira", Key: "ENG-7"}, {Provider: "linear", Key: "ENG-8"}, {Provider: "linear", Key: "ENG-7", Description: strings.Repeat("x", MaxTicketTextBytes+1)}} {
		run := NewRun("codex", "", scope, nil, func(context.Context, string, string) (TicketContent, error) { return content, nil })
		if _, err := run.ReadTicket(context.Background(), "linear", "ENG-7"); err == nil {
			t.Fatal("invalid ticket response accepted")
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	run = NewRun("codex", "", scope, nil, func(context.Context, string, string) (TicketContent, error) {
		close(started)
		<-release
		return TicketContent{Provider: "linear", Key: "ENG-7", Description: "late"}, nil
	})
	result := make(chan error, 1)
	go func() { _, err := run.ReadTicket(context.Background(), "linear", "ENG-7"); result <- err }()
	<-started
	run.Cancel()
	close(release)
	if err := <-result; err == nil {
		t.Fatal("cancelled review exposed late ticket content")
	}
}
