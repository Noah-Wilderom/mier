package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"mag-ik-een-review/internal/githubclient"
)

type GitHubService struct {
	credentials *CredentialService
	mu          sync.Mutex
	clients     map[string]*githubclient.Client
}

func NewGitHubService(credentials *CredentialService) *GitHubService {
	return &GitHubService{credentials: credentials, clients: map[string]*githubclient.Client{}}
}
func (s *GitHubService) client(ctx context.Context, input string) (*githubclient.Client, error) {
	host, token, _, err := s.credentials.resolveToken(ctx, input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(token))
	key := host + ":" + hex.EncodeToString(hash[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if client := s.clients[key]; client != nil {
		return client, nil
	}
	client, err := githubclient.New(host, token)
	if err != nil {
		return nil, err
	}
	// One cached client per host prevents stale token cache growth after account changes.
	for oldKey := range s.clients {
		if len(oldKey) > len(host) && oldKey[:len(host)+1] == host+":" {
			delete(s.clients, oldKey)
		}
	}
	s.clients[key] = client
	return client, nil
}
func (s *GitHubService) ListPullRequests(ctx context.Context, host, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return nil, err
	}
	return client.ListPullRequests(ctx, owner, repo, state)
}
func (s *GitHubService) GetPullRequest(ctx context.Context, host, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return nil, err
	}
	return client.GetPullRequest(ctx, owner, repo, number)
}
func (s *GitHubService) CreatePullRequest(ctx context.Context, host, owner, repo string, input githubclient.CreateRequest) (githubclient.PullRequestSummary, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	return client.CreatePullRequest(ctx, owner, repo, input)
}
func (s *GitHubService) UpdatePullRequest(ctx context.Context, host, owner, repo string, number int, input githubclient.UpdateRequest) (githubclient.PullRequestSummary, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	return client.UpdatePullRequest(ctx, owner, repo, number, input)
}
func (s *GitHubService) SetDraft(ctx context.Context, host, owner, repo string, number int, draft bool) error {
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.SetDraft(ctx, owner, repo, number, draft)
}
func (s *GitHubService) AddIssueComment(ctx context.Context, host, owner, repo string, number int, body string) (githubclient.Comment, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.Comment{}, err
	}
	return client.AddIssueComment(ctx, owner, repo, number, body)
}
func (s *GitHubService) AddReviewComment(ctx context.Context, host, owner, repo string, number int, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.Comment{}, err
	}
	return client.AddReviewComment(ctx, owner, repo, number, input)
}
func (s *GitHubService) SubmitReview(ctx context.Context, host, owner, repo string, number int, input githubclient.ReviewRequest) (githubclient.Review, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.Review{}, err
	}
	return client.SubmitReview(ctx, owner, repo, number, input)
}
func (s *GitHubService) ReplyReviewComment(ctx context.Context, host, owner, repo string, number int, commentID int64, body string) (githubclient.Comment, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.Comment{}, err
	}
	return client.ReplyReviewComment(ctx, owner, repo, number, commentID, body)
}
func (s *GitHubService) ResolveThread(ctx context.Context, host, threadID string, resolved bool) error {
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.ResolveThread(ctx, threadID, resolved)
}
func (s *GitHubService) MergePullRequest(ctx context.Context, host, owner, repo string, number int, method, expectedHeadSHA string) (githubclient.MergeResult, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.MergeResult{}, err
	}
	return client.MergePullRequest(ctx, owner, repo, number, method, expectedHeadSHA)
}

func (s *GitHubService) RequestReviewers(ctx context.Context, host, owner, repo string, number int, reviewers []string) (githubclient.PullRequestSummary, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	return client.RequestReviewers(ctx, owner, repo, number, reviewers)
}

func (s *GitHubService) RemoveReviewers(ctx context.Context, host, owner, repo string, number int, reviewers []string) error {
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.RemoveReviewers(ctx, owner, repo, number, reviewers)
}
func (s *GitHubService) SetAssignees(ctx context.Context, host, owner, repo string, number int, assignees []string) error {
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.SetAssignees(ctx, owner, repo, number, assignees)
}
func (s *GitHubService) SetLabels(ctx context.Context, host, owner, repo string, number int, labels []string) ([]string, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return nil, err
	}
	return client.SetLabels(ctx, owner, repo, number, labels)
}
