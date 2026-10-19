package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mag-ik-een-review/internal/githubclient"
	"mag-ik-een-review/internal/typesafe"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func fakePredictionFixture(t *testing.T, scenario string) (*PredictionService, PredictionRequest) {
	t.Helper()
	source, request, _ := predictionFixture(t, nil)
	t.Setenv("MIER_JEV_FAKE", "1")
	t.Setenv("MIER_JEV_FAKE_SCENARIO", scenario)
	s := newPredictionService(source.ai, nil, nil, t.TempDir(), nil)
	s.store = &memorySecrets{values: map[string]string{}, fail: true}
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	return s, request
}

func TestFakePredictionModeKeepsRealKeysSettingsAndConsentSeparate(t *testing.T) {
	s, request := fakePredictionFixture(t, "success")
	ctx := context.Background()
	status, err := s.GetStatus(request.Scope)
	if err != nil || !status.Configured || status.Enabled || status.Provider != "fake" || status.Model != typesafe.FakeModel {
		t.Fatal("local provider tried to load a credential or enabled itself", status, err)
	}
	if _, err := s.Evaluate(ctx, request); err == nil {
		t.Fatal("local test mode bypassed repository consent")
	}
	if _, err := s.Connect(ctx, ""); err != nil {
		t.Fatal("local provider required a keychain", err)
	}
	if err := s.SaveSettings(request.Scope, typesafe.FakeModel, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(s.configPath), "predictions.json")
	realSettings := `{"model":"jev-real-pinned","scopes":{"github.com/team/app":false}}`
	if err := os.WriteFile(path, []byte(realSettings), 0600); err != nil {
		t.Fatal(err)
	}
	before := s.ai.tasks[request.RunID].run.Snapshot()
	result, err := s.Evaluate(ctx, request)
	if err != nil || result.Provider != "fake" || result.Model != typesafe.FakeModel || len(result.Rows) != 2 || !strings.Contains(result.Warnings[0], "LOCAL TEST PROVIDER") {
		t.Fatal("fake did not exercise native preparation and result mapping", result, err)
	}
	if !reflect.DeepEqual(before, s.ai.tasks[request.RunID].run.Snapshot()) {
		t.Fatal("assessment changed the original findings")
	}
	repeated, err := s.Evaluate(ctx, request)
	if err != nil || !repeated.Cached || repeated.ID != result.ID {
		t.Fatal("native result cache failed", repeated, err)
	}
	if err := s.Feedback(request.Scope, result.ID, true); err != nil || s.Evaluation()[0].Provider != "fake" {
		t.Fatal("synthetic evaluation feedback lost its provider", err)
	}
	if err := s.Disconnect(); err == nil {
		t.Fatal("test mode permitted a real credential operation")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != realSettings {
		t.Fatal("local test changed real provider settings", string(data), err)
	}
	directory := filepath.Dir(filepath.Dir(s.configPath))
	restarted := newPredictionService(s.ai, nil, nil, directory, nil)
	restarted.store = s.store
	status, err = restarted.GetStatus(request.Scope)
	if err != nil || !status.Enabled || status.Provider != "fake" {
		t.Fatal("local consent was not restored independently", status, err)
	}
	t.Setenv("MIER_JEV_FAKE", "")
	real := newPredictionService(nil, nil, nil, directory, nil)
	if real.config.Model != "jev-real-pinned" || real.config.Scopes["github.com/team/app"] {
		t.Fatal("local test consent enabled the real provider", real.config)
	}
}

func TestFakePredictionProviderExercisesNativePRTicketFindingAndCICapture(t *testing.T) {
	s, request := fakePredictionFixture(t, "success")
	ctx := context.Background()
	pr := fixtureJiraPR(request.PullRequest)
	jira := newJiraHarness(t)
	jira.authorize(t)
	if err := jira.service.SaveJiraRepositoryPreferences(ctx, pr.Repository, JiraRepositoryPreferences{SiteID: "site-a", Projects: []string{"FAN"}, AcceptanceField: "customfield_10001"}); err != nil {
		t.Fatal(err)
	}
	s.ai.jira = jira.service
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer github-test-only" {
			t.Errorf("unexpected provider write or credential: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(403)
			return
		}
		reads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/team/app/pulls/12":
			json.NewEncoder(w).Encode(githubclient.PullRequestSummary{Number: 12, Title: pr.Title, Body: pr.Body, Head: githubclient.Branch{Ref: pr.Branch, SHA: request.HeadSHA}, Base: githubclient.Branch{Ref: "main", SHA: request.BaseSHA}})
		case r.URL.Path == "/repos/team/app/pulls/12/files":
			json.NewEncoder(w).Encode([]map[string]string{{"filename": "source.go", "status": "modified", "patch": "@@ -1 +1 @@\n-old\n+new"}})
		case strings.Contains(r.URL.Path, "/compare/"):
			json.NewEncoder(w).Encode(map[string]any{"merge_base_commit": map[string]string{"sha": request.BaseSHA}})
		case r.URL.Path == "/repos/team/app/actions/jobs/99":
			io.WriteString(w, `{"id":99,"run_id":12,"run_attempt":1,"name":"Build","status":"completed","conclusion":"failure"}`)
		case r.URL.Path == "/repos/team/app/actions/jobs/99/logs":
			io.WriteString(w, "Setup complete\n##[error] src/service.ts:41 Type mismatch\nBuild failed\n")
		default:
			t.Errorf("unexpected provider read: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client, err := githubclient.NewWithHTTPClient("github.com", "github-test-only", &http.Client{Transport: jiraTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host = endpoint.Scheme, endpoint.Host
		return transport.RoundTrip(copy)
	})})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("github-test-only"))
	credentials := &CredentialService{store: &memorySecrets{values: map[string]string{credentialKeyringService + ":github.com": "github-test-only"}}}
	s.github = &GitHubService{credentials: credentials, clients: map[string]*githubclient.Client{"github.com:" + hex.EncodeToString(hash[:]): client}}
	if err := s.SaveSettings(request.Scope, typesafe.FakeModel, true); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"review", "tickets", "findings", "ci"} {
		t.Run(kind, func(t *testing.T) {
			r := request
			r.Kind, r.JobID = kind, 99
			result, err := s.Evaluate(ctx, r)
			if err != nil || len(result.Rows) == 0 || result.Kind != kind || result.Provider != "fake" {
				t.Fatal(result, err)
			}
			for _, row := range result.Rows {
				if row.Answer.Type == "" || row.Answer.Type == "choice" && row.Options[row.Answer.Choice] == "" {
					t.Fatal("question answer was not mapped to its real evidence", row)
				}
			}
			if kind == "tickets" && result.Rows[0].TicketKey == "" || kind == "findings" && result.Rows[0].FindingID == "" || kind == "ci" && result.Rows[1].Excerpt == "" {
				t.Fatal("native evidence was missing", result.Rows)
			}
		})
	}
	if reads.Load() < 10 {
		t.Fatal("the normal provider capture path was not exercised", reads.Load())
	}
}

func TestFakePredictionProviderCapturesARealConflictWithoutChangingIt(t *testing.T) {
	s, _ := fakePredictionFixture(t, "success")
	ai, root, _ := actionServiceFixture(t)
	s.repositories = ai.repositories
	actionTestGit(t, root, "checkout", "-b", "incoming")
	os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 2\n"), 0600)
	actionTestGit(t, root, "commit", "-am", "incoming")
	actionTestGit(t, root, "checkout", "main")
	os.WriteFile(filepath.Join(root, "source.ts"), []byte("const value = 3\n"), 0600)
	actionTestGit(t, root, "commit", "-am", "current")
	merge := exec.Command("git", "merge", "incoming")
	merge.Dir = root
	if merge.Run() == nil {
		t.Fatal("expected a real merge conflict")
	}
	ctx := context.Background()
	file, err := s.repositories.Conflict(ctx, root, "source.ts")
	if err != nil {
		t.Fatal(err)
	}
	request := PredictionRequest{Kind: "conflict", Scope: PredictionScope{Root: root}, Path: "source.ts", ConflictToken: file.Token}
	if err := s.SaveSettings(request.Scope, typesafe.FakeModel, true); err != nil {
		t.Fatal(err)
	}
	before := actionTestGit(t, root, "status", "--porcelain=v1")
	result, err := s.Evaluate(ctx, request)
	if err != nil || len(result.Rows) != 2 || result.Rows[0].Answer.Choice != "manual" {
		t.Fatal(result, err)
	}
	after, err := s.repositories.Conflict(ctx, root, "source.ts")
	if err != nil || after.Token != file.Token || actionTestGit(t, root, "status", "--porcelain=v1") != before {
		t.Fatal("fake assessment changed the real conflict or index", err)
	}
}
