package main

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/reviewagent"
	"strings"
	"time"
)

// Capture references locally at review start. No ticket content is fetched
// until the reviewer asks, and no arbitrary key, URL or search is exposed.
func (s *AIService) captureReviewTickets(ctx context.Context, request TicketPullRequest) ([]reviewagent.TicketReference, reviewagent.ReadTicket, []string) {
	references := []reviewagent.TicketReference{}
	warnings := []string{}
	readers := map[string]func(context.Context) (TicketIssue, error){}
	add := func(provider string, tickets []Ticket, truncated bool, read func(context.Context, string) (TicketIssue, error)) {
		for _, ticket := range tickets {
			key := ticket.Key
			references = append(references, reviewagent.TicketReference{Provider: provider, Key: key, Sources: append([]string{}, ticket.Sources...)})
			readers[provider+"/"+key] = func(ctx context.Context) (TicketIssue, error) { return read(ctx, key) }
		}
		if truncated {
			warnings = append(warnings, provider+" ticket context is limited to 25 linked tickets. Review remaining requirements manually.")
		}
	}
	pr, err := jiraPRKey(request)
	if err != nil {
		return references, nil, []string{"Ticket references could not be captured for this repository."}
	}
	if s.jira != nil {
		status, _ := s.jira.GetJiraStatus(ctx)
		if status.Connected {
			connection, err := s.jira.jiraConnection(request.Repository)
			if err == nil {
				s.jira.mu.Lock()
				selection := s.jira.config.Selections[pr]
				tickets, truncated := jiraDetectedTickets(request, selection, connection.preferences.Projects)
				current := !s.jira.closed && s.jira.epoch == connection.epoch
				s.jira.mu.Unlock()
				if current {
					add("jira", tickets, truncated, func(ctx context.Context, key string) (TicketIssue, error) {
						ctx, cancel := s.jira.requestContext(ctx)
						defer cancel()
						return s.jira.fetchJiraIssue(ctx, connection, key, true, false)
					})
				} else {
					err = errors.New("Jira settings changed")
				}
			}
			if err != nil {
				warnings = append(warnings, "Jira ticket context is unavailable. Check the connected site and repository settings, then start a new review.")
			}
		} else if status.ClientID != "" && status.Error != "" {
			warnings = append(warnings, "Jira ticket context is unavailable. Check the connection in Settings.")
		}
	}
	if s.linear != nil {
		status, _ := s.linear.GetLinearStatus(ctx)
		if status.Connected {
			connection, _, err := s.linear.linearConnection(request.Repository)
			if err == nil {
				s.linear.mu.Lock()
				selection := s.linear.config.Selections[connection.workspace.ID+"/"+pr]
				tickets, truncated := jiraDetectedTickets(request, selection, connection.preferences.Teams)
				current := !s.linear.closed && s.linear.epoch == connection.epoch
				s.linear.mu.Unlock()
				if current {
					add("linear", tickets, truncated, func(ctx context.Context, key string) (TicketIssue, error) {
						ctx, cancel := s.linear.requestContext(ctx)
						defer cancel()
						return s.linear.fetchLinearIssue(ctx, connection, key, true, false)
					})
				} else {
					err = errors.New("Linear settings changed")
				}
			}
			if err != nil {
				warnings = append(warnings, "Linear ticket context is unavailable. Check the connected workspace and repository settings, then start a new review.")
			}
		} else if status.ClientID != "" && status.Error != "" {
			warnings = append(warnings, "Linear ticket context is unavailable. Check the connection in Settings.")
		}
	}
	return references, func(ctx context.Context, provider, key string) (reviewagent.TicketContent, error) {
		if err := ctx.Err(); err != nil {
			return reviewagent.TicketContent{}, err
		}
		read := readers[provider+"/"+key]
		if read == nil {
			return reviewagent.TicketContent{}, errors.New("This ticket was not linked when the review started")
		}
		issue, err := read(ctx)
		if ctx.Err() != nil {
			return reviewagent.TicketContent{}, ctx.Err()
		}
		if err != nil {
			// Never forward transport, credential-store or provider response data.
			return reviewagent.TicketContent{}, errors.New("Linked ticket could not be read. Check the ticket and provider connection; if settings changed, start a new review. Continue reviewing code and report the missing ticket context")
		}
		return aiTicketContent(provider, issue), nil
	}, warnings
}

type aiTicketTextBudget struct {
	remaining int
	truncated bool
}

func (b *aiTicketTextBudget) take(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	limit = min(limit, b.remaining)
	if len(value) > limit {
		b.truncated = true
		value = boundedLinearText(value, limit)
	}
	b.remaining -= len(value)
	return value
}

// Jira's ADF is converted to readable text. Only visible text and known display
// attributes are traversed, never raw JSON, arbitrary attributes or embedded URLs.
func (b *aiTicketTextBudget) document(value any, limit int) string {
	if text, ok := value.(string); ok {
		return b.take(text, limit)
	}
	var text strings.Builder
	visits := 0
	var walk func(any, int)
	walk = func(value any, depth int) {
		visits++
		if visits > 8192 || depth > 32 || text.Len() >= limit {
			b.truncated = true
			return
		}
		switch value := value.(type) {
		case []any:
			for _, child := range value {
				walk(child, depth+1)
				if visits > 8192 || text.Len() >= limit {
					break
				}
			}
		case map[string]any:
			kind, _ := value["type"].(string)
			write := func(value string) {
				if len(value) > limit-text.Len() {
					b.truncated = true
				}
				text.WriteString(boundedLinearText(value, max(0, limit-text.Len())))
			}
			switch kind {
			case "text":
				value, _ := value["text"].(string)
				write(value)
			case "mention", "emoji":
				attrs, _ := value["attrs"].(map[string]any)
				for _, key := range []string{"text", "shortName"} {
					if label, ok := attrs[key].(string); ok {
						write(label)
						break
					}
				}
			case "hardBreak", "rule":
				write("\n")
			default:
				if kind == "listItem" || kind == "taskItem" {
					write("- ")
				}
				walk(value["content"], depth+1)
				switch kind {
				case "paragraph", "heading", "listItem", "taskItem", "codeBlock", "tableRow":
					write("\n")
				case "tableCell", "tableHeader":
					write("\t")
				}
			}
		}
	}
	walk(value, 0)
	return b.take(strings.TrimSpace(text.String()), limit)
}

func aiTicketContent(provider string, issue TicketIssue) reviewagent.TicketContent {
	budget := aiTicketTextBudget{remaining: reviewagent.MaxTicketTextBytes}
	content := reviewagent.TicketContent{Provider: provider, Key: issue.Key, Summary: budget.take(issue.Summary, 4096), URL: budget.take(issue.URL, 2048), Status: budget.take(issue.Status, 256), Type: budget.take(issue.Type, 512), Assignee: budget.take(issue.Assignee, 256), Priority: budget.take(issue.Priority, 128), ParentKey: budget.take(issue.ParentKey, 128), UpdatedAt: budget.take(issue.UpdatedAt, 128), ReadAt: time.Now().UTC().Format(time.RFC3339), Labels: []string{}, Comments: []reviewagent.TicketComment{}, Warnings: []string{}}
	for _, label := range issue.Labels[:min(20, len(issue.Labels))] {
		content.Labels = append(content.Labels, budget.take(label, 128))
	}
	if len(issue.Labels) > 20 {
		budget.truncated = true
	}
	content.Description = budget.document(issue.Description, 16*1024)
	content.AcceptanceCriteria = budget.document(issue.AcceptanceCriteria, 8*1024)
	for _, comment := range issue.Comments[:min(10, len(issue.Comments))] {
		content.Comments = append(content.Comments, reviewagent.TicketComment{Author: budget.take(comment.Author, 256), CreatedAt: budget.take(comment.CreatedAt, 128), Body: budget.document(comment.Body, 2048)})
	}
	if len(issue.Comments) > 10 {
		budget.truncated = true
	}
	if issue.CommentsError != "" {
		content.Warnings = append(content.Warnings, "Recent comments could not be loaded; the description is still available.")
	}
	content.Truncated = budget.truncated
	if content.Truncated {
		content.Warnings = append(content.Warnings, "Ticket content was truncated. Review the full requirements in the ticket provider.")
	}
	return content
}
