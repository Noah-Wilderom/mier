package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type jiraTransport func(*http.Request) (*http.Response, error)

func (f jiraTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type jiraHarness struct {
	service   *IntegrationService
	store     *memorySecrets
	mu        sync.Mutex
	calls     []string
	refreshes atomic.Int32
	handler   func(http.ResponseWriter, *http.Request) bool
}

func newJiraHarness(t *testing.T) *jiraHarness {
	t.Helper()
	h := &jiraHarness{store: &memorySecrets{values: map[string]string{}}}
	client := &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
		h.mu.Lock()
		h.calls = append(h.calls, r.URL.String())
		h.mu.Unlock()
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		if h.handler != nil && h.handler(w, r) {
			return w.Result(), nil
		}
		switch {
		case r.URL.Host == "auth.atlassian.com" && r.URL.Path == "/oauth/token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_secret"] != "fixture-client-secret" || body["client_id"] != "fixture-client" {
				t.Errorf("incorrect OAuth credentials")
			}
			if body["grant_type"] == "refresh_token" {
				h.refreshes.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh-" + strconv.Itoa(int(h.refreshes.Load())), "expires_in": 3600, "scope": "read:jira-work offline_access"})
		case r.URL.Host == "api.atlassian.com" && r.URL.Path == "/oauth/token/accessible-resources":
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": "site-a", "name": "Team Jira", "url": "https://team.atlassian.net", "scopes": []string{"read:jira-work"}}, map[string]any{"id": "site-b", "name": "Other Jira", "url": "https://other.atlassian.net", "scopes": []string{"read:jira-work"}}})
		case strings.Contains(r.URL.Path, "/issue/") && strings.HasSuffix(r.URL.Path, "/comment"):
			if r.URL.Query().Get("orderBy") != "-created" {
				t.Error("comments did not request most recent first")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"comments": []any{map[string]any{"id": "comment-1", "author": map[string]string{"displayName": "Reviewer"}, "created": "2026-10-14T12:00:00Z", "body": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]string{"type": "text", "text": "Please check the acceptance criteria."}}}}}}}})
		case strings.Contains(r.URL.Path, "/issue/"):
			if r.Header.Get("Authorization") != "Bearer fixture-access" {
				t.Errorf("missing Jira bearer token")
			}
			key := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			if key == "FAN-404" {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"message":"fixture-client-secret"}`)
				break
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "issue-" + key, "key": key, "fields": map[string]any{"summary": "Review requirements for " + key, "status": map[string]any{"name": "In Progress", "statusCategory": map[string]string{"key": "indeterminate"}}, "issuetype": map[string]string{"name": "Story"}, "priority": map[string]string{"name": "High"}, "assignee": map[string]string{"displayName": "Developer"}, "description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]string{"type": "text", "text": "Keep the original source unchanged."}}}}}, "customfield_10001": "Hover and definitions work.", "labels": []string{"review"}}})
		case strings.HasSuffix(r.URL.Path, "/search/jql"):
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{map[string]any{"id": "issue-FAN-7", "key": "FAN-7", "fields": map[string]string{"summary": "Found ticket"}}}})
		default:
			t.Errorf("unexpected Jira request %s", r.URL)
			w.WriteHeader(500)
		}
		return w.Result(), nil
	})}
	h.service = newJiraIntegration(filepath.Join(t.TempDir(), "jira.json"), h.store, client)
	t.Cleanup(func() { _ = h.service.ServiceShutdown() })
	return h
}
func (h *jiraHarness) configure(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if err = h.service.ConfigureJira(context.Background(), JiraAppConfiguration{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", CallbackPort: port}); err != nil {
		t.Fatal(err)
	}
}
func (h *jiraHarness) authorize(t *testing.T) {
	t.Helper()
	h.configure(t)
	address, err := h.service.BeginJiraConnection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(address)
	values := u.Query()
	if values.Get("client_secret") != "" || values.Get("scope") != "read:jira-work offline_access" || len(values.Get("state")) < 40 {
		t.Fatal("unsafe or incomplete authorization URL")
	}
	callback := values.Get("redirect_uri") + "?" + url.Values{"state": {values.Get("state")}, "code": {"fixture-code"}}.Encode()
	response, err := http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("OAuth callback failed: %d", response.StatusCode)
	}
	status, err := h.service.GetJiraStatus(context.Background())
	if err != nil || !status.Connected || status.State != "idle" {
		t.Fatalf("authorization not verified: %+v %v", status, err)
	}
}
func fixtureJiraPR(number int) JiraPullRequest {
	return JiraPullRequest{Repository: JiraRepository{Host: "github.com", Owner: "team", Repo: "app"}, Number: number, Branch: "FAN-2187-update-calcasa", Title: "FAN-2187: review improvement", Body: "Also see FAN-19 and FAN-404."}
}

func TestJiraOAuthCredentialsStorageAndState(t *testing.T) {
	h := newJiraHarness(t)
	h.configure(t)
	address, err := h.service.BeginJiraConnection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.BeginJiraConnection(context.Background()); err == nil {
		t.Fatal("duplicate authorization started")
	}
	u, _ := url.Parse(address)
	query := u.Query()
	bad, err := http.Get(query.Get("redirect_uri") + "?state=wrong&code=ignored")
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.Body.Close()
	if bad.StatusCode != 400 {
		t.Fatal("invalid state accepted")
	}
	h.mu.Lock()
	calls := len(h.calls)
	h.mu.Unlock()
	if calls != 0 {
		t.Fatal("invalid callback reached OAuth provider")
	}
	response, err := http.Get(query.Get("redirect_uri") + "?" + url.Values{"state": {query.Get("state")}, "code": {"fixture-code"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("valid authorization failed")
	}
	status, _ := h.service.GetJiraStatus(context.Background())
	encoded, _ := json.Marshal(status)
	data, err := os.ReadFile(h.service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"fixture-client-secret", "fixture-access", "fixture-refresh"} {
		if strings.Contains(string(encoded), value) || strings.Contains(string(data), value) {
			t.Fatal("secret escaped keychain")
		}
	}
	info, _ := os.Stat(h.service.configPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions are not private")
	}
	reopened := newJiraIntegration(h.service.configPath, h.store, h.service.client)
	defer reopened.ServiceShutdown()
	saved, _ := reopened.GetJiraStatus(context.Background())
	if !saved.Connected || len(saved.Sites) != 2 {
		t.Fatal("connection did not survive restart")
	}
}
func TestJiraOAuthCancelFailureAndShutdown(t *testing.T) {
	for _, action := range []string{"cancel", "decline", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			h := newJiraHarness(t)
			h.configure(t)
			address, err := h.service.BeginJiraConnection(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(address)
			if action == "cancel" {
				h.service.CancelJiraConnection()
			}
			if action == "shutdown" {
				_ = h.service.ServiceShutdown()
			}
			if action == "decline" {
				response, err := http.Get(u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "error": {"access_denied"}, "error_description": {"fixture-client-secret"}}.Encode())
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
			}
			status, _ := h.service.GetJiraStatus(context.Background())
			if status.Connected || strings.Contains(status.Error, "fixture-client-secret") {
				t.Fatal("unsuccessful authorization reported success or exposed provider text")
			}
			if action == "decline" && status.State != "failed" {
				t.Fatal("decline lost its state")
			}
		})
	}
}
func TestJiraConfigureKeychainFailureAndRepair(t *testing.T) {
	h := newJiraHarness(t)
	h.store.fail = true
	if err := h.service.ConfigureJira(context.Background(), JiraAppConfiguration{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", CallbackPort: 43827}); err == nil {
		t.Fatal("keychain failure hidden")
	}
	if _, err := os.Stat(h.service.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed keychain write persisted configuration")
	}
	h.store.fail = false
	h.configure(t)
	h.store.mu.Lock()
	h.store.values[jiraKeyringService+":fixture-client"] = "invalid-json"
	h.store.mu.Unlock()
	reopened := newJiraIntegration(h.service.configPath, h.store, h.service.client)
	defer reopened.ServiceShutdown()
	if err := reopened.ConfigureJira(context.Background(), JiraAppConfiguration{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", CallbackPort: 43827}); err != nil {
		t.Fatalf("corrupt credentials could not be repaired: %v", err)
	}
}
func TestJiraTicketDetectionMappingCacheAndDetails(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	request := fixtureJiraPR(12)
	result, err := h.service.GetJiraTickets(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tickets) != 3 || !reflect.DeepEqual(result.Tickets[0].Sources, []string{"branch", "title"}) || result.Tickets[2].Issue != nil || strings.Contains(result.Tickets[2].Error, "fixture-client-secret") {
		t.Fatalf("wrong ticket matches: %+v", result)
	}
	h.mu.Lock()
	count := len(h.calls)
	h.mu.Unlock()
	_, err = h.service.GetJiraTickets(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	cachedCount := len(h.calls)
	h.mu.Unlock()
	if cachedCount != count+1 {
		t.Fatal("successful summaries were not cached; only the missing ticket should retry")
	}
	if err = h.service.SaveJiraRepositoryPreferences(context.Background(), request.Repository, JiraRepositoryPreferences{SiteID: "site-b", Projects: []string{"fan"}, AcceptanceField: "customfield_10001"}); err != nil {
		t.Fatal(err)
	}
	issue, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: request.Repository, Key: "FAN-2187"})
	if err != nil {
		t.Fatal(err)
	}
	if issue.URL != "https://other.atlassian.net/browse/FAN-2187" || issue.AcceptanceCriteria != "Hover and definitions work." || issue.Description == nil || len(issue.Comments) != 1 {
		t.Fatalf("incomplete ticket detail: %+v", issue)
	}
	if _, err = h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: request.Repository, Key: "OTHER-1"}); err == nil {
		t.Fatal("project restriction ignored")
	}
	if _, err = h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: request.Repository, Key: "../../secret"}); err == nil {
		t.Fatal("invalid issue path accepted")
	}
}
func TestJiraPinnedHiddenSelectionsAndBoundaries(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	request := fixtureJiraPR(12)
	for _, input := range []JiraTicketSelection{{PullRequest: request, Key: "FAN-19", Action: "hide"}, {PullRequest: request, Key: "FAN-7", Action: "pin"}} {
		if err := h.service.SelectJiraTicket(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	result, err := h.service.GetJiraTickets(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tickets[0].Key != "FAN-7" || len(result.Hidden) != 1 {
		t.Fatal("manual selections were lost")
	}
	other, err := h.service.GetJiraTickets(context.Background(), fixtureJiraPR(13))
	if err != nil {
		t.Fatal(err)
	}
	if other.Tickets[0].Key == "FAN-7" || len(other.Hidden) != 0 {
		t.Fatal("selection leaked to another PR")
	}
	for _, action := range []string{"hide", "restore"} {
		if err = h.service.SelectJiraTicket(context.Background(), JiraTicketSelection{PullRequest: request, Key: "FAN-7", Action: action}); err != nil {
			t.Fatal(err)
		}
		restored, loadErr := h.service.GetJiraTickets(context.Background(), request)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		found := false
		for _, ticket := range restored.Tickets {
			found = found || ticket.Key == "FAN-7"
		}
		if found != (action == "restore") {
			t.Fatal("hiding and restoring a manual ticket lost its saved selection")
		}
	}
	tickets, truncated := jiraDetectedTickets(JiraPullRequest{Branch: "feature/FAN-2187-new", Title: "fooFAN-7bar FAN-2", Body: "FAN-2187 éFAN-8 FAN-9x"}, jiraSelection{}, nil)
	if truncated || len(tickets) != 2 || tickets[0].Key != "FAN-2187" || tickets[1].Key != "FAN-2" {
		t.Fatalf("bad key boundaries: %+v", tickets)
	}
	var body strings.Builder
	for n := 1; n < 50; n++ {
		body.WriteString(" FAN-" + strconv.Itoa(n))
	}
	tickets, truncated = jiraDetectedTickets(JiraPullRequest{Body: body.String()}, jiraSelection{}, nil)
	if len(tickets) != 25 || !truncated {
		t.Fatal("detection was not bounded")
	}
}
func TestJiraRefreshIsSerializedAndRotatingTokenPersists(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	h.service.mu.Lock()
	h.service.secrets.ExpiresAt = time.Now().Add(-time.Minute)
	h.service.mu.Unlock()
	var workers sync.WaitGroup
	for n := 0; n < 8; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := h.service.jiraAccessToken(context.Background(), ""); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if h.refreshes.Load() != 1 {
		t.Fatalf("concurrent refreshes consumed rotating tokens: %d", h.refreshes.Load())
	}
	h.store.mu.Lock()
	saved := h.store.values[jiraKeyringService+":fixture-client"]
	h.store.mu.Unlock()
	if !strings.Contains(saved, "fixture-refresh-1") {
		t.Fatal("new rotating refresh token was not saved")
	}
}
func TestJiraSearchEscapesQueryAndUsesEnhancedAPI(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	repo := fixtureJiraPR(12).Repository
	if err := h.service.SaveJiraRepositoryPreferences(context.Background(), repo, JiraRepositoryPreferences{SiteID: "site-a", Projects: []string{"FAN"}}); err != nil {
		t.Fatal(err)
	}
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/search/jql") {
			return false
		}
		var input struct {
			JQL string `json:"jql"`
			Max int    `json:"maxResults"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if !strings.Contains(input.JQL, `project in ("FAN")`) || input.Max != 15 || !strings.Contains(input.JQL, `\"`) {
			t.Errorf("unsafe or unscoped JQL: %s", input.JQL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{map[string]any{"id": "7", "key": "FAN-7", "fields": map[string]string{"summary": "Found ticket"}}}})
		return true
	}
	items, err := h.service.SearchJiraIssues(context.Background(), JiraSearchRequest{Repository: repo, Query: `hello" OR project = OTHER`})
	if err != nil || len(items) != 1 {
		t.Fatalf("search failed: %+v %v", items, err)
	}
}
func TestJiraCooldownAndLateAccountResults(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	repo := fixtureJiraPR(12).Repository
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/issue/") {
			return false
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		return true
	}
	_, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: repo, Key: "FAN-1"})
	if err == nil {
		t.Fatal("rate limit was hidden")
	}
	h.mu.Lock()
	count := len(h.calls)
	h.mu.Unlock()
	_, _ = h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: repo, Key: "FAN-2"})
	h.mu.Lock()
	after := len(h.calls)
	h.mu.Unlock()
	if count != after {
		t.Fatal("Retry-After was not honored")
	}
	h.service.mu.Lock()
	h.service.retryAt = time.Time{}
	h.service.mu.Unlock()
	started, release := make(chan struct{}), make(chan struct{})
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.Contains(r.URL.Path, "/issue/") {
			return false
		}
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "key": "FAN-1", "fields": map[string]string{"summary": "Old account data"}})
		return true
	}
	result := make(chan error, 1)
	go func() {
		_, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: repo, Key: "FAN-1"})
		result <- err
	}()
	<-started
	if err = h.service.DisconnectJira(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-result; err == nil {
		t.Fatal("late account result survived disconnect")
	}
}

func TestJiraRotatedTokenSurvivesTemporaryKeychainFailure(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	h.service.mu.Lock()
	h.service.secrets.ExpiresAt = time.Now().Add(-time.Minute)
	h.service.mu.Unlock()
	h.store.mu.Lock()
	h.store.fail = true
	h.store.mu.Unlock()
	if _, err := h.service.jiraAccessToken(context.Background(), ""); err == nil {
		t.Fatal("failed durable token write was hidden")
	}
	h.store.mu.Lock()
	h.store.fail = false
	h.store.mu.Unlock()
	if _, err := h.service.jiraAccessToken(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if h.refreshes.Load() != 1 {
		t.Fatal("the old rotating token was reused after a keychain failure")
	}
	h.store.mu.Lock()
	saved := h.store.values[jiraKeyringService+":fixture-client"]
	h.store.mu.Unlock()
	if !strings.Contains(saved, "fixture-refresh-1") {
		t.Fatal("the rotated token was not saved after unlocking the keychain")
	}
	status, _ := h.service.GetJiraStatus(context.Background())
	if status.Error != "" || !status.Connected {
		t.Fatal("keychain recovery left the account in an error state")
	}
}

func TestJiraExpiredGrantClearsCachedAccountData(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	repo := fixtureJiraPR(12).Repository
	if _, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: repo, Key: "FAN-1"}); err != nil {
		t.Fatal(err)
	}
	h.service.mu.Lock()
	h.service.secrets.ExpiresAt = time.Now().Add(-time.Minute)
	h.service.mu.Unlock()
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "auth.atlassian.com" {
			return false
		}
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"fixture-client-secret"}`)
		return true
	}
	_, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: repo, Key: "FAN-2"})
	if err == nil || strings.Contains(err.Error(), "fixture-client-secret") {
		t.Fatal("expired grant did not produce a safe reconnect error")
	}
	status, _ := h.service.GetJiraStatus(context.Background())
	if status.Connected || !status.Configured || !strings.Contains(status.Error, "Authorize again") {
		t.Fatal("expired grant still reported a connected account")
	}
	h.service.mu.Lock()
	cacheCount := len(h.service.cache)
	h.service.mu.Unlock()
	if cacheCount != 0 {
		t.Fatal("expired account data remained cached")
	}
}

func TestJiraRedirectsNeverForwardCredentials(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	var attempts atomic.Int32
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		attempts.Add(1)
		if r.URL.Host != "api.atlassian.com" && r.URL.Host != "auth.atlassian.com" {
			t.Error("credentials were forwarded outside the fixed Atlassian hosts")
		}
		w.Header().Set("Location", "https://untrusted.example/receive")
		w.WriteHeader(http.StatusTemporaryRedirect)
		return true
	}
	if _, err := h.service.ReadJiraIssue(context.Background(), JiraIssueRequest{Repository: fixtureJiraPR(12).Repository, Key: "FAN-1"}); err == nil {
		t.Fatal("API redirect was accepted")
	}
	var response jiraTokenResponse
	if err := h.service.jiraJSON(context.Background(), http.MethodPost, "https://auth.atlassian.com/oauth/token", "", map[string]string{"client_secret": "fixture-client-secret"}, &response); err == nil {
		t.Fatal("OAuth redirect was accepted")
	}
	if attempts.Load() != 2 {
		t.Fatal("the HTTP client followed a credential-bearing redirect")
	}
}

func TestJiraCallbackPortCollisionAndRequestCancellation(t *testing.T) {
	h := newJiraHarness(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = h.service.ConfigureJira(context.Background(), JiraAppConfiguration{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", CallbackPort: listener.Addr().(*net.TCPAddr).Port}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.BeginJiraConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "port is in use") {
		t.Fatal("callback port collision was not explained")
	}
	h.authorize(t)
	started := make(chan struct{})
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		close(started)
		<-r.Context().Done()
		return true
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.ReadJiraIssue(ctx, JiraIssueRequest{Repository: fixtureJiraPR(12).Repository, Key: "FAN-1"})
		finished <- err
	}()
	<-started
	cancel()
	select {
	case err = <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request cancellation was not propagated: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled ticket request did not stop")
	}
}

func TestJiraConfigurationCorruptionIsRecoverable(t *testing.T) {
	for _, input := range []string{`{"clientId":"partial-client","callbackPort":"broken"}`, `{"clientId":"fixture-client","sites":[{"id":"site-a","url":"javascript:alert(1)"}]}`} {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jira.json")
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			h := newJiraHarness(t)
			reopened := newJiraIntegration(path, h.store, h.service.client)
			defer reopened.ServiceShutdown()
			status, _ := reopened.GetJiraStatus(context.Background())
			if status.Error == "" || status.Configured || len(status.Sites) != 0 || status.CallbackPort != defaultJiraCallbackPort {
				t.Fatal("corrupt configuration was partially activated")
			}
			if err := reopened.ConfigureJira(context.Background(), JiraAppConfiguration{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", CallbackPort: defaultJiraCallbackPort}); err != nil {
				t.Fatal(err)
			}
			status, _ = reopened.GetJiraStatus(context.Background())
			if status.Error != "" || !status.Configured || status.Connected {
				t.Fatal("configuration could not be repaired without fake connection success")
			}
		})
	}
}

func TestJiraOversizedConfigurationPreservesSavedSettings(t *testing.T) {
	h := newJiraHarness(t)
	h.configure(t)
	original, err := os.ReadFile(h.service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	oversized := h.service.config
	oversized.Selections = map[string]jiraSelection{}
	keys := make([]string, 100)
	for index := range keys {
		keys[index] = "FAN-" + strconv.Itoa(index+1)
	}
	for index := 0; index < 1000; index++ {
		oversized.Selections["github.com/team/app#"+strconv.Itoa(index+1)] = jiraSelection{Hidden: keys}
	}
	if err = writeJiraConfiguration(h.service.configPath, oversized); err == nil {
		t.Fatal("configuration too large to reopen was saved")
	}
	after, err := os.ReadFile(h.service.configPath)
	if err != nil || string(after) != string(original) {
		t.Fatal("size rejection replaced the previous valid configuration")
	}
}
