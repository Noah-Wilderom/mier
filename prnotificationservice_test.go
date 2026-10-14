package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"mag-ik-een-review/internal/githubclient"
)

type notificationCredentialsFake struct {
	mu     sync.Mutex
	status AccountStatus
	err    error
	calls  int
}

func (f *notificationCredentialsFake) AccountStatus(context.Context, string) (AccountStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.status, f.err
}

func notificationHarness(t *testing.T) (*PRNotificationService, *[]PRNotificationEvent, *notificationCredentialsFake) {
	t.Helper()
	events := []PRNotificationEvent{}
	credentials := &notificationCredentialsFake{status: AccountStatus{Authenticated: true, Login: "noah"}}
	s := NewPRNotificationService(credentials, func(event PRNotificationEvent) { events = append(events, event) }, func() error { return nil })
	s.configPath = filepath.Join(t.TempDir(), "notifications.json")
	s.configError = nil
	s.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	return s, &events, credentials
}

func notificationPR(number int, status string, reviewers ...string) githubclient.PullRequestSummary {
	pr := githubclient.PullRequestSummary{Number: number, Status: status, Title: "Improve code review", URL: "https://github.com/team/repo/pull/1", CreatedAt: "2026-10-13T09:00:00Z", UpdatedAt: "2026-10-13T10:00:00Z", Author: githubclient.User{Login: "elena"}}
	for _, reviewer := range reviewers {
		pr.Reviewers = append(pr.Reviewers, githubclient.User{Login: reviewer})
	}
	return pr
}

func observeNotifications(t *testing.T, service *PRNotificationService, prs ...githubclient.PullRequestSummary) {
	t.Helper()
	if err := service.Observe(context.Background(), "github.com", "Team", "Repo", prs); err != nil {
		t.Fatal(err)
	}
}

func notificationTypes(events []PRNotificationEvent) []string {
	types := []string{}
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func TestPRNotificationsIgnoreOlderCloneResponses(t *testing.T) {
	s, events, _ := notificationHarness(t)
	opened := notificationPR(1, "open")
	observeNotifications(t, s, opened)
	merged := notificationPR(1, "merged")
	merged.UpdatedAt = "2026-10-13T10:01:00Z"
	observeNotifications(t, s, merged)
	observeNotifications(t, s, opened)
	observeNotifications(t, s, merged)
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"merged"}) {
		t.Fatalf("Stale clone response produced duplicate transitions: %v", got)
	}
}

func TestPRNotificationsDefaultsAndPersistentRepositoryIdentity(t *testing.T) {
	s, _, _ := notificationHarness(t)
	prefs, err := s.GetPreferences("GitHub.com/Team/Repo")
	if err != nil || prefs != defaultNotificationPreferences() || !prefs.ToastsEnabled || prefs.DesktopEnabled {
		t.Fatalf("Incorrect defaults: %+v, %v", prefs, err)
	}
	prefs.Opened, prefs.ToastsEnabled, prefs.ReviewRequested = false, false, false
	if err := s.SavePreferences("github.com/TEAM/REPO", prefs); err != nil {
		t.Fatal(err)
	}
	copy := NewPRNotificationService(nil, nil, nil)
	copy.configPath, copy.configError = s.configPath, nil
	loaded, err := copy.GetPreferences("github.com/team/repo")
	if err != nil || loaded != prefs {
		t.Fatalf("Explicit overrides were lost: %+v, %v", loaded, err)
	}
	for _, key := range []string{"github.com/team/other", "github.enterprise.test/team/repo"} {
		loaded, err := copy.GetPreferences(key)
		if err != nil || loaded != defaultNotificationPreferences() {
			t.Fatalf("Preferences leaked into %s: %+v, %v", key, loaded, err)
		}
	}
	info, err := os.Stat(s.configPath)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("Unexpected preference file permissions: %v, %v", info, err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(s.configPath), ".notifications-*"))
	if len(files) != 0 {
		t.Fatalf("Temporary files left behind: %v", files)
	}
}

func TestPRNotificationsPermissionFailureDoesNotEnableDesktop(t *testing.T) {
	s, _, _ := notificationHarness(t)
	permissionCalls := 0
	s.requestPermission = func() error { permissionCalls++; return errors.New("Permission denied") }
	prefs := defaultNotificationPreferences()
	prefs.DesktopEnabled = true
	if err := s.SavePreferences("github.com/team/repo", prefs); err == nil {
		t.Fatal("Desktop save bypassed denied permission")
	}
	loaded, _ := s.GetPreferences("github.com/team/repo")
	if loaded.DesktopEnabled || permissionCalls != 1 {
		t.Fatalf("Denied preference was saved: %+v, calls=%d", loaded, permissionCalls)
	}
	s.requestPermission = func() error { permissionCalls++; return nil }
	if err := s.RequestDesktopPermission(); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePreferences("github.com/team/repo", prefs); err != nil {
		t.Fatal(err)
	}
	if permissionCalls != 2 {
		t.Fatalf("Already granted permission prompted again: %d", permissionCalls)
	}
}

func TestPRNotificationsPreferencesFailureIsVisibleAndDoesNotOverwrite(t *testing.T) {
	s, _, _ := notificationHarness(t)
	bad := []byte("{broken preferences")
	if err := os.WriteFile(s.configPath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPreferences("github.com/team/repo"); err == nil {
		t.Fatal("Corrupt configuration was silently accepted")
	}
	if err := s.SavePreferences("github.com/team/repo", defaultNotificationPreferences()); err == nil {
		t.Fatal("Corrupt configuration was overwritten")
	}
	data, _ := os.ReadFile(s.configPath)
	if string(data) != string(bad) {
		t.Fatal("Corrupt file was replaced")
	}
	s2, _, _ := notificationHarness(t)
	_, _ = s2.GetPreferences("github.com/team/repo")
	s2.configPath = filepath.Dir(s2.configPath) // Rename to an existing directory must fail.
	prefs := defaultNotificationPreferences()
	prefs.Opened = false
	if err := s2.SavePreferences("github.com/team/repo", prefs); err == nil {
		t.Fatal("Invalid save destination unexpectedly succeeded")
	}
	loaded, _ := s2.GetPreferences("github.com/team/repo")
	if !loaded.Opened {
		t.Fatal("Failed save changed the in-memory preference")
	}
}

func TestPRNotificationsNewFieldsUseDefaultsWithoutLosingExplicitFalse(t *testing.T) {
	s, _, _ := notificationHarness(t)
	data := `{"version":1,"repositories":{"github.com/team/repo":{"opened":false}}}`
	if err := os.WriteFile(s.configPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetPreferences("github.com/team/repo")
	if err != nil || loaded.Opened || !loaded.ToastsEnabled || !loaded.Closed || !loaded.ReviewRequested || loaded.DesktopEnabled {
		t.Fatalf("Incorrect defaults for newly added fields: %+v, %v", loaded, err)
	}
}

func TestPRNotificationsBaselineAndStatusTransitions(t *testing.T) {
	s, events, _ := notificationHarness(t)
	observeNotifications(t, s, notificationPR(1, "open", "noah"), notificationPR(2, "draft"), notificationPR(3, "closed"))
	if len(*events) != 0 {
		t.Fatal("Initial import flooded notifications")
	}
	newDraft := notificationPR(4, "draft")
	newDraft.CreatedAt, newDraft.UpdatedAt = "2026-10-13T12:01:00Z", "2026-10-13T12:01:00Z"
	s.now = func() time.Time { return time.Date(2026, 10, 13, 12, 2, 0, 0, time.UTC) }
	observeNotifications(t, s, notificationPR(1, "merged"), notificationPR(2, "closed"), notificationPR(3, "open"), newDraft, notificationPR(5, "closed"))
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"merged", "closed", "opened", "opened"}) {
		t.Fatalf("Wrong transition events: %v", got)
	}
	for _, event := range *events {
		if !event.Toast || event.Desktop || event.ID == "" || event.CreatedAt == "" || event.Key != "github.com/team/repo" || event.Title != "Improve code review" {
			t.Fatalf("Incorrect payload: %+v", event)
		}
		if event.Type != "opened" && event.Actor != "" {
			t.Fatal("Unknown closer/merger was attributed to the PR author")
		}
	}
	newDraft.Status = "open"
	observeNotifications(t, s, notificationPR(1, "merged"), notificationPR(2, "closed"), notificationPR(3, "draft"), newDraft)
	if len(*events) != 4 {
		t.Fatal("Identical poll or draft-ready transition was duplicated")
	}
}

func TestPRNotificationsHistoricalWindowEntriesBecomeBaseline(t *testing.T) {
	for _, createdAt := range []string{"2026-10-13T09:00:00Z", "2026-10-13T12:00:00Z", "", "invalid"} {
		t.Run(createdAt, func(t *testing.T) {
			s, events, _ := notificationHarness(t)
			observeNotifications(t, s, notificationPR(1, "open"))
			entry := notificationPR(2, "open", "noah")
			entry.CreatedAt, entry.UpdatedAt = createdAt, "2026-10-13T12:01:00Z"
			observeNotifications(t, s, entry)
			if len(*events) != 0 {
				t.Fatalf("Previously unseen history fabricated events: %+v", *events)
			}
			// Deltas omit unaffected rows, but explicit later transitions still emit.
			entry.Status, entry.UpdatedAt = "closed", "2026-10-13T12:02:00Z"
			observeNotifications(t, s, entry, notificationPR(1, "closed"))
			if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"closed", "closed"}) {
				t.Fatalf("Known PR transitions in a delta were missed: %v", got)
			}
		})
	}
}

func TestPRNotificationsNewPullRequestsAfterBaselineEmit(t *testing.T) {
	s, events, _ := notificationHarness(t)
	observeNotifications(t, s)
	pr := notificationPR(1, "draft", "noah")
	pr.CreatedAt, pr.UpdatedAt = "2026-10-13T12:01:00Z", "2026-10-13T12:01:00Z"
	s.now = func() time.Time { return time.Date(2026, 10, 13, 12, 2, 0, 0, time.UTC) }
	observeNotifications(t, s, pr)
	observeNotifications(t, s, pr)
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"opened", "reviewRequested"}) {
		t.Fatalf("New PR events were missed or duplicated: %v", got)
	}
	if (*events)[0].Actor != "elena" {
		t.Fatalf("New PR opener was lost: %+v", (*events)[0])
	}
}

func TestPRNotificationsCreationBaselineIsRepositorySpecific(t *testing.T) {
	s, events, _ := notificationHarness(t)
	observeNotifications(t, s)
	s.now = func() time.Time { return time.Date(2026, 10, 13, 12, 5, 0, 0, time.UTC) }
	if err := s.Observe(context.Background(), "github.com", "team", "other", nil); err != nil {
		t.Fatal(err)
	}
	pr := notificationPR(1, "open")
	pr.CreatedAt, pr.UpdatedAt = "2026-10-13T12:01:00Z", "2026-10-13T12:06:00Z"
	s.now = func() time.Time { return time.Date(2026, 10, 13, 12, 6, 0, 0, time.UTC) }
	observeNotifications(t, s, pr)
	if err := s.Observe(context.Background(), "github.com", "team", "other", []githubclient.PullRequestSummary{pr}); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 || (*events)[0].Key != "github.com/team/repo" || (*events)[0].Type != "opened" {
		t.Fatalf("Creation baseline leaked between repositories: %+v", *events)
	}
}

func TestPRNotificationsReviewRequestTransitionsUseAuthenticatedViewer(t *testing.T) {
	s, events, credentials := notificationHarness(t)
	observeNotifications(t, s, notificationPR(1, "open"))
	observeNotifications(t, s, notificationPR(1, "open", "elena"))
	observeNotifications(t, s, notificationPR(1, "open", "NOAH", "elena"))
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	observeNotifications(t, s, notificationPR(1, "open"))
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	observeNotifications(t, s, notificationPR(1, "closed", "noah"))
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"reviewRequested", "reviewRequested", "closed"}) {
		t.Fatalf("Wrong viewer review transitions: %v", got)
	}
	if credentials.calls != 1 {
		t.Fatalf("Account lookup was not cached: %d", credentials.calls)
	}
}

func TestPRNotificationsAccountFailuresDoNotFabricateReviewRequests(t *testing.T) {
	s, events, credentials := notificationHarness(t)
	clock := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	credentials.status = AccountStatus{}
	credentials.err = errors.New("Offline")
	observeNotifications(t, s, notificationPR(1, "open"))
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	clock = clock.Add(time.Minute)
	credentials.status = AccountStatus{Authenticated: true, Login: "noah"}
	credentials.err = nil
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	if len(*events) != 0 {
		t.Fatal("Reconnect fabricated a new request from unchanged reviewers")
	}
	observeNotifications(t, s, notificationPR(1, "open"))
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"reviewRequested"}) {
		t.Fatalf("Recovered viewer request was missed: %v", got)
	}
}

func TestPRNotificationsEmptyOrMissingListsNeverInferClosed(t *testing.T) {
	s, events, _ := notificationHarness(t)
	observeNotifications(t, s, notificationPR(1, "open"), notificationPR(2, "open"))
	observeNotifications(t, s)
	observeNotifications(t, s, notificationPR(2, "open"))
	observeNotifications(t, s, notificationPR(1, "open"), notificationPR(2, "open"))
	if len(*events) != 0 {
		t.Fatalf("Missing PR caused a notification: %+v", *events)
	}
	observeNotifications(t, s, notificationPR(1, "closed"))
	if got := notificationTypes(*events); !reflect.DeepEqual(got, []string{"closed"}) {
		t.Fatalf("Explicit close was missed: %v", got)
	}
}

func TestPRNotificationsDisabledChannelsAndEventsStillAdvanceBaseline(t *testing.T) {
	s, events, _ := notificationHarness(t)
	observeNotifications(t, s)
	prefs := defaultNotificationPreferences()
	prefs.ToastsEnabled = false
	if err := s.SavePreferences("github.com/team/repo", prefs); err != nil {
		t.Fatal(err)
	}
	observeNotifications(t, s, notificationPR(1, "open", "noah"))
	if len(*events) != 0 {
		t.Fatal("Both channels disabled but event emitted")
	}
	prefs.DesktopEnabled, prefs.Opened = true, false
	if err := s.SavePreferences("github.com/team/repo", prefs); err != nil {
		t.Fatal(err)
	}
	observeNotifications(t, s, notificationPR(1, "open", "noah"), notificationPR(2, "open"))
	if len(*events) != 0 {
		t.Fatal("Enabling a channel replayed previous events or ignored disabled type")
	}
	observeNotifications(t, s, notificationPR(2, "merged"))
	if len(*events) != 1 || (*events)[0].Toast || !(*events)[0].Desktop || (*events)[0].Type != "merged" {
		t.Fatalf("Desktop-only event was incorrect: %+v", *events)
	}
}

func TestPRNotificationsConcurrentClonePollsDeduplicateAndCancel(t *testing.T) {
	s, _, _ := notificationHarness(t)
	var mu sync.Mutex
	events := []PRNotificationEvent{}
	s.emit = func(event PRNotificationEvent) { mu.Lock(); events = append(events, event); mu.Unlock() }
	observeNotifications(t, s, notificationPR(1, "open"))
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_ = s.Observe(context.Background(), "github.com", "TEAM", "REPO", []githubclient.PullRequestSummary{notificationPR(1, "merged")})
		}()
	}
	workers.Wait()
	if len(events) != 1 {
		t.Fatalf("Clone polls duplicated transitions: %d", len(events))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Observe(ctx, "github.com", "team", "repo", []githubclient.PullRequestSummary{notificationPR(2, "open")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Cancelled observation was accepted: %v", err)
	}
}
