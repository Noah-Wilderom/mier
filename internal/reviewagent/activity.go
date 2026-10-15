package reviewagent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
)

const MaxActivity = 200

// Activity deliberately excludes tool arguments, results, source, and errors.
// Only executed review tools and bounded repository/ticket locations appear.
type ToolActivity struct {
	ID             string `json:"id"`
	Tool           string `json:"tool"`
	State          string `json:"state"`
	Path           string `json:"path,omitempty"`
	Side           string `json:"side,omitempty"`
	TicketProvider string `json:"ticketProvider,omitempty"`
	TicketKey      string `json:"ticketKey,omitempty"`
	StartLine      int    `json:"startLine,omitempty"`
	EndLine        int    `json:"endLine,omitempty"`
	Lines          int    `json:"lines"`
	Bytes          int    `json:"bytes"`
	StartedAt      string `json:"startedAt"`
	FinishedAt     string `json:"finishedAt,omitempty"`
	DurationMs     int64  `json:"durationMs"`
}
type ActivityTotals struct {
	ToolCalls       int `json:"toolCalls"`
	FailedCalls     int `json:"failedCalls"`
	FileReads       int `json:"fileReads"`
	FilesRead       int `json:"filesRead"`
	LinesRead       int `json:"linesRead"`
	BytesRead       int `json:"bytesRead"`
	TicketReads     int `json:"ticketReads"`
	TicketsRead     int `json:"ticketsRead"`
	TicketBytesRead int `json:"ticketBytesRead"`
	Omitted         int `json:"omitted"`
}
type toolCall struct {
	id      string
	tool    string
	path    string
	started time.Time
}

func activityPath(value string) string {
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\\:") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	clean := path.Clean(value)
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}
func (r *Run) beginTool(tool, file, side string, start, count int) toolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.snapshot.ActivityTotals.ToolCalls++
	call := toolCall{id: fmt.Sprintf("tool-%d", r.snapshot.ActivityTotals.ToolCalls), tool: tool, path: activityPath(file), started: now}
	entry := ToolActivity{ID: call.id, Tool: tool, Path: call.path, State: "running", StartedAt: now.UTC().Format(time.RFC3339Nano)}
	if call.path != "" && (side == "old" || side == "new") && start > 0 && count > 0 && count <= 250 && start <= int(^uint(0)>>1)-count {
		entry.Side, entry.StartLine, entry.EndLine = side, start, start+count-1
	}
	if len(r.snapshot.Activity) == MaxActivity {
		r.snapshot.Activity = r.snapshot.Activity[1:]
		r.snapshot.ActivityTotals.Omitted++
	}
	r.snapshot.Activity = append(r.snapshot.Activity, entry)
	r.snapshot.Version++
	return call
}
func (r *Run) endTool(call toolCall, source *Source, err error) {
	r.completeTool(call, source, nil, err)
}
func (r *Run) beginTicketTool(provider, key string) toolCall {
	call := r.beginTool("read_review_ticket", "", "", 0, 0)
	// Do not retain arbitrary arguments from rejected ticket requests.
	if r.ticketAllowed(provider, key) {
		r.mu.Lock()
		for i := range r.snapshot.Activity {
			if r.snapshot.Activity[i].ID == call.id {
				r.snapshot.Activity[i].TicketProvider = provider
				r.snapshot.Activity[i].TicketKey = key
				break
			}
		}
		r.mu.Unlock()
	}
	return call
}
func (r *Run) completeTool(call toolCall, source *Source, ticket *TicketContent, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.snapshot.Activity {
		if entry.ID == call.id && entry.State != "running" {
			// Cancellation already closed this call for a terminal snapshot.
			return
		}
	}
	now := time.Now()
	state := "completed"
	lines, bytes := 0, 0
	if err != nil {
		state = "failed"
		if errors.Is(err, context.Canceled) {
			state = "cancelled"
		}
		r.snapshot.ActivityTotals.FailedCalls++
	} else if source != nil {
		lines = len(source.Lines)
		for _, line := range source.Lines {
			bytes += len(line)
		}
		r.snapshot.ActivityTotals.FileReads++
		r.snapshot.ActivityTotals.LinesRead += lines
		r.snapshot.ActivityTotals.BytesRead += bytes
		if r.readFiles == nil {
			r.readFiles = map[string]bool{}
		}
		if call.path != "" {
			r.readFiles[call.path] = true
		}
		r.snapshot.ActivityTotals.FilesRead = len(r.readFiles)
	} else if ticket != nil {
		bytes = ticket.TextBytes()
		r.snapshot.ActivityTotals.TicketReads++
		r.snapshot.ActivityTotals.TicketBytesRead += bytes
		if r.readTickets == nil {
			r.readTickets = map[string]bool{}
		}
		r.readTickets[ticket.Provider+"/"+ticket.Key] = true
		r.snapshot.ActivityTotals.TicketsRead = len(r.readTickets)
	}
	for i := range r.snapshot.Activity {
		entry := &r.snapshot.Activity[i]
		if entry.ID != call.id {
			continue
		}
		entry.State, entry.FinishedAt, entry.DurationMs = state, now.UTC().Format(time.RFC3339Nano), now.Sub(call.started).Milliseconds()
		entry.Lines, entry.Bytes = lines, bytes
		break
	}
	// Provider usage and in-flight tool completion may arrive during shutdown.
	r.snapshot.Version++
}
