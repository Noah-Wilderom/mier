package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mag-ik-een-review/internal/githubclient"
	"mag-ik-een-review/internal/reviewagent"
	"mag-ik-een-review/internal/typesafe"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func predictionFixture(t *testing.T, block <-chan struct{}) (*PredictionService, PredictionRequest, *atomic.Int32) {
	t.Helper()
	t.Setenv("MIER_JEV_FAKE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	scope := PredictionScope{Host: "github.com", Owner: "team", Repo: "app"}
	run := reviewagent.NewRun("external", "", reviewagent.Context{Host: scope.Host, Owner: scope.Owner, Repository: scope.Repo, PullRequest: 12, HeadSHA: strings.Repeat("b", 40), BaseSHA: strings.Repeat("a", 40), Files: []reviewagent.File{{Path: "source.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}, nil)
	if _, err := run.AddFinding(reviewagent.Finding{Path: "source.go", Line: 1, Side: "new", Severity: "bug", Body: "The new branch drops validation"}); err != nil {
		t.Fatal(err)
	}
	run.Finish("completed", "", "op")
	ai := &AIService{tasks: map[string]*aiTask{run.Snapshot().ID: {run: run}}}
	s := NewPredictionService(ai, nil, nil)
	s.store = &memorySecrets{values: map[string]string{}}
	s.configPath = filepath.Join(t.TempDir(), "predictions.json")
	calls := &atomic.Int32{}
	s.newClient = func(key string) (*typesafe.Client, error) {
		return typesafe.NewWithHTTPClient(key, &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer api-private" {
				return nil, errors.New("wrong secret")
			}
			var body any
			if r.URL.Path == "/v1/models" {
				body = map[string]any{"models": []typesafe.Model{{Name: "jev-1.13.0", Description: "Fixture"}}}
			} else {
				calls.Add(1)
				if block != nil {
					select {
					case <-block:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				var input typesafe.Request
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					return nil, err
				}
				answers := map[string]any{}
				for id, q := range input.Questions {
					if q.Type == "noul" {
						answers[id] = map[string]any{"type": "noul", "noul": .1}
					} else {
						options := q.Criteria.(map[string]any)
						probabilities := map[string]float64{}
						for option := range options {
							probabilities[option] = 0
						}
						probabilities["needs_evidence"] = 1
						answers[id] = map[string]any{"type": "choice", "choice": "needs_evidence", "confidence": 1, "probabilities": probabilities}
					}
				}
				body = map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 100, "output_tokens": 0}}
			}
			data, _ := json.Marshal(body)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
		})})
	}
	request := PredictionRequest{Kind: "findings", Scope: scope, PullRequest: 12, HeadSHA: strings.Repeat("b", 40), BaseSHA: strings.Repeat("a", 40), RunID: run.Snapshot().ID}
	t.Cleanup(func() { s.ServiceShutdown() })
	return s, request, calls
}
func TestPredictionConsentSecretStorageCachingAndFeedback(t *testing.T) {
	s, r, calls := predictionFixture(t, nil)
	ctx := context.Background()
	if _, err := s.Evaluate(ctx, r); err == nil || calls.Load() != 0 {
		t.Fatal("source sent before consent")
	}
	if _, err := s.Connect(ctx, "api-private"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(r.Scope, "jev-1.13.0", true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.configPath)
	if strings.Contains(string(data), "api-private") {
		t.Fatal("API secret persisted as metadata")
	}
	before := s.ai.tasks[r.RunID].run.Snapshot()
	first, err := s.Evaluate(ctx, r)
	if err != nil || len(first.Rows) != 2 || first.Rows[0].FindingID == "" {
		t.Fatal(first, err)
	}
	second, err := s.Evaluate(ctx, r)
	if err != nil || !second.Cached || calls.Load() != 1 {
		t.Fatal("repeat inference not cached", second, err, calls.Load())
	}
	if !reflect.DeepEqual(before, s.ai.tasks[r.RunID].run.Snapshot()) {
		t.Fatal("triage mutated original findings")
	}
	if err := s.Feedback(r.Scope, first.ID, true); err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(s.Evaluation())
	if strings.Contains(string(metadata), "source.go") || strings.Contains(string(metadata), "validation") || len(s.Evaluation()) != 1 {
		t.Fatal("feedback retained source", string(metadata))
	}
	r.HeadSHA = strings.Repeat("c", 40)
	if _, err := s.Evaluate(ctx, r); err == nil || calls.Load() != 1 {
		t.Fatal("stale findings sent")
	}
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	status, _ := s.GetStatus(r.Scope)
	if status.Configured || len(s.cache) != 0 {
		t.Fatal("disconnect retained active credentials/cache")
	}
}
func TestDisablingPredictionsCancelsInFlightModelCall(t *testing.T) {
	block := make(chan struct{})
	s, r, calls := predictionFixture(t, block)
	ctx := context.Background()
	if _, err := s.Connect(ctx, "api-private"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(r.Scope, "jev-1.13.0", true); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Evaluate(ctx, r); done <- err }()
	deadline := time.After(3 * time.Second)
	for calls.Load() == 0 {
		select {
		case err := <-done:
			t.Fatal("assessment ended early", err)
		case <-deadline:
			t.Fatal("model call did not start")
		case <-time.After(time.Millisecond):
		}
	}
	if err := s.SaveSettings(r.Scope, "jev-1.13.0", false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("disabled result returned")
	}
	if len(s.cache) != 0 {
		t.Fatal("disabled result cached")
	}
}
func TestPredictionSettingsPreserveConsentOnGlobalModelChangeAndFailedKeychainWrite(t *testing.T) {
	s, r, _ := predictionFixture(t, nil)
	ctx := context.Background()
	if _, err := s.Connect(ctx, "api-private"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(r.Scope, "jev-1.13.0", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(PredictionScope{}, "jev-latest", false); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetStatus(r.Scope)
	if err != nil || !status.Enabled || status.Model != "jev-latest" {
		t.Fatal("global model change lost consent", status, err)
	}
	store := s.store.(*memorySecrets)
	store.fail = true
	if _, err := s.Connect(ctx, "api-private"); err == nil {
		t.Fatal("keychain failure hidden")
	}
	if key, _ := store.Get(predictionKeyringService, "api-key"); key != "api-private" {
		t.Fatal("previous secret lost")
	}
}

func TestPredictionConnectSelectsAnAvailableModelBeforeAssessment(t *testing.T) {
	s, request, _ := predictionFixture(t, nil)
	s.newClient = func(string) (*typesafe.Client, error) { return typesafe.NewFake("success") }
	if _, err := s.Connect(context.Background(), "local-test-credential"); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetStatus(request.Scope)
	if err != nil || status.Model != typesafe.FakeModel {
		t.Fatalf("connection retained an unavailable model: %+v, %v", status, err)
	}
	if err := s.SaveSettings(request.Scope, status.Model, true); err != nil {
		t.Fatal(err)
	}
	result, err := s.Evaluate(context.Background(), request)
	if err != nil || len(result.Rows) != 2 {
		t.Fatalf("connected provider could not assess findings: %+v, %v", result, err)
	}
}

func TestPredictionModelCatalogPreservesPinsAndRepairsUnavailableSelections(t *testing.T) {
	models := []typesafe.Model{{Name: "jev-other"}, {Name: "jev-latest"}}
	if availablePredictionModel("jev-other", models) != "jev-other" || availablePredictionModel("jev-removed", models) != "jev-latest" {
		t.Fatal("available pins or stable alias not respected")
	}
	s, request, _ := predictionFixture(t, nil)
	if _, err := s.Connect(context.Background(), "api-private"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(request.Scope, "jev-removed", true); err != nil {
		t.Fatal(err)
	}
	s.newClient = func(string) (*typesafe.Client, error) { return typesafe.NewFake("success") }
	if _, err := s.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetStatus(request.Scope)
	if err != nil || status.Model != typesafe.FakeModel || !status.Enabled {
		t.Fatal("refresh retained unavailable selection or lost consent", status, err)
	}
	restored := NewPredictionService(nil, nil, nil)
	data, err := os.ReadFile(s.configPath)
	if err != nil || json.Unmarshal(data, &restored.config) != nil || restored.config.Model != typesafe.FakeModel {
		t.Fatal("available model not saved", string(data), err)
	}
}

func TestPredictionCatalogRepairRollsBackWhenTheKeyCannotBeSaved(t *testing.T) {
	s, request, _ := predictionFixture(t, nil)
	ctx := context.Background()
	if _, err := s.Connect(ctx, "api-private"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(request.Scope, "jev-original-pin", true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	s.store.(*memorySecrets).fail = true
	s.newClient = func(string) (*typesafe.Client, error) { return typesafe.NewFake("success") }
	if _, err := s.Connect(ctx, "replacement-test-key"); err == nil {
		t.Fatal("keychain failure was hidden")
	}
	status, err := s.GetStatus(request.Scope)
	if err != nil || status.Model != "jev-original-pin" || !status.Enabled || s.key != "api-private" {
		t.Fatal("failed connection replaced the previous configuration", status, err)
	}
	after, err := os.ReadFile(s.configPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed connection persisted the replacement model", err)
	}
}

func TestPredictionDisconnectInvalidatesPendingConnection(t *testing.T) {
	s, _, _ := predictionFixture(t, nil)
	started, release := make(chan struct{}), make(chan struct{})
	s.newClient = func(key string) (*typesafe.Client, error) {
		return typesafe.NewWithHTTPClient(key, &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
			close(started)
			<-release
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"models":[{"name":"jev-latest"}]}`))}, nil
		})})
	}
	done := make(chan error, 1)
	go func() { _, err := s.Connect(context.Background(), "fixture-only-key"); done <- err }()
	<-started
	if err := s.Disconnect(); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale connection reconnected after disconnect")
	}
	status, err := s.GetStatus(PredictionScope{})
	if err != nil || status.Configured {
		t.Fatal("disconnected key was restored by an older request", status, err)
	}
}
func TestPredictionCriteriaAndLogExcerptsStayBoundedAndPreserveEvidence(t *testing.T) {
	criteria := predictionCriteria("# Acceptance criteria\n- [ ] Preserve the original requester\n- [x] Keep completed reports\n\n")
	if !reflect.DeepEqual(criteria, []string{"Preserve the original requester", "Keep completed reports"}) {
		t.Fatal(criteria)
	}
	lines := strings.Repeat("normal progress\n", 50) + "##[error] Typescript compilation failed\nsource.ts:2 invalid assignment\n" + strings.Repeat("normal progress\n", 50)
	excerpts := predictionLogExcerpts(lines)
	if len(excerpts) != 1 || excerpts[0].Line != 48 || !strings.Contains(excerpts[0].Text, "invalid assignment") {
		t.Fatal(excerpts)
	}
	for _, excerpt := range predictionLogExcerpts(strings.Repeat("error: "+strings.Repeat("x", 2000)+"\n", 500)) {
		if len(excerpt.Text) > 1200 {
			t.Fatal("unbounded log evidence")
		}
	}
}
func TestPredictionScopeSupportsNestedReviewProvidersAndRejectsTraversal(t *testing.T) {
	if _, err := predictionScopeKey(PredictionScope{Host: "gitlab.example.test", Owner: "group/nested", Repo: "app"}); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"group/../elsewhere", "group//nested", "/group"} {
		if _, err := predictionScopeKey(PredictionScope{Host: "gitlab.example.test", Owner: owner, Repo: "app"}); err == nil {
			t.Fatal("unsafe scope accepted", owner)
		}
	}
}

func TestJevTicketCoverageReadsLinkedCriteriaWithoutTicketMutation(t *testing.T) {
	h := newJiraHarness(t)
	h.authorize(t)
	ctx := context.Background()
	pr := fixtureJiraPR(12)
	if err := h.service.SaveJiraRepositoryPreferences(ctx, pr.Repository, JiraRepositoryPreferences{SiteID: "site-a", Projects: []string{"FAN"}, AcceptanceField: "customfield_10001"}); err != nil {
		t.Fatal(err)
	}
	s := &PredictionService{ai: &AIService{jira: h.service}}
	state := map[string]any{"files": []any{}, "title": "", "description": ""}
	questions := map[string]typesafe.Question{}
	rows := []PredictionRow{}
	warnings := []string{}
	before := h.handler
	h.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/issue/") && r.Method != http.MethodGet {
			t.Error("ticket mutation attempted")
		}
		if before != nil {
			return before(w, r)
		}
		return false
	}
	r := PredictionRequest{Scope: PredictionScope{Host: pr.Repository.Host, Owner: pr.Repository.Owner, Repo: pr.Repository.Repo}, PullRequest: pr.Number}
	if err := s.prepareTicketQuestions(ctx, r, pr.Branch, pr.Title, pr.Body, state, questions, &rows, &warnings); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || len(questions) != len(rows) {
		t.Fatal("criteria were not converted to named questions", rows)
	}
	for _, row := range rows {
		if row.TicketProvider != "jira" || !strings.HasPrefix(row.TicketKey, "FAN-") || row.Label != "Hover and definitions work." {
			t.Fatal("unexpected or invented requirement", row)
		}
	}
	data, _ := json.Marshal(state)
	if strings.Contains(string(data), "fixture-access") || strings.Contains(string(data), "fixture-client-secret") {
		t.Fatal("ticket credentials in prediction state")
	}
}

func TestJevConflictGuidanceReadsAllVersionsAndRejectsChangedToken(t *testing.T) {
	ai, root, _ := actionServiceFixture(t)
	ctx := context.Background()
	actionTestGit(t, root, "checkout", "-b", "incoming")
	if err := os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actionTestGit(t, root, "commit", "-am", "incoming")
	actionTestGit(t, root, "checkout", "main")
	if err := os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actionTestGit(t, root, "commit", "-am", "current")
	// An actual Git conflict ensures the preparation cannot silently resolve it.
	command := exec.Command("git", "merge", "incoming")
	command.Dir = root
	if command.Run() == nil {
		t.Fatal("expected conflict")
	}
	file, err := ai.repositories.Conflict(ctx, root, "source.ts")
	if err != nil {
		t.Fatal(err)
	}
	s := &PredictionService{repositories: ai.repositories}
	r := PredictionRequest{Scope: PredictionScope{Root: root}, Kind: "conflict", Path: "source.ts", ConflictToken: file.Token}
	prepared, rows, _, err := s.prepare(ctx, r)
	if err != nil || len(rows) != 2 || len(prepared.Questions) != 2 {
		t.Fatal(rows, err)
	}
	state := prepared.State.(map[string]any)
	if state["base"] != "const value = 1\n" || state["current"] != "const value = 3\n" || state["incoming"] != "const value = 2\n" {
		t.Fatal("conflict source mixed", state)
	}
	after, err := ai.repositories.Conflict(ctx, root, "source.ts")
	if err != nil || after.Token != file.Token {
		t.Fatal("guidance mutated the conflict", err)
	}
	r.ConflictToken = "earlier-token"
	if _, _, _, err := s.prepare(ctx, r); err == nil {
		t.Fatal("stale conflict was assessed")
	}
}

func TestJevCITriageUsesVerifiedJobLogAndBoundedErrorEvidence(t *testing.T) {
	token := "github-fixture-token"
	hash := sha256.Sum256([]byte(token))
	calls := []string{}
	transport := &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing GitHub authentication")
		}
		body := ""
		switch r.URL.Path {
		case "/repos/team/app/actions/jobs/99":
			body = `{"id":99,"run_id":12,"run_attempt":1,"name":"Build","status":"completed","conclusion":"failure"}`
		case "/repos/team/app/actions/jobs/99/logs":
			body = "Setup complete\n##[error] src/service.ts:41 Type mismatch\nBuild failed\n"
		default:
			t.Error("unexpected provider call", r.URL)
			body = "{}"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client, err := githubclient.NewWithHTTPClient("github.com", token, transport)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &CredentialService{store: &memorySecrets{values: map[string]string{credentialKeyringService + ":github.com": token}}}
	s := &PredictionService{github: &GitHubService{credentials: credentials, clients: map[string]*githubclient.Client{"github.com:" + hex.EncodeToString(hash[:]): client}}}
	prepared, rows, _, err := s.prepare(context.Background(), PredictionRequest{Kind: "ci", Scope: PredictionScope{Host: "github.com", Owner: "team", Repo: "app"}, JobID: 99})
	if err != nil || len(rows) != 2 || len(prepared.Questions) != 2 {
		t.Fatal(rows, err)
	}
	if !strings.Contains(rows[1].Excerpt, "src/service.ts:41") || !reflect.DeepEqual(calls, []string{"GET /repos/team/app/actions/jobs/99", "GET /repos/team/app/actions/jobs/99/logs"}) {
		t.Fatal("missing job evidence or unexpected mutation", rows, calls)
	}
	data, _ := json.Marshal(prepared)
	if strings.Contains(string(data), token) {
		t.Fatal("GitHub credential entered model context")
	}
}
