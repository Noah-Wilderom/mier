package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"mag-ik-een-review/internal/githubclient"
)

type WatchRequest struct {
	Path  string `json:"path"`
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}
type WorkspaceUpdate struct {
	Path                 string                             `json:"path"`
	Host                 string                             `json:"host"`
	Owner                string                             `json:"owner"`
	Repo                 string                             `json:"repo"`
	Repository           *RepositorySnapshot                `json:"repository,omitempty"`
	PullRequests         *[]githubclient.PullRequestSummary `json:"pullRequests,omitempty"`
	OpenPullRequestCount *int                               `json:"openPullRequestCount,omitempty"`
	PullRequest          *githubclient.PullRequestDetail    `json:"pullRequest,omitempty"`
	Error                string                             `json:"error,omitempty"`
	RepositoryUpdatedAt  string                             `json:"repositoryUpdatedAt,omitempty"`
	UpdatedAt            string                             `json:"updatedAt"`
}
type repositoryReader interface {
	Snapshot(context.Context, string) (RepositorySnapshot, error)
}
type pullRequestReader interface {
	ListPullRequestsPage(context.Context, string, string, string, githubclient.PullRequestListRequest) (githubclient.PullRequestPage, error)
	GetPullRequest(context.Context, string, string, string, int) (*githubclient.PullRequestDetail, error)
}
type workspaceWatch struct {
	request     WatchRequest
	selected    int
	cancel      context.CancelFunc
	signal      chan struct{}
	polling     sync.Mutex
	ctx         context.Context
	pending     atomic.Bool
	refreshedAt time.Time
}

// WorkspaceSyncService runs one cancellation-aware, non-overlapping poller per repository.
// Code review snapshots are retained by the frontend until the reviewer accepts a new head.
type WorkspaceSyncService struct {
	repositories    repositoryReader
	github          pullRequestReader
	emit            func(WorkspaceUpdate)
	mu              sync.Mutex
	watches         map[string]*workspaceWatch
	ctx             context.Context
	interval        time.Duration
	providerTimeout time.Duration
	workers         sync.WaitGroup
	observePRs      func(context.Context, string, string, string, []githubclient.PullRequestSummary) error
}

func NewWorkspaceSyncService(repositories repositoryReader, github pullRequestReader, emit func(WorkspaceUpdate)) *WorkspaceSyncService {
	return &WorkspaceSyncService{repositories: repositories, github: github, emit: emit, watches: map[string]*workspaceWatch{}, ctx: context.Background(), interval: 15 * time.Second, providerTimeout: 90 * time.Second}
}
func (s *WorkspaceSyncService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	return nil
}
func (s *WorkspaceSyncService) ServiceShutdown() error {
	s.mu.Lock()
	for _, watch := range s.watches {
		watch.cancel()
	}
	s.watches = map[string]*workspaceWatch{}
	s.mu.Unlock()
	s.workers.Wait()
	return nil
}
func (s *WorkspaceSyncService) StartWatch(ctx context.Context, request WatchRequest) error {
	snapshot, err := s.repositories.Snapshot(ctx, request.Path)
	if err != nil {
		return err
	}
	request.Path = snapshot.Root
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.watches[request.Path]; ok {
		old.cancel()
	}
	workerCtx, cancel := context.WithCancel(s.ctx)
	watch := &workspaceWatch{request: request, ctx: workerCtx, cancel: cancel, signal: make(chan struct{}, 1)}
	s.watches[request.Path] = watch
	s.workers.Add(1)
	go s.run(workerCtx, watch)
	return nil
}
func (s *WorkspaceSyncService) StopWatch(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if watch, ok := s.watches[path]; ok {
		watch.cancel()
		delete(s.watches, path)
	}
}
func (s *WorkspaceSyncService) WatchPullRequest(path string, number int) error {
	if number < 0 {
		return errors.New("Invalid pull request number")
	}
	s.mu.Lock()
	watch, ok := s.watches[path]
	if !ok {
		s.mu.Unlock()
		return errors.New("Repository is not being watched")
	}
	watch.selected = number
	s.mu.Unlock()
	select {
	case watch.signal <- struct{}{}:
	default:
	}
	return nil
}
func (s *WorkspaceSyncService) Refresh(ctx context.Context, path string) error {
	s.mu.Lock()
	watch, ok := s.watches[path]
	s.mu.Unlock()
	if !ok {
		return errors.New("Repository is not being watched")
	}
	return s.poll(ctx, watch)
}
func (s *WorkspaceSyncService) run(ctx context.Context, watch *workspaceWatch) {
	defer s.workers.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	_ = s.poll(ctx, watch)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.poll(ctx, watch)
		case <-watch.signal:
			_ = s.poll(ctx, watch)
		}
	}
}
func (s *WorkspaceSyncService) poll(ctx context.Context, watch *workspaceWatch) error {
	if !watch.polling.TryLock() {
		watch.pending.Store(true)
		return nil
	}
	defer func() {
		watch.polling.Unlock()
		if watch.pending.Swap(false) {
			select {
			case watch.signal <- struct{}{}:
			default:
			}
		}
	}()
	s.mu.Lock()
	request, selected := watch.request, watch.selected
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopCancel := context.AfterFunc(watch.ctx, cancel)
	defer stopCancel()
	pollStarted := time.Now().UTC()
	update := WorkspaceUpdate{Path: request.Path, Host: request.Host, Owner: request.Owner, Repo: request.Repo, UpdatedAt: pollStarted.Format(time.RFC3339Nano)}
	snapshot, err := s.repositories.Snapshot(ctx, request.Path)
	if err == nil {
		update.RepositoryUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		update.Repository = &snapshot
	} else {
		update.Error = err.Error()
	}
	if request.Host != "" && request.Owner != "" && request.Repo != "" {
		// First refresh reads one recent page. Later refreshes read back only to
		// the previous successful checkpoint, so notifications see every change.
		listCtx, listCancel := context.WithTimeout(ctx, s.providerTimeout)
		page, prErr := s.recentPullRequests(listCtx, request, watch.refreshedAt)
		listCancel()
		if prErr == nil {
			update.PullRequests = &page.Items
			update.OpenPullRequestCount = &page.OpenCount
		} else {
			if update.Error != "" {
				update.Error += "; "
			}
			update.Error += prErr.Error()
		}
		if selected > 0 {
			detailCtx, detailCancel := context.WithTimeout(ctx, s.providerTimeout)
			detail, detailErr := s.github.GetPullRequest(detailCtx, request.Host, request.Owner, request.Repo, selected)
			detailCancel()
			if detailErr == nil {
				update.PullRequest = detail
			} else {
				if update.Error != "" {
					update.Error += "; "
				}
				update.Error += detailErr.Error()
			}
		}
	}
	if request.Host != "" && update.Repository != nil {
		capturedAt := time.Now().UTC().Format(time.RFC3339Nano)
		latest, latestErr := s.repositories.Snapshot(ctx, request.Path)
		if latestErr == nil {
			update.Repository = &latest
			update.RepositoryUpdatedAt = capturedAt
		} else {
			update.Repository = nil
		}
	}
	update.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if ctx.Err() != nil && update.Error == "" {
		update.Error = ctx.Err().Error()
	}
	s.mu.Lock()
	active := s.watches[request.Path] == watch
	s.mu.Unlock()
	if active && ctx.Err() == nil && update.PullRequests != nil {
		var observeErr error
		if s.observePRs != nil {
			observeErr = s.observePRs(ctx, request.Host, request.Owner, request.Repo, *update.PullRequests)
		}
		if observeErr != nil {
			if update.Error != "" {
				update.Error += "; "
			}
			update.Error += "Notifications: " + observeErr.Error()
		} else if ctx.Err() == nil {
			// GitHub timestamps have second precision. Overlap the boundary so
			// an update during this request cannot fall between checkpoints.
			watch.refreshedAt = pollStarted.Add(-time.Second).Truncate(time.Second)
		}
	}
	if active && ctx.Err() != context.Canceled && s.emit != nil {
		s.emit(update)
	}
	if update.Error != "" {
		return errors.New(update.Error)
	}
	return nil
}

func (s *WorkspaceSyncService) recentPullRequests(ctx context.Context, request WatchRequest, since time.Time) (githubclient.PullRequestPage, error) {
	query := githubclient.PullRequestListRequest{State: "all", View: "inbox", PageSize: 25}
	result, err := s.github.ListPullRequestsPage(ctx, request.Host, request.Owner, request.Repo, query)
	if err != nil || since.IsZero() {
		return result, err
	}
	page := result
	cursors := map[string]bool{}
	for page.HasNextPage {
		if len(page.Items) == 0 {
			return githubclient.PullRequestPage{}, errors.New("GitHub returned an empty PR page with a continuation cursor")
		}
		oldest, parseErr := time.Parse(time.RFC3339Nano, page.Items[len(page.Items)-1].UpdatedAt)
		if parseErr == nil && oldest.Before(since) {
			break
		}
		if page.NextCursor == "" || cursors[page.NextCursor] || len(cursors) >= 1000 {
			return githubclient.PullRequestPage{}, errors.New("GitHub returned an invalid PR refresh cursor")
		}
		query.Cursor = page.NextCursor
		cursors[query.Cursor] = true
		page, err = s.github.ListPullRequestsPage(ctx, request.Host, request.Owner, request.Repo, query)
		if err != nil {
			return githubclient.PullRequestPage{}, err
		}
		result.Items = append(result.Items, page.Items...)
	}
	return result, nil
}
