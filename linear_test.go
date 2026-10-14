package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type linearHarness struct {
	service                 *LinearService
	store                   *memorySecrets
	mu                      sync.Mutex
	challenge, organization string
	queries                 []string
	refreshes               atomic.Int32
	handler                 func(http.ResponseWriter, *http.Request, string) bool
}

func newLinearHarness(t *testing.T) *linearHarness {
	t.Helper()
	h := &linearHarness{store: &memorySecrets{values: map[string]string{}}, organization: "workspace-a"}
	client := &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.linear.app" || r.Method != http.MethodPost {
			t.Errorf("unexpected Linear host or method: %s %s", r.Method, r.URL)
		}
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_secret") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Form.Get("client_id") == "" {
				t.Error("PKCE token exchange must use form data and no client secret")
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				h.mu.Lock()
				expected := h.challenge
				h.mu.Unlock()
				if len(r.Form.Get("code_verifier")) < 43 || base64.RawURLEncoding.EncodeToString(challenge[:]) != expected {
					t.Error("provider could not verify the advertised PKCE challenge")
				}
			} else if r.Form.Get("grant_type") == "refresh_token" {
				h.refreshes.Add(1)
				if r.Form.Get("refresh_token") == "" {
					t.Error("missing rotating token")
				}
			} else {
				t.Errorf("unknown grant: %s", r.Form.Get("grant_type"))
			}
			h.mu.Lock()
			handler := h.handler
			h.mu.Unlock()
			if handler != nil && handler(w, r, "") {
				return w.Result(), nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "token_type": "Bearer", "expires_in": 86400, "scope": "read"})
			return w.Result(), nil
		}
		var request struct {
			Query     string
			Variables map[string]any
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || strings.Contains(request.Query, "mutation") {
			t.Error("invalid or mutating Linear request")
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer fixture-") {
			t.Error("missing OAuth bearer token")
		}
		h.mu.Lock()
		h.queries = append(h.queries, request.Query)
		handler, organization := h.handler, h.organization
		h.mu.Unlock()
		if handler != nil && handler(w, r, request.Query) {
			return w.Result(), nil
		}
		var result any
		switch {
		case strings.Contains(request.Query, "MierLinearIdentity"):
			result = map[string]any{"viewer": map[string]string{"id": "user-1", "name": "Reviewer"}, "organization": map[string]string{"id": organization, "name": "Linear workspace", "urlKey": organization}}
		case strings.Contains(request.Query, "MierLinearComments"):
			result = map[string]any{"issue": map[string]any{"comments": map[string]any{"nodes": []any{map[string]any{"id": "comment-1", "body": "Please **verify** the requirements.", "createdAt": "2026-10-14T12:00:00Z", "user": map[string]string{"name": "Reviewer"}}}}}}
		case strings.Contains(request.Query, "MierLinearSearch"):
			result = map[string]any{"searchIssues": map[string]any{"nodes": []any{linearFixtureIssue("ENG-7", organization)}}}
		case strings.Contains(request.Query, "MierLinearIssue"):
			key, _ := request.Variables["id"].(string)
			if key == "ENG-404" {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "fixture-private-provider-message", "extensions": map[string]string{"code": "ENTITY_NOT_FOUND"}}}})
				return w.Result(), nil
			}
			issue := linearFixtureIssue(key, organization)
			if !strings.Contains(request.Query, "description") {
				delete(issue, "description")
			}
			result = map[string]any{"issue": issue}
		default:
			t.Errorf("unexpected query: %s", request.Query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
		return w.Result(), nil
	})}
	h.service = newLinearService(filepath.Join(t.TempDir(), "linear.json"), h.store, client)
	t.Cleanup(func() { _ = h.service.ServiceShutdown() })
	return h
}
func linearFixtureIssue(key, workspace string) map[string]any {
	return map[string]any{"id": "issue-" + key, "identifier": key, "title": "Requirements for " + key, "url": "https://linear.app/" + workspace + "/issue/" + key, "state": map[string]string{"name": "In Progress", "type": "started"}, "assignee": map[string]string{"name": "Developer"}, "team": map[string]string{"key": strings.Split(key, "-")[0], "name": "Engineering"}, "description": "## Requirements\n- [x] Keep the review open.\n\n```ts\nconst value = 1\n```", "priorityLabel": "High", "parent": map[string]string{"identifier": "ENG-1"}, "labels": map[string]any{"nodes": []any{map[string]string{"name": "review"}}}, "updatedAt": "2026-10-14T12:00:00Z"}
}
func (h *linearHarness) configure(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if err := h.service.ConfigureLinear(context.Background(), LinearAppConfiguration{ClientID: "fixture-client", CallbackPort: port}); err != nil {
		t.Fatal(err)
	}
}
func (h *linearHarness) begin(t *testing.T) url.Values {
	t.Helper()
	address, err := h.service.BeginLinearConnection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(address)
	if err != nil || u.Host != "linear.app" || u.Path != "/oauth/authorize" {
		t.Fatalf("bad authorization URL: %s", address)
	}
	query := u.Query()
	if query.Get("scope") != "read" || query.Get("actor") != "user" || query.Get("code_challenge_method") != "S256" || len(query.Get("state")) != 43 || query.Get("client_secret") != "" || query.Get("code_verifier") != "" {
		t.Fatal("incomplete PKCE authorization request")
	}
	h.mu.Lock()
	h.challenge = query.Get("code_challenge")
	h.mu.Unlock()
	return query
}
func (h *linearHarness) complete(t *testing.T, query url.Values) int {
	t.Helper()
	response, err := http.Get(query.Get("redirect_uri") + "?" + url.Values{"state": {query.Get("state")}, "code": {"fixture-code"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response.StatusCode
}
func (h *linearHarness) authorize(t *testing.T) {
	t.Helper()
	h.configure(t)
	if code := h.complete(t, h.begin(t)); code != 200 {
		t.Fatalf("authorization returned %d", code)
	}
}
func (h *linearHarness) expire() {
	h.service.mu.Lock()
	h.service.secrets.ExpiresAt = time.Now().Add(-time.Minute)
	h.service.mu.Unlock()
}
func fixtureLinearPR() TicketPullRequest {
	return TicketPullRequest{Repository: TicketRepository{Host: "github.com", Owner: "team", Repo: "app"}, Number: 12, Branch: "review/eng-123-fix", Title: "ENG-123: improve reviews", Body: "See https://linear.app/workspace-a/issue/ENG-19/title and ENG-404."}
}

func TestLinearPKCEStorageRestartAndDisconnect(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	status, err := h.service.GetLinearStatus(context.Background())
	if err != nil || !status.Connected || status.Workspace.ID != "workspace-a" || status.Account != "Reviewer" {
		t.Fatalf("connection not verified: %+v %v", status, err)
	}
	data, err := os.ReadFile(h.service.configPath)
	if err != nil || strings.Contains(string(data), "fixture-access") || strings.Contains(string(data), "fixture-refresh") || strings.Contains(string(data), "code_verifier") {
		t.Fatal("credentials leaked to configuration")
	}
	serialized, _ := json.Marshal(status)
	if strings.Contains(string(serialized), "fixture-access") || strings.Contains(string(serialized), "fixture-refresh") {
		t.Fatal("credentials leaked to frontend")
	}
	reopened := newLinearService(h.service.configPath, h.store, h.service.client)
	defer reopened.ServiceShutdown()
	status, err = reopened.GetLinearStatus(context.Background())
	if err != nil || !status.Connected {
		t.Fatal("saved authorization did not survive restart")
	}
	if err := reopened.DisconnectLinear(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, _ = reopened.GetLinearStatus(context.Background())
	if status.Connected || !status.Configured || status.Workspace != nil {
		t.Fatal("disconnect did not retain only the app configuration")
	}
}

func TestLinearCallbackRejectsInvalidRequestsWithoutConsumingAttempt(t *testing.T) {
	h := newLinearHarness(t)
	h.configure(t)
	query := h.begin(t)
	if _, err := h.service.BeginLinearConnection(context.Background()); err == nil {
		t.Fatal("duplicate authorization allowed")
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.URL.RawQuery = "state=wrong&code=fixture-code" },
		func(r *http.Request) { r.Host = "attacker.example" },
		func(r *http.Request) { r.Method = http.MethodPost },
		func(r *http.Request) { r.URL.Path = "/wrong" },
		func(r *http.Request) { r.URL.RawQuery += "&state=duplicate" },
	} {
		r := httptest.NewRequest(http.MethodGet, query.Get("redirect_uri")+"?"+url.Values{"state": {query.Get("state")}, "code": {"fixture-code"}}.Encode(), nil)
		change(r)
		w := httptest.NewRecorder()
		h.service.handleLinearCallback(h.service.pending, w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("unsafe callback accepted: %d", w.Code)
		}
	}
	if code := h.complete(t, query); code != 200 {
		t.Fatal("invalid callback consumed legitimate attempt")
	}
}

func TestLinearOAuthDenialOccupiedPortAndCancellation(t *testing.T) {
	h := newLinearHarness(t)
	h.configure(t)
	query := h.begin(t)
	r := httptest.NewRequest(http.MethodGet, query.Get("redirect_uri")+"?"+url.Values{"state": {query.Get("state")}, "error": {"access_denied"}}.Encode(), nil)
	w := httptest.NewRecorder()
	h.service.handleLinearCallback(h.service.pending, w, r)
	status, _ := h.service.GetLinearStatus(context.Background())
	if w.Code != 400 || status.Connected || !strings.Contains(status.Error, "declined") {
		t.Fatal("denied consent was not reported")
	}
	h.service.CancelLinearConnection()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := h.service.ConfigureLinear(context.Background(), LinearAppConfiguration{ClientID: "fixture-client", CallbackPort: listener.Addr().(*net.TCPAddr).Port}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.BeginLinearConnection(context.Background()); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatal("occupied callback port was not reported")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.service.BeginLinearConnection(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled call started authorization")
	}
}

func TestLinearSecureStoreFailureAndRecovery(t *testing.T) {
	h := newLinearHarness(t)
	h.configure(t)
	h.store.fail = true
	if code := h.complete(t, h.begin(t)); code != 400 {
		t.Fatal("connection accepted without durable tokens")
	}
	status, _ := h.service.GetLinearStatus(context.Background())
	if status.Connected || !strings.Contains(status.Error, "keychain") {
		t.Fatal("keychain failure not reported")
	}
	h.store.fail = false
	if code := h.complete(t, h.begin(t)); code != 200 {
		t.Fatal("authorization did not recover")
	}
}

func TestLinearReadDiscoverySelectionsSearchAndTeams(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	pr := fixtureLinearPR()
	if err := h.service.SaveLinearRepositoryPreferences(context.Background(), pr.Repository, LinearRepositoryPreferences{Teams: []string{" eng ", "ENG"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.service.SelectLinearTicket(context.Background(), TicketSelection{PullRequest: pr, Key: "ENG-7", Action: "pin"}); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.GetLinearTickets(context.Background(), pr)
	if err != nil || len(result.Tickets) != 4 || result.Tickets[0].Key != "ENG-7" || !reflect.DeepEqual(result.Tickets[1].Sources, []string{"branch", "title"}) || result.Tickets[3].Error == "" || strings.Contains(result.Tickets[3].Error, "private-provider") {
		t.Fatalf("invalid discovered tickets: %+v %v", result, err)
	}
	issue, err := h.service.ReadLinearIssue(context.Background(), TicketIssueRequest{Repository: pr.Repository, Key: "ENG-123"})
	if err != nil || len(issue.Comments) != 1 || issue.Priority != "High" || issue.StatusCategory != "indeterminate" || issue.Description == "" {
		t.Fatalf("detail read failed: %+v %v", issue, err)
	}
	for _, action := range []string{"hide", "restore"} {
		if err := h.service.SelectLinearTicket(context.Background(), TicketSelection{PullRequest: pr, Key: "ENG-7", Action: action}); err != nil {
			t.Fatal(err)
		}
		result, err = h.service.GetLinearTickets(context.Background(), pr)
		if err != nil || (action == "hide" && len(result.Tickets) != 3) || (action == "restore" && result.Tickets[0].Key != "ENG-7") {
			t.Fatal("manual hide/restore failed")
		}
	}
	search, err := h.service.SearchLinearIssues(context.Background(), TicketSearchRequest{Repository: pr.Repository, Query: "requirements"})
	if err != nil || len(search) != 1 || search[0].Key != "ENG-7" {
		t.Fatal("Linear GraphQL search failed")
	}
	search, err = h.service.SearchLinearIssues(context.Background(), TicketSearchRequest{Repository: pr.Repository, Query: "APP-123"})
	if err != nil || len(search) != 0 {
		t.Fatal("exact search escaped the configured team")
	}
	if _, err := h.service.ReadLinearIssue(context.Background(), TicketIssueRequest{Repository: pr.Repository, Key: "APP-123"}); err == nil {
		t.Fatal("detail read escaped the configured team")
	}
}

func TestLinearRefreshSerializedAndRotationRecovered(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	h.expire()
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := h.service.linearAccessToken(context.Background(), ""); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if h.refreshes.Load() != 1 {
		t.Fatal("parallel calls refreshed the same token more than once")
	}
	h.expire()
	h.store.fail = true
	if _, err := h.service.linearAccessToken(context.Background(), ""); err == nil {
		t.Fatal("refresh ignored failed token persistence")
	}
	h.store.fail = false
	if _, err := h.service.linearAccessToken(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if h.refreshes.Load() != 2 {
		t.Fatal("unsaved rotated token was refreshed instead of persisted")
	}
}

func TestLinearGraphQLAuthenticationAndRevokedGrant(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	var rejected atomic.Bool
	h.mu.Lock()
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if strings.Contains(query, "MierLinearIssue") && !rejected.Swap(true) {
			_, _ = io.WriteString(w, `{"errors":[{"message":"private-token","extensions":{"type":"authentication error"}}]}`)
			return true
		}
		return false
	}
	h.mu.Unlock()
	pr := fixtureLinearPR()
	if _, err := h.service.ReadLinearIssue(context.Background(), TicketIssueRequest{Repository: pr.Repository, Key: "ENG-123"}); err != nil || h.refreshes.Load() != 1 {
		t.Fatalf("HTTP-200 authorization failure was not retried: %v", err)
	}
	h.mu.Lock()
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if r.URL.Path == "/oauth/token" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","description":"private-token"}`)
			return true
		}
		return false
	}
	h.mu.Unlock()
	h.expire()
	if _, err := h.service.linearAccessToken(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "private-token") {
		t.Fatal("revoked grant not reported safely")
	}
	status, _ := h.service.GetLinearStatus(context.Background())
	if status.Connected {
		t.Fatal("revoked grant remained connected")
	}
}

func TestLinearCommentFailureAndRateLimit(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	pr := fixtureLinearPR()
	h.mu.Lock()
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if strings.Contains(query, "MierLinearComments") {
			w.WriteHeader(403)
			return true
		}
		return false
	}
	h.mu.Unlock()
	issue, err := h.service.ReadLinearIssue(context.Background(), TicketIssueRequest{Repository: pr.Repository, Key: "ENG-123"})
	if err != nil || issue.CommentsError == "" || issue.Description == "" {
		t.Fatal("comment failure discarded the issue description")
	}
	h.mu.Lock()
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if strings.Contains(query, "MierLinearSearch") {
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(429)
			return true
		}
		return false
	}
	h.mu.Unlock()
	if _, err := h.service.SearchLinearIssues(context.Background(), TicketSearchRequest{Repository: pr.Repository, Query: "requirements"}); err == nil || !strings.Contains(err.Error(), "limiting") {
		t.Fatal("rate limit not reported")
	}
	h.service.mu.Lock()
	wait := time.Until(h.service.retryAt)
	h.service.mu.Unlock()
	if wait < 10*time.Second || wait > 12*time.Second {
		t.Fatal("Retry-After not respected")
	}
}

func TestLinearLateResultsDiscardedAndWorkspaceSelectionsIsolated(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	pr := fixtureLinearPR()
	if err := h.service.SelectLinearTicket(context.Background(), TicketSelection{PullRequest: pr, Key: "ENG-7", Action: "pin"}); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	h.mu.Lock()
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if strings.Contains(query, "MierLinearIssue") {
			close(started)
			<-release
		}
		return false
	}
	h.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := h.service.ReadLinearIssue(context.Background(), TicketIssueRequest{Repository: pr.Repository, Key: "ENG-123"})
		done <- err
	}()
	<-started
	if err := h.service.DisconnectLinear(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late result escaped disconnected account")
	}
	h.mu.Lock()
	h.handler, h.organization = nil, "workspace-b"
	h.mu.Unlock()
	if code := h.complete(t, h.begin(t)); code != 200 {
		t.Fatal("second workspace failed authorization")
	}
	pr.Branch, pr.Title, pr.Body = "feature", "Review", ""
	result, err := h.service.GetLinearTickets(context.Background(), pr)
	if err != nil || len(result.Tickets) != 0 {
		t.Fatal("personal selection crossed workspace boundary")
	}
}

func TestLinearLargeTokensFitNativeKeychainLimits(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			h := newLinearHarness(t)
			h.service.store = &jiraKeychainStore{base: platformLimitedJiraStore{secretStore: h.store, platform: platform}}
			h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
				if r.URL.Path != "/oauth/token" {
					return false
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-" + strings.Repeat("a", 6000), "refresh_token": strings.Repeat("b", 5000), "token_type": "Bearer", "expires_in": 86400, "scope": []string{"read"}})
				return true
			}
			h.authorize(t)
			reopened := newLinearService(h.service.configPath, platformLimitedJiraStore{secretStore: h.store, platform: platform}, h.service.client)
			defer reopened.ServiceShutdown()
			status, err := reopened.GetLinearStatus(context.Background())
			if err != nil || !status.Connected {
				t.Fatalf("large Linear tokens were not persisted on %s: %v", platform, err)
			}
		})
	}
}

func TestLinearFailedReauthorizationPreservesExistingGrant(t *testing.T) {
	h := newLinearHarness(t)
	h.authorize(t)
	oldID := h.service.config.CredentialID
	oldData, err := h.service.store.Get(linearKeyringService, oldID)
	if err != nil {
		t.Fatal(err)
	}
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	h.service.configPath = filepath.Join(block, "linear.json")
	h.mu.Lock()
	h.organization = "workspace-b"
	h.mu.Unlock()
	if code := h.complete(t, h.begin(t)); code != 400 {
		t.Fatal("authorization accepted with failed configuration publication")
	}
	status, _ := h.service.GetLinearStatus(context.Background())
	data, err := h.service.store.Get(linearKeyringService, oldID)
	if err != nil || data != oldData || !status.Connected || status.Workspace.ID != "workspace-a" || h.service.config.CredentialID != oldID {
		t.Fatal("failed authorization replaced the existing workspace or tokens")
	}
}

func TestLinearRequiresGrantedReadScope(t *testing.T) {
	h := newLinearHarness(t)
	h.configure(t)
	h.handler = func(w http.ResponseWriter, r *http.Request, query string) bool {
		if r.URL.Path != "/oauth/token" {
			return false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "token_type": "Bearer", "expires_in": 86400, "scope": "comments:create"})
		return true
	}
	if code := h.complete(t, h.begin(t)); code != 400 {
		t.Fatal("connection accepted without read scope")
	}
	status, _ := h.service.GetLinearStatus(context.Background())
	if status.Connected || !strings.Contains(status.Error, "read access") {
		t.Fatal("missing read scope was not reported")
	}
}

func TestLinearRequestRefusesRedirectsOversizedResponsesAndCancellation(t *testing.T) {
	h := newLinearHarness(t)
	var requests atomic.Int32
	h.service.client = &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		w := httptest.NewRecorder()
		w.Header().Set("Location", "https://attacker.example/token")
		w.WriteHeader(http.StatusFound)
		return w.Result(), nil
	}), CheckRedirect: h.service.client.CheckRedirect}
	if _, err := h.service.linearRequest(context.Background(), "/graphql", "fixture-private", map[string]string{}); err == nil || requests.Load() != 1 {
		t.Fatal("credential-bearing request followed a redirect")
	}
	h.service.client.Transport = jiraTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		_, _ = io.WriteString(w, strings.Repeat("x", 2*1024*1024+1))
		return w.Result(), nil
	})
	if _, err := h.service.linearRequest(context.Background(), "/graphql", "fixture-private", map[string]string{}); err == nil || !strings.Contains(err.Error(), "too much data") {
		t.Fatal("oversized provider response was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.service.linearRequest(ctx, "/graphql", "fixture-private", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled API request was sent")
	}
}
