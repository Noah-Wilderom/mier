package reviewagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPActivityShowsRunningReadsFailuresAndTerminalSummary(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	run := NewRun("external", "", reviewFixture(), func(ctx context.Context, path, side string, start, count int) (Source, error) {
		if path != "source.ts" {
			return Source{}, errors.New("private-error-and-source-must-not-be-in-activity")
		}
		close(entered)
		<-release
		return Source{Path: path, Lines: []string{"first", "second"}}, nil
	})
	bridge, err := StartBridge(run)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	session := connectBridge(t, bridge)
	done := make(chan error, 1)
	go func() {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_review_file", Arguments: ReadFileInput{Path: "source.ts", Side: "new", StartLine: 3, Count: 20}})
		if err == nil && result.IsError {
			err = errors.New("unexpected read failure")
		}
		done <- err
	}()
	<-entered
	value := run.Snapshot()
	if len(value.Activity) != 1 || value.Activity[0].State != "running" || value.ActivityTotals.FileReads != 0 {
		t.Fatal(value.Activity, value.ActivityTotals)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, call := range []*mcp.CallToolParams{
		{Name: "read_review_file", Arguments: ReadFileInput{Path: "../credentials", Side: "new", StartLine: 1, Count: 2}},
		{Name: "finish_review", Arguments: SummaryInput{Summary: "Done"}},
	} {
		if _, err := session.CallTool(context.Background(), call); err != nil {
			t.Fatal(err)
		}
	}
	value = run.Snapshot()
	if value.State != "completed" || value.Activity[2].State != "completed" || value.Activity[2].FinishedAt == "" {
		t.Fatal("terminal summary activity did not finish", value)
	}
	if value.ActivityTotals != (ActivityTotals{ToolCalls: 3, FileReads: 1, FilesRead: 1, LinesRead: 2, BytesRead: 11, FailedCalls: 1}) {
		t.Fatal(value.ActivityTotals)
	}
	if value.Activity[0].Path != "source.ts" || value.Activity[0].StartLine != 3 || value.Activity[0].EndLine != 22 || value.Activity[0].Lines != 2 || value.Activity[1].State != "failed" || value.Activity[1].Path != "" {
		t.Fatal(value.Activity)
	}
	activity, _ := json.Marshal(value.Activity)
	if strings.Contains(string(activity), "credentials") || strings.Contains(string(activity), "private-error") || strings.Contains(string(activity), "first") {
		t.Fatal("activity retained tool contents")
	}
}
func TestActivityRetentionAndSnapshotIsolation(t *testing.T) {
	run := NewRun("external", "", reviewFixture(), nil)
	for i := 0; i < MaxActivity+25; i++ {
		call := run.beginTool("get_review_context", "", "", 0, 0)
		run.endTool(call, nil, nil)
	}
	value := run.Snapshot()
	if len(value.Activity) != MaxActivity || value.ActivityTotals.Omitted != 25 || value.ActivityTotals.ToolCalls != MaxActivity+25 {
		t.Fatal("unbounded or incomplete activity", value.ActivityTotals)
	}
	value.Activity[0].State = "modified"
	if run.Snapshot().Activity[0].State != "completed" {
		t.Fatal("snapshot exposed mutable activity")
	}
	call := run.beginTool("read_review_file", "source.ts", "new", 1, 1)
	run.Cancel()
	run.endTool(call, nil, context.Canceled)
	value = run.Snapshot()
	if value.Activity[len(value.Activity)-1].State != "cancelled" || value.ActivityTotals.FailedCalls != 1 {
		t.Fatal(value.ActivityTotals)
	}
}
