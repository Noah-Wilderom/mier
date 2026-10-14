package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"mag-ik-een-review/internal/githubclient"
)

var jiraSiteID = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
var jiraIssueKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,18}$`)
var jiraKeyInText = regexp.MustCompile(`(?i)[A-Z][A-Z0-9_]{0,63}-[1-9][0-9]{0,18}`)
var jiraProjectKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var jiraCustomField = regexp.MustCompile(`^customfield_[0-9]{1,12}$`)

// Keep the existing Jira RPC contract while sharing the ticket reader model.
type JiraRepository = TicketRepository
type JiraPullRequest = TicketPullRequest
type JiraIssueRequest = TicketIssueRequest
type JiraSearchRequest = TicketSearchRequest
type JiraTicketSelection = TicketSelection
type JiraComment = TicketComment
type JiraIssue = TicketIssue
type JiraTicket = Ticket
type JiraTickets struct {
	Site        JiraSite                  `json:"site"`
	Preferences JiraRepositoryPreferences `json:"preferences"`
	Tickets     []JiraTicket              `json:"tickets"`
	Hidden      []string                  `json:"hidden"`
	Truncated   bool                      `json:"truncated"`
}
type jiraCachedIssue struct {
	issue   JiraIssue
	expires time.Time
}
type jiraConnection struct {
	site        JiraSite
	preferences JiraRepositoryPreferences
	epoch       uint64
}

func jiraRepositoryKey(repo JiraRepository) (string, error) {
	host, err := githubclient.NormalizeHost(repo.Host)
	if err != nil {
		return "", errors.New("Choose a GitHub repository before linking tickets")
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	if !valid.MatchString(repo.Owner) || !valid.MatchString(repo.Repo) || repo.Owner == "." || repo.Owner == ".." || repo.Repo == "." || repo.Repo == ".." {
		return "", errors.New("Invalid repository identity")
	}
	return strings.ToLower(host + "/" + repo.Owner + "/" + repo.Repo), nil
}
func jiraPRKey(request JiraPullRequest) (string, error) {
	key, err := jiraRepositoryKey(request.Repository)
	if err != nil {
		return "", err
	}
	if request.Number < 1 {
		return "", errors.New("Choose a pull request before linking tickets")
	}
	return key + "#" + strconv.Itoa(request.Number), nil
}
func (s *IntegrationService) jiraConnection(repo JiraRepository) (jiraConnection, error) {
	key, err := jiraRepositoryKey(repo)
	if err != nil {
		return jiraConnection{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.loadSecretsLocked(); err != nil {
		return jiraConnection{}, err
	}
	if s.secrets.RefreshToken == "" {
		return jiraConnection{}, errors.New("Connect Jira in Settings to view tickets for this pull request")
	}
	preferences := s.config.Repositories[key]
	preferences.Projects = append([]string{}, preferences.Projects...)
	id := preferences.SiteID
	if id == "" {
		id = s.config.DefaultSiteID
	}
	for _, site := range s.config.Sites {
		if site.ID == id && jiraSiteID.MatchString(site.ID) {
			return jiraConnection{site: site, preferences: preferences, epoch: s.epoch}, nil
		}
	}
	return jiraConnection{}, errors.New("Choose an authorized Jira site for this repository")
}
func (s *IntegrationService) SaveJiraRepositoryPreferences(ctx context.Context, repo JiraRepository, preferences JiraRepositoryPreferences) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := jiraRepositoryKey(repo)
	if err != nil {
		return err
	}
	if len(preferences.Projects) > 32 {
		return errors.New("Choose at most 32 Jira projects")
	}
	projects := []string{}
	seen := map[string]bool{}
	for _, project := range preferences.Projects {
		project = strings.ToUpper(strings.TrimSpace(project))
		if !jiraProjectKey.MatchString(project) {
			return errors.New("Enter Jira project keys, such as FAN, separated by commas")
		}
		if !seen[project] {
			projects = append(projects, project)
			seen[project] = true
		}
	}
	preferences.Projects = projects
	preferences.AcceptanceField = strings.TrimSpace(preferences.AcceptanceField)
	if preferences.AcceptanceField != "" && !jiraCustomField.MatchString(preferences.AcceptanceField) {
		return errors.New("Acceptance criteria field must use a Jira field ID such as customfield_10001")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	for _, site := range s.config.Sites {
		if site.ID == preferences.SiteID {
			known = true
		}
	}
	if !known {
		return errors.New("Choose an authorized Jira site")
	}
	next := s.config
	next.Repositories = map[string]JiraRepositoryPreferences{}
	for name, value := range s.config.Repositories {
		next.Repositories[name] = value
	}
	next.Repositories[key] = preferences
	if len(next.Repositories) > 200 {
		return errors.New("Jira repository settings have reached their limit")
	}
	if err = writeJiraConfiguration(s.configPath, next); err != nil {
		return err
	}
	s.config = next
	s.epoch++
	s.cache = map[string]jiraCachedIssue{}
	return nil
}
func (s *IntegrationService) SelectJiraTicket(ctx context.Context, input JiraTicketSelection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pr, err := jiraPRKey(input.PullRequest)
	if err != nil {
		return err
	}
	key := strings.ToUpper(strings.TrimSpace(input.Key))
	if !jiraIssueKey.MatchString(key) {
		return errors.New("Enter a Jira issue key such as FAN-2187")
	}
	if input.Action != "pin" && input.Action != "hide" && input.Action != "restore" {
		return errors.New("Unknown Jira ticket selection action")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	selection := s.config.Selections[pr]
	remove := func(values []string) []string {
		next := []string{}
		for _, value := range values {
			if value != key {
				next = append(next, value)
			}
		}
		return next
	}
	selection.Hidden = remove(selection.Hidden)
	if input.Action == "pin" {
		selection.Pinned = remove(selection.Pinned)
		selection.Pinned = append(selection.Pinned, key)
	}
	if input.Action == "hide" {
		selection.Hidden = append(selection.Hidden, key)
	}
	if len(selection.Pinned) > 25 || len(selection.Hidden) > 100 {
		return errors.New("This pull request has reached its Jira ticket selection limit")
	}
	next := s.config
	next.Selections = map[string]jiraSelection{}
	for name, value := range s.config.Selections {
		next.Selections[name] = value
	}
	next.Selections[pr] = selection
	if len(next.Selections) > 1000 {
		return errors.New("Saved Jira pull request selections have reached their limit")
	}
	if err = writeJiraConfiguration(s.configPath, next); err != nil {
		return err
	}
	s.config = next
	return nil
}
func jiraProjectAllowed(key string, projects []string) bool {
	if len(projects) == 0 {
		return true
	}
	prefix, _, _ := strings.Cut(key, "-")
	for _, project := range projects {
		if prefix == project {
			return true
		}
	}
	return false
}
func jiraDetectedTickets(request JiraPullRequest, selection jiraSelection, projects []string) ([]JiraTicket, bool) {
	result := []JiraTicket{}
	indices := map[string]int{}
	hidden := map[string]bool{}
	for _, key := range selection.Hidden {
		hidden[key] = true
	}
	truncated := false
	add := func(key, source string) {
		key = strings.ToUpper(key)
		if hidden[key] || !jiraIssueKey.MatchString(key) || !jiraProjectAllowed(key, projects) {
			return
		}
		if index, ok := indices[key]; ok {
			for _, existing := range result[index].Sources {
				if existing == source {
					return
				}
			}
			result[index].Sources = append(result[index].Sources, source)
			return
		}
		if len(result) >= 25 {
			truncated = true
			return
		}
		indices[key] = len(result)
		result = append(result, JiraTicket{Key: key, Sources: []string{source}})
	}
	for _, key := range selection.Pinned {
		add(key, "manual")
	}
	for _, part := range []struct{ value, source string }{{request.Branch, "branch"}, {request.Title, "title"}, {request.Body, "description"}} {
		for _, index := range jiraKeyInText.FindAllStringIndex(part.value, 100) {
			// Do not match a key suffix within another word or a partial issue number.
			boundary := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
			before, _ := utf8.DecodeLastRuneInString(part.value[:index[0]])
			after, _ := utf8.DecodeRuneInString(part.value[index[1]:])
			if index[0] > 0 && boundary(before) || index[1] < len(part.value) && boundary(after) {
				continue
			}
			add(part.value[index[0]:index[1]], part.source)
		}
	}
	return result, truncated
}
func (s *IntegrationService) GetJiraTickets(ctx context.Context, request JiraPullRequest) (JiraTickets, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	pr, err := jiraPRKey(request)
	if err != nil {
		return JiraTickets{}, err
	}
	if len(request.Title) > 4096 || len(request.Branch) > 1024 || len(request.Body) > 128*1024 {
		return JiraTickets{}, errors.New("Pull request text is too large to scan for Jira tickets")
	}
	connection, err := s.jiraConnection(request.Repository)
	if err != nil {
		return JiraTickets{}, err
	}
	s.mu.Lock()
	selection := s.config.Selections[pr]
	selection.Pinned = append([]string{}, selection.Pinned...)
	selection.Hidden = append([]string{}, selection.Hidden...)
	s.mu.Unlock()
	tickets, truncated := jiraDetectedTickets(request, selection, connection.preferences.Projects)
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for index := range tickets {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				tickets[index].Error = "Jira request was cancelled"
				return
			}
			defer func() { <-limit }()
			issue, err := s.fetchJiraIssue(ctx, connection, tickets[index].Key, false, request.Refresh)
			if err != nil {
				tickets[index].Error = err.Error()
			} else {
				tickets[index].Issue = &issue
			}
		}(index)
	}
	workers.Wait()
	if err = ctx.Err(); err != nil {
		return JiraTickets{}, err
	}
	s.mu.Lock()
	changed := s.epoch != connection.epoch
	s.mu.Unlock()
	if changed {
		return JiraTickets{}, errors.New("Jira settings changed. Reload tickets for this repository.")
	}
	return JiraTickets{Site: connection.site, Preferences: connection.preferences, Tickets: tickets, Hidden: selection.Hidden, Truncated: truncated}, nil
}
func (s *IntegrationService) ReadJiraIssue(ctx context.Context, request JiraIssueRequest) (JiraIssue, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	key := strings.ToUpper(strings.TrimSpace(request.Key))
	if !jiraIssueKey.MatchString(key) {
		return JiraIssue{}, errors.New("Enter a valid Jira ticket key")
	}
	connection, err := s.jiraConnection(request.Repository)
	if err != nil {
		return JiraIssue{}, err
	}
	if !jiraProjectAllowed(key, connection.preferences.Projects) {
		return JiraIssue{}, errors.New("This ticket is outside the configured Jira projects")
	}
	return s.fetchJiraIssue(ctx, connection, key, true, request.Refresh)
}
func jiraDocument(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if len(raw) > 64*1024 {
		return "This content is too large to display. Open the ticket in Jira."
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}
func (s *IntegrationService) fetchJiraIssue(ctx context.Context, connection jiraConnection, key string, details, refresh bool) (JiraIssue, error) {
	cacheKey := fmt.Sprintf("%d/%s/%s/%t/%s", connection.epoch, connection.site.ID, key, details, connection.preferences.AcceptanceField)
	s.mu.Lock()
	if s.epoch != connection.epoch || s.closed {
		s.mu.Unlock()
		return JiraIssue{}, errors.New("Jira settings changed. Retry the request.")
	}
	cached, found := s.cache[cacheKey]
	s.mu.Unlock()
	if found && !refresh && cached.expires.After(time.Now()) {
		return cached.issue, nil
	}
	fields := "summary,status,issuetype,assignee,priority,labels,parent,updated,project"
	if details {
		fields += ",description"
		if connection.preferences.AcceptanceField != "" {
			fields += "," + connection.preferences.AcceptanceField
		}
	}
	base := "https://api.atlassian.com/ex/jira/" + connection.site.ID + "/rest/api/3"
	var response struct {
		ID     string                     `json:"id"`
		Key    string                     `json:"key"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := s.jiraAPI(ctx, base+"/issue/"+key+"?fields="+url.QueryEscape(fields), nil, &response); err != nil {
		return JiraIssue{}, err
	}
	if response.ID == "" || !jiraIssueKey.MatchString(response.Key) || !jiraProjectAllowed(response.Key, connection.preferences.Projects) {
		return JiraIssue{}, errors.New("Jira returned a ticket outside this repository's configured projects")
	}
	text := func(name string) string {
		var value string
		_ = json.Unmarshal(response.Fields[name], &value)
		if len(value) > 8192 {
			value = value[:8192]
		}
		return strings.ToValidUTF8(value, "")
	}
	name := func(field string) string {
		var value struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(response.Fields[field], &value)
		return value.Name
	}
	issue := JiraIssue{ID: response.ID, Key: response.Key, Summary: text("summary"), URL: connection.site.URL + "/browse/" + response.Key, Type: name("issuetype"), Status: name("status"), Priority: name("priority"), UpdatedAt: text("updated"), Labels: []string{}, Comments: []JiraComment{}}
	var status struct {
		StatusCategory struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	_ = json.Unmarshal(response.Fields["status"], &status)
	issue.StatusCategory = status.StatusCategory.Key
	var assignee struct {
		DisplayName string `json:"displayName"`
	}
	_ = json.Unmarshal(response.Fields["assignee"], &assignee)
	issue.Assignee = assignee.DisplayName
	var parent struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(response.Fields["parent"], &parent)
	issue.ParentKey = parent.Key
	_ = json.Unmarshal(response.Fields["labels"], &issue.Labels)
	if len(issue.Labels) > 50 {
		issue.Labels = issue.Labels[:50]
	}
	if details {
		issue.Description = jiraDocument(response.Fields["description"])
		issue.AcceptanceCriteria = jiraDocument(response.Fields[connection.preferences.AcceptanceField])
		var comments struct {
			Comments []struct {
				ID      string          `json:"id"`
				Created string          `json:"created"`
				Body    json.RawMessage `json:"body"`
				Author  struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
			} `json:"comments"`
		}
		commentsCtx, cancelComments := context.WithTimeout(ctx, 5*time.Second)
		if err := s.jiraAPI(commentsCtx, base+"/issue/"+response.Key+"/comment?maxResults=10&orderBy=-created", nil, &comments); err != nil {
			issue.CommentsError = err.Error()
			if errors.Is(err, context.DeadlineExceeded) {
				issue.CommentsError = "Recent comments took too long to load. Retry the ticket or open Jira."
			}
		} else {
			for index, comment := range comments.Comments {
				if index >= 10 {
					break
				}
				issue.Comments = append(issue.Comments, JiraComment{ID: comment.ID, CreatedAt: comment.Created, Author: comment.Author.DisplayName, Body: jiraDocument(comment.Body)})
			}
		}
		cancelComments()
	}
	if err := ctx.Err(); err != nil {
		return JiraIssue{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != connection.epoch || s.closed {
		return JiraIssue{}, errors.New("Jira settings changed. Retry the request.")
	}
	if len(s.cache) >= 64 {
		oldestKey := ""
		var oldest time.Time
		for name, value := range s.cache {
			if oldestKey == "" || value.expires.Before(oldest) {
				oldestKey = name
				oldest = value.expires
			}
		}
		delete(s.cache, oldestKey)
	}
	s.cache[cacheKey] = jiraCachedIssue{issue: issue, expires: time.Now().Add(2 * time.Minute)}
	return issue, nil
}
func jqlString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
func (s *IntegrationService) SearchJiraIssues(ctx context.Context, request JiraSearchRequest) ([]JiraIssue, error) {
	ctx, cancel := s.requestContext(ctx)
	defer cancel()
	query := strings.TrimSpace(request.Query)
	if len(query) < 2 || len(query) > 120 || strings.ContainsAny(query, "\r\n\x00") {
		return nil, errors.New("Search with 2 to 120 characters")
	}
	connection, err := s.jiraConnection(request.Repository)
	if err != nil {
		return nil, err
	}
	if jiraIssueKey.MatchString(strings.ToUpper(query)) {
		issue, err := s.fetchJiraIssue(ctx, connection, strings.ToUpper(query), false, false)
		if err != nil {
			return nil, err
		}
		return []JiraIssue{issue}, nil
	}
	// Escape Lucene's syntax as well as JQL's outer string literal.
	var escaped strings.Builder
	for _, char := range query {
		if strings.ContainsRune(`+-!(){}[]^"~*?:\/&|`, char) {
			escaped.WriteRune('\\')
		}
		escaped.WriteRune(char)
	}
	jql := "summary ~ " + jqlString(`"`+escaped.String()+`"`)
	if len(connection.preferences.Projects) > 0 {
		values := []string{}
		for _, project := range connection.preferences.Projects {
			values = append(values, jqlString(project))
		}
		jql += " AND project in (" + strings.Join(values, ",") + ")"
	}
	jql += " ORDER BY updated DESC"
	var result struct {
		Issues []struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err = s.jiraAPI(ctx, "https://api.atlassian.com/ex/jira/"+connection.site.ID+"/rest/api/3/search/jql", map[string]any{"jql": jql, "maxResults": 15, "fields": []string{"summary"}}, &result); err != nil {
		return nil, err
	}
	issues := []JiraIssue{}
	for index, item := range result.Issues {
		if index >= 15 {
			break
		}
		if jiraIssueKey.MatchString(item.Key) && jiraProjectAllowed(item.Key, connection.preferences.Projects) {
			issues = append(issues, JiraIssue{ID: item.ID, Key: item.Key, Summary: item.Fields.Summary, URL: connection.site.URL + "/browse/" + item.Key})
		}
	}
	s.mu.Lock()
	changed := s.epoch != connection.epoch
	s.mu.Unlock()
	if changed {
		return nil, errors.New("Jira settings changed. Search again.")
	}
	return issues, nil
}
