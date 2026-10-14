package main

// Ticket content is shared by provider readers; provider authentication and
// repository preferences remain independent.
type TicketRepository struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}
type TicketPullRequest struct {
	Repository TicketRepository `json:"repository"`
	Number     int              `json:"number"`
	Branch     string           `json:"branch"`
	Title      string           `json:"title"`
	Body       string           `json:"body"`
	Refresh    bool             `json:"refresh"`
}
type TicketIssueRequest struct {
	Repository TicketRepository `json:"repository"`
	Key        string           `json:"key"`
	Refresh    bool             `json:"refresh"`
}
type TicketSearchRequest struct {
	Repository TicketRepository `json:"repository"`
	Query      string           `json:"query"`
}
type TicketSelection struct {
	PullRequest TicketPullRequest `json:"pullRequest"`
	Key         string            `json:"key"`
	Action      string            `json:"action"`
}
type TicketComment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	Body      any    `json:"body"`
}
type TicketIssue struct {
	ID                 string          `json:"id"`
	Key                string          `json:"key"`
	Summary            string          `json:"summary"`
	URL                string          `json:"url"`
	Type               string          `json:"type"`
	Status             string          `json:"status"`
	StatusCategory     string          `json:"statusCategory"`
	Assignee           string          `json:"assignee"`
	Priority           string          `json:"priority"`
	Labels             []string        `json:"labels"`
	ParentKey          string          `json:"parentKey"`
	Description        any             `json:"description"`
	AcceptanceCriteria any             `json:"acceptanceCriteria"`
	Comments           []TicketComment `json:"comments"`
	CommentsError      string          `json:"commentsError"`
	UpdatedAt          string          `json:"updatedAt"`
}
type Ticket struct {
	Key     string       `json:"key"`
	Sources []string     `json:"sources"`
	Issue   *TicketIssue `json:"issue"`
	Error   string       `json:"error"`
}
