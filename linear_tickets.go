package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type LinearTickets struct {
	Workspace   LinearWorkspace             `json:"workspace"`
	Preferences LinearRepositoryPreferences `json:"preferences"`
	Tickets     []Ticket                    `json:"tickets"`
	Hidden      []string                    `json:"hidden"`
	Truncated   bool                        `json:"truncated"`
}
type linearConnection struct {
	workspace   LinearWorkspace
	preferences LinearRepositoryPreferences
	epoch       uint64
}
type linearIssue struct {
	ID, Identifier, Title, URL, UpdatedAt, Description, PriorityLabel string
	State                                                             struct{ Name, Type string }
	Team                                                              struct{ Key, Name string }
	Assignee                                                          *struct{ Name string }
	Parent                                                            *struct{ Identifier string }
	Project, Cycle                                                    *struct{ Name string }
	Labels                                                            struct{ Nodes []struct{ Name string } }
}

// Queries use the published Linear schema. Only detail reads request large
// Markdown fields; comments have their own timeout and failure state.
const linearSummaryFields = `id identifier title url updatedAt state { name type } assignee { name } team { key name }`
const linearDetailFields = `description priorityLabel parent { identifier } project { name } cycle { name } labels(first: 50) { nodes { name } }`

func (s *LinearService) linearConnection(repo TicketRepository) (linearConnection, string, error) {
	key, err := jiraRepositoryKey(repo)
	if err != nil {
		return linearConnection{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return linearConnection{}, "", errors.New("Linear integration has shut down")
	}
	if err := s.loadSecretsLocked(); err != nil {
		return linearConnection{}, "", err
	}
	if s.config.Workspace == nil || s.secrets.RefreshToken == "" {
		return linearConnection{}, "", errors.New("Connect Linear in Settings to view tickets for this pull request")
	}
	key = s.config.Workspace.ID + "/" + key
	prefs := s.config.Repositories[key]
	prefs.Teams = append([]string{}, prefs.Teams...)
	return linearConnection{workspace: *s.config.Workspace, preferences: prefs, epoch: s.epoch}, key, nil
}

func (s *LinearService) SaveLinearRepositoryPreferences(ctx context.Context, repo TicketRepository, preferences LinearRepositoryPreferences) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	connection, key, err := s.linearConnection(repo)
	if err != nil {
		return err
	}
	if len(preferences.Teams) > 32 {
		return errors.New("Choose at most 32 Linear teams")
	}
	teams, seen := []string{}, map[string]bool{}
	for _, name := range preferences.Teams {
		name = strings.ToUpper(strings.TrimSpace(name))
		if !jiraProjectKey.MatchString(name) {
			return errors.New("Enter Linear team keys, such as ENG, separated by commas")
		}
		if !seen[name] {
			teams, seen[name] = append(teams, name), true
		}
	}
	preferences.Teams = teams
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.epoch != connection.epoch {
		return errors.New("Linear settings changed. Retry the request.")
	}
	next := s.config
	next.Repositories = map[string]LinearRepositoryPreferences{}
	for name, value := range s.config.Repositories {
		next.Repositories[name] = value
	}
	next.Repositories[key] = preferences
	if len(next.Repositories) > 200 {
		return errors.New("Linear repository settings have reached their limit")
	}
	if err := writeLinearConfiguration(s.configPath, next); err != nil {
		return err
	}
	s.config = next
	s.epoch++
	s.cache = map[string]linearCachedIssue{}
	return nil
}

func (s *LinearService) SelectLinearTicket(ctx context.Context, input TicketSelection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pr, err := jiraPRKey(input.PullRequest)
	if err != nil {
		return err
	}
	connection, _, err := s.linearConnection(input.PullRequest.Repository)
	if err != nil {
		return err
	}
	key := strings.ToUpper(strings.TrimSpace(input.Key))
	if !jiraIssueKey.MatchString(key) || !jiraProjectAllowed(key, connection.preferences.Teams) {
		return errors.New("Enter an issue key from this repository's Linear teams, such as ENG-123")
	}
	if input.Action != "pin" && input.Action != "hide" && input.Action != "restore" {
		return errors.New("Unknown Linear ticket selection action")
	}
	pr = connection.workspace.ID + "/" + pr
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.epoch != connection.epoch {
		return errors.New("Linear settings changed. Retry the request.")
	}
	selection := s.config.Selections[pr]
	pinned, hidden := []string{}, []string{}
	for _, value := range selection.Pinned {
		if value != key {
			pinned = append(pinned, value)
		}
	}
	for _, value := range selection.Hidden {
		if value != key {
			hidden = append(hidden, value)
		}
	}
	if input.Action == "pin" {
		pinned = append(pinned, key)
	} else {
		// Hidden manual references stay pinned so Restore returns them too.
		pinned = append([]string{}, selection.Pinned...)
	}
	if input.Action == "hide" {
		hidden = append(hidden, key)
	}
	if len(pinned) > 25 || len(hidden) > 100 {
		return errors.New("This pull request has reached its Linear ticket selection limit")
	}
	next := s.config
	next.Selections = map[string]jiraSelection{}
	for name, value := range s.config.Selections {
		next.Selections[name] = value
	}
	next.Selections[pr] = jiraSelection{Pinned: pinned, Hidden: hidden}
	if len(next.Selections) > 1000 {
		return errors.New("Saved Linear pull request selections have reached their limit")
	}
	if err := writeLinearConfiguration(s.configPath, next); err != nil {
		return err
	}
	s.config = next
	return nil
}

func (s *LinearService) GetLinearTickets(ctx context.Context, request TicketPullRequest) (LinearTickets, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	pr, err := jiraPRKey(request)
	if err != nil {
		return LinearTickets{}, err
	}
	if len(request.Title) > 4096 || len(request.Branch) > 1024 || len(request.Body) > 128*1024 {
		return LinearTickets{}, errors.New("Pull request text is too large to scan for Linear tickets")
	}
	connection, _, err := s.linearConnection(request.Repository)
	if err != nil {
		return LinearTickets{}, err
	}
	s.mu.Lock()
	selection := s.config.Selections[connection.workspace.ID+"/"+pr]
	selection.Pinned = append([]string{}, selection.Pinned...)
	selection.Hidden = append([]string{}, selection.Hidden...)
	s.mu.Unlock()
	tickets, truncated := jiraDetectedTickets(request, selection, connection.preferences.Teams)
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for index := range tickets {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				tickets[index].Error = "Linear request was cancelled"
				return
			}
			defer func() { <-limit }()
			issue, err := s.fetchLinearIssue(ctx, connection, tickets[index].Key, false, request.Refresh)
			if err != nil {
				tickets[index].Error = err.Error()
			} else {
				tickets[index].Issue = &issue
			}
		}(index)
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return LinearTickets{}, err
	}
	if !s.linearCurrent(connection) {
		return LinearTickets{}, errors.New("Linear settings changed. Reload tickets for this repository.")
	}
	return LinearTickets{Workspace: connection.workspace, Preferences: connection.preferences, Tickets: tickets, Hidden: selection.Hidden, Truncated: truncated}, nil
}

func (s *LinearService) ReadLinearIssue(ctx context.Context, request TicketIssueRequest) (TicketIssue, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	key := strings.ToUpper(strings.TrimSpace(request.Key))
	if !jiraIssueKey.MatchString(key) {
		return TicketIssue{}, errors.New("Enter a valid Linear ticket key")
	}
	connection, _, err := s.linearConnection(request.Repository)
	if err != nil {
		return TicketIssue{}, err
	}
	if !jiraProjectAllowed(key, connection.preferences.Teams) {
		return TicketIssue{}, errors.New("This ticket is outside the configured Linear teams")
	}
	return s.fetchLinearIssue(ctx, connection, key, true, request.Refresh)
}

func boundedLinearText(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	return strings.ToValidUTF8(value, "")
}
func linearMarkdown(value string) string {
	if len(value) > 64*1024 {
		return "This content is too large to display. Open the ticket in Linear."
	}
	return strings.ToValidUTF8(value, "")
}
func linearTicket(raw *linearIssue, connection linearConnection) (TicketIssue, error) {
	if raw == nil {
		return TicketIssue{}, &linearAPIError{status: 404}
	}
	u, err := url.Parse(raw.URL)
	if raw.ID == "" || len(raw.ID) > 128 || !jiraIssueKey.MatchString(raw.Identifier) || !jiraProjectAllowed(raw.Identifier, connection.preferences.Teams) || err != nil || u.Scheme != "https" || u.Host != "linear.app" || u.User != nil || len(raw.URL) > 4096 {
		return TicketIssue{}, errors.New("Linear returned invalid ticket information for this repository")
	}
	issue := TicketIssue{ID: raw.ID, Key: raw.Identifier, Summary: boundedLinearText(raw.Title, 8192), URL: raw.URL, Type: boundedLinearText(raw.Team.Name, 1024), Status: boundedLinearText(raw.State.Name, 1024), StatusCategory: "new", Priority: boundedLinearText(raw.PriorityLabel, 1024), UpdatedAt: boundedLinearText(raw.UpdatedAt, 128), Description: linearMarkdown(raw.Description), Labels: []string{}, Comments: []TicketComment{}}
	switch raw.State.Type {
	case "started":
		issue.StatusCategory = "indeterminate"
	case "completed":
		issue.StatusCategory = "done"
	case "canceled":
		issue.StatusCategory = "canceled"
	}
	if raw.Assignee != nil {
		issue.Assignee = boundedLinearText(raw.Assignee.Name, 1024)
	}
	if raw.Parent != nil && jiraIssueKey.MatchString(raw.Parent.Identifier) {
		issue.ParentKey = raw.Parent.Identifier
	}
	for _, extra := range []*struct{ Name string }{raw.Project, raw.Cycle} {
		if extra != nil && extra.Name != "" {
			issue.Type += " · " + boundedLinearText(extra.Name, 1024)
		}
	}
	for _, label := range raw.Labels.Nodes[:min(50, len(raw.Labels.Nodes))] {
		issue.Labels = append(issue.Labels, boundedLinearText(label.Name, 1024))
	}
	return issue, nil
}

func (s *LinearService) linearCurrent(connection linearConnection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.epoch == connection.epoch
}
func (s *LinearService) fetchLinearIssue(ctx context.Context, connection linearConnection, key string, details, refresh bool) (TicketIssue, error) {
	cacheKey := fmt.Sprintf("%d/%s/%t", connection.epoch, key, details)
	s.mu.Lock()
	if s.closed || s.epoch != connection.epoch {
		s.mu.Unlock()
		return TicketIssue{}, errors.New("Linear settings changed. Retry the request.")
	}
	cached, found := s.cache[cacheKey]
	s.mu.Unlock()
	if found && !refresh && cached.expires.After(time.Now()) {
		return cached.issue, nil
	}
	fields := linearSummaryFields
	if details {
		fields += " " + linearDetailFields
	}
	var result struct{ Issue *linearIssue }
	if err := s.linearGraphQL(ctx, "query MierLinearIssue($id: String!) { issue(id: $id) { "+fields+" } }", map[string]any{"id": key}, &result); err != nil {
		return TicketIssue{}, err
	}
	issue, err := linearTicket(result.Issue, connection)
	if err != nil {
		return TicketIssue{}, err
	}
	if details {
		commentCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var result struct {
			Issue *struct {
				Comments struct {
					Nodes []struct {
						ID, Body, CreatedAt string
						User                *struct{ Name string }
					}
				}
			}
		}
		err := s.linearGraphQL(commentCtx, "query MierLinearComments($id: String!) { issue(id: $id) { comments(last: 10) { nodes { id body createdAt user { name } } } } }", map[string]any{"id": issue.ID}, &result)
		cancel()
		if err != nil {
			issue.CommentsError = "Recent comments could not be loaded. Refresh the ticket or open it in Linear."
		} else if result.Issue != nil {
			for _, item := range result.Issue.Comments.Nodes[:min(10, len(result.Issue.Comments.Nodes))] {
				author := "Unknown author"
				if item.User != nil && item.User.Name != "" {
					author = boundedLinearText(item.User.Name, 1024)
				}
				issue.Comments = append(issue.Comments, TicketComment{ID: boundedLinearText(item.ID, 128), Author: author, Body: linearMarkdown(item.Body), CreatedAt: boundedLinearText(item.CreatedAt, 128)})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return TicketIssue{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.epoch != connection.epoch {
		return TicketIssue{}, errors.New("Linear settings changed. Retry the request.")
	}
	if len(s.cache) >= 64 {
		for name := range s.cache {
			delete(s.cache, name)
			break
		}
	}
	s.cache[cacheKey] = linearCachedIssue{issue: issue, expires: time.Now().Add(2 * time.Minute)}
	return issue, nil
}

func (s *LinearService) SearchLinearIssues(ctx context.Context, request TicketSearchRequest) ([]TicketIssue, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	query := strings.TrimSpace(request.Query)
	if len(query) < 2 || len(query) > 120 {
		return nil, errors.New("Enter 2 to 120 characters to search Linear tickets")
	}
	connection, _, err := s.linearConnection(request.Repository)
	if err != nil {
		return nil, err
	}
	key := strings.ToUpper(query)
	if jiraIssueKey.MatchString(key) {
		if !jiraProjectAllowed(key, connection.preferences.Teams) {
			return []TicketIssue{}, nil
		}
		issue, err := s.fetchLinearIssue(ctx, connection, key, false, false)
		if err != nil {
			return nil, err
		}
		return []TicketIssue{issue}, nil
	}
	filter := map[string]any{}
	if len(connection.preferences.Teams) > 0 {
		filter["team"] = map[string]any{"key": map[string]any{"in": connection.preferences.Teams}}
	}
	var result struct {
		SearchIssues struct{ Nodes []*linearIssue }
	}
	if err := s.linearGraphQL(ctx, "query MierLinearSearch($term: String!, $filter: IssueFilter) { searchIssues(term: $term, first: 15, filter: $filter, includeArchived: false) { nodes { "+linearSummaryFields+" } } }", map[string]any{"term": query, "filter": filter}, &result); err != nil {
		return nil, err
	}
	issues := []TicketIssue{}
	for _, raw := range result.SearchIssues.Nodes[:min(15, len(result.SearchIssues.Nodes))] {
		issue, err := linearTicket(raw, connection)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if !s.linearCurrent(connection) {
		return nil, errors.New("Linear settings changed. Retry the search.")
	}
	return issues, nil
}
