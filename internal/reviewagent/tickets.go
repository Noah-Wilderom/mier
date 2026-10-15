package reviewagent

import (
	"context"
	"errors"
)

const MaxTicketReads = 30
const MaxTicketTextBytes = 32 * 1024

// References are captured with the PR; issue contents are read on demand and
// remain mutable provider data, independent of the captured Git revision.
type TicketReference struct {
	Provider string   `json:"provider"`
	Key      string   `json:"key"`
	Sources  []string `json:"sources"`
}
type TicketComment struct {
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	Body      string `json:"body"`
}
type TicketContent struct {
	Provider           string          `json:"provider"`
	Key                string          `json:"key"`
	Summary            string          `json:"summary"`
	URL                string          `json:"url"`
	Status             string          `json:"status"`
	Type               string          `json:"type"`
	Assignee           string          `json:"assignee"`
	Priority           string          `json:"priority"`
	Labels             []string        `json:"labels"`
	ParentKey          string          `json:"parentKey"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptanceCriteria"`
	Comments           []TicketComment `json:"comments"`
	UpdatedAt          string          `json:"updatedAt"`
	ReadAt             string          `json:"readAt"`
	Truncated          bool            `json:"truncated"`
	Warnings           []string        `json:"warnings"`
}
type ReadTicket func(context.Context, string, string) (TicketContent, error)

func cloneTicketReferences(values []TicketReference) []TicketReference {
	result := append([]TicketReference{}, values...)
	for i := range result {
		result[i].Sources = append([]string{}, result[i].Sources...)
	}
	return result
}
func (r *Run) ticketAllowed(provider, key string) bool {
	for _, ticket := range r.context.Tickets {
		if ticket.Provider == provider && ticket.Key == key {
			return true
		}
	}
	return false
}
func (r *Run) ReadTicket(ctx context.Context, provider, key string) (TicketContent, error) {
	if err := ctx.Err(); err != nil {
		return TicketContent{}, err
	}
	r.mu.Lock()
	if terminal(r.snapshot.State) {
		r.mu.Unlock()
		return TicketContent{}, errors.New("This review is no longer active")
	}
	if !r.ticketAllowed(provider, key) || r.ticketReader == nil {
		r.mu.Unlock()
		return TicketContent{}, errors.New("Choose a provider and ticket key listed in get_review_context. Other tickets are unavailable")
	}
	r.ticketCalls++
	calls := r.ticketCalls
	r.mu.Unlock()
	if calls > MaxTicketReads {
		return TicketContent{}, errors.New("Ticket read limit reached. Complete the review using the context already read")
	}
	value, err := r.ticketReader(ctx, provider, key)
	if err != nil {
		return TicketContent{}, err
	}
	if err := ctx.Err(); err != nil {
		return TicketContent{}, err
	}
	r.mu.Lock()
	active := !terminal(r.snapshot.State)
	r.mu.Unlock()
	if !active {
		return TicketContent{}, errors.New("This review is no longer active")
	}
	if value.Provider != provider || value.Key != key || value.TextBytes() > MaxTicketTextBytes || len(value.Comments) > 10 {
		return TicketContent{}, errors.New("Ticket reader returned content outside this review's limits")
	}
	return value, nil
}
func (t TicketContent) TextBytes() int {
	count := 0
	for _, value := range []string{t.Summary, t.URL, t.Status, t.Type, t.Assignee, t.Priority, t.ParentKey, t.Description, t.AcceptanceCriteria, t.UpdatedAt} {
		count += len(value)
	}
	for _, label := range t.Labels {
		count += len(label)
	}
	for _, comment := range t.Comments {
		count += len(comment.Author) + len(comment.CreatedAt) + len(comment.Body)
	}
	return count
}
