package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"mag-ik-een-review/internal/githubclient"
)

type syncRepositoryFake struct {
	snapshot func(context.Context, string) (RepositorySnapshot, error)
}

func (f syncRepositoryFake) Snapshot(ctx context.Context, path string) (RepositorySnapshot, error) {
	if f.snapshot != nil {
		return f.snapshot(ctx, path)
	}
	if err := ctx.Err(); err != nil {
		return RepositorySnapshot{}, err
	}
	return RepositorySnapshot{Root: path, CurrentBranch: "main"}, nil
}

type syncGitHubFake struct {
	list   func(context.Context, string, string, string, string) ([]githubclient.PullRequestSummary, error)
	detail func(context.Context, string, string, string, int) (*githubclient.PullRequestDetail, error)
}

func (f syncGitHubFake) ListPullRequests(ctx context.Context, host, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
	if f.list != nil {
		return f.list(ctx, host, owner, repo, state)
	}
	return []githubclient.PullRequestSummary{}, ctx.Err()
}
func (f syncGitHubFake) GetPullRequest(ctx context.Context, host, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
	if f.detail != nil {
		return f.detail(ctx, host, owner, repo, number)
	}
	return &githubclient.PullRequestDetail{PullRequest: githubclient.PullRequestSummary{Number: number}}, ctx.Err()
}
func syncHarness(t *testing.T, repo repositoryReader, github pullRequestReader) (*WorkspaceSyncService, chan WorkspaceUpdate) {
	t.Helper()
	updates := make(chan WorkspaceUpdate, 128)
	s := NewWorkspaceSyncService(repo, github, func(update WorkspaceUpdate) {
		select {
		case updates <- update:
		default:
		}
	})
	s.interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.ServiceShutdown() })
	return s, updates
}
func syncNext(t *testing.T, updates <-chan WorkspaceUpdate) WorkspaceUpdate {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for workspace update")
		return WorkspaceUpdate{}
	}
}
func syncRequest() WatchRequest {
	return WatchRequest{Path: "/repo", Host: "github.com", Owner: "owner", Repo: "repo"}
}

func TestWorkspaceSyncDefaultsToFifteenSeconds(t *testing.T) {
	s := NewWorkspaceSyncService(syncRepositoryFake{}, syncGitHubFake{}, nil)
	if s.interval != 15*time.Second {
		t.Fatalf("interval %s", s.interval)
	}
}

func TestWorkspaceSyncTicksAndStopsOnWatchCancellation(t *testing.T) {
	var calls atomic.Int32
	github := syncGitHubFake{list: func(ctx context.Context, host, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
		calls.Add(1)
		if state != "all" {
			t.Errorf("state %s", state)
		}
		return []githubclient.PullRequestSummary{}, ctx.Err()
	}}
	s, updates := syncHarness(t, syncRepositoryFake{}, github)
	s.interval = 5 * time.Millisecond
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	syncNext(t, updates)
	syncNext(t, updates)
	s.StopWatch("/repo")
	before := calls.Load()
	time.Sleep(25 * time.Millisecond)
	if after := calls.Load(); after > before+1 {
		t.Fatalf("polling continued after StopWatch: %d -> %d", before, after)
	}
}

func TestWorkspaceSyncSelectedPullRequestAndZeroSelection(t *testing.T) {
	var mutex sync.Mutex
	selected := []int{}
	github := syncGitHubFake{detail: func(ctx context.Context, host, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
		mutex.Lock()
		selected = append(selected, number)
		mutex.Unlock()
		return &githubclient.PullRequestDetail{PullRequest: githubclient.PullRequestSummary{Number: number}}, ctx.Err()
	}}
	s, updates := syncHarness(t, syncRepositoryFake{}, github)
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	syncNext(t, updates)
	for _, number := range []int{12, 13} {
		if err := s.WatchPullRequest("/repo", number); err != nil {
			t.Fatal(err)
		}
		update := syncNext(t, updates)
		if update.PullRequest == nil || update.PullRequest.PullRequest.Number != number {
			t.Fatal(update)
		}
	}
	if err := s.WatchPullRequest("/repo", 0); err != nil {
		t.Fatal(err)
	}
	update := syncNext(t, updates)
	if update.PullRequest != nil {
		t.Fatal("cleared selection still fetched detail")
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(selected) != 2 || selected[0] != 12 || selected[1] != 13 {
		t.Fatal(selected)
	}
}

func TestWorkspaceSyncProviderErrorPreservesRepositoryAndEmptyListClearsPRs(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	github := syncGitHubFake{list: func(context.Context, string, string, string, string) ([]githubclient.PullRequestSummary, error) {
		if fail.Load() {
			return nil, errors.New("GitHub unavailable")
		}
		return []githubclient.PullRequestSummary{}, nil
	}}
	s, updates := syncHarness(t, syncRepositoryFake{}, github)
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	update := syncNext(t, updates)
	if update.Repository == nil || update.Repository.CurrentBranch != "main" || !strings.Contains(update.Error, "GitHub unavailable") || update.PullRequests != nil {
		t.Fatal(update)
	}
	fail.Store(false)
	if err := s.Refresh(context.Background(), "/repo"); err != nil {
		t.Fatal(err)
	}
	update = syncNext(t, updates)
	if update.PullRequests == nil || len(*update.PullRequests) != 0 || update.Error != "" {
		t.Fatal("empty remote list cannot clear stale UI", update)
	}
}

func TestWorkspaceSyncManualRefreshQueuesBehindActivePollWithoutOverlap(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls, active, maximum atomic.Int32
	github := syncGitHubFake{list: func(ctx context.Context, _ string, _ string, _ string, _ string) ([]githubclient.PullRequestSummary, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, current) {
				break
			}
		}
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return []githubclient.PullRequestSummary{}, nil
	}}
	s, updates := syncHarness(t, syncRepositoryFake{}, github)
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	if err := s.Refresh(context.Background(), "/repo"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping provider requests")
	}
	close(release)
	syncNext(t, updates)
	select {
	case <-updates:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("manual refresh was discarded during active poll")
	}
	if calls.Load() != 2 || maximum.Load() != 1 {
		t.Fatalf("calls %d, max concurrent %d", calls.Load(), maximum.Load())
	}
}

func TestWorkspaceSyncStopWatchCancelsManualRefreshAndSuppressesLateEvent(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	github := syncGitHubFake{list: func(ctx context.Context, _ string, _ string, _ string, _ string) ([]githubclient.PullRequestSummary, error) {
		if calls.Add(1) == 2 {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}
		return []githubclient.PullRequestSummary{}, nil
	}}
	s, updates := syncHarness(t, syncRepositoryFake{}, github)
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	syncNext(t, updates)
	caller, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Refresh(caller, "/repo") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not start")
	}
	s.StopWatch("/repo")
	select {
	case <-canceled:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("StopWatch did not cancel an active manual refresh")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manual refresh did not return")
	}
	select {
	case event := <-updates:
		t.Fatalf("late canceled watch event: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestWorkspaceSyncEmitsFreshLocalSnapshotAfterSlowProvider(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var branch atomic.Value
	branch.Store("before-local-commit")
	repo := syncRepositoryFake{snapshot: func(ctx context.Context, path string) (RepositorySnapshot, error) {
		return RepositorySnapshot{Root: path, CurrentBranch: branch.Load().(string)}, ctx.Err()
	}}
	github := syncGitHubFake{list: func(ctx context.Context, _ string, _ string, _ string, _ string) ([]githubclient.PullRequestSummary, error) {
		close(started)
		select {
		case <-release:
			return []githubclient.PullRequestSummary{}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	s, updates := syncHarness(t, repo, github)
	if err := s.StartWatch(context.Background(), syncRequest()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	branch.Store("after-local-commit")
	close(release)
	update := syncNext(t, updates)
	if update.Repository == nil || update.Repository.CurrentBranch != "after-local-commit" {
		t.Fatal("slow provider emitted stale local repository state", update)
	}
}
