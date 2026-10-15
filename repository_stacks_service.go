package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/githubclient"
	"time"
)

func (s *RepositoryService) BranchStacks(ctx context.Context, path string) (gitclient.BranchStacks, error) {
	if s.initErr != nil {
		return gitclient.BranchStacks{}, s.initErr
	}
	return s.client.BranchStacks(ctx, path)
}
func (s *RepositoryService) StackForkPoint(ctx context.Context, path, parent string) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.StackForkPoint(ctx, path, parent)
}
func (s *RepositoryService) MutateStackBranch(ctx context.Context, path string, request gitclient.StackBranchRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateStackBranch(ctx, path, request)
}
func (s *RepositoryService) PreviewStackPush(ctx context.Context, path string, request gitclient.StackPushRequest) (gitclient.StackPushPreview, error) {
	if s.initErr != nil {
		return gitclient.StackPushPreview{}, s.initErr
	}
	return s.client.PreviewStackPush(ctx, path, request)
}
func (s *RepositoryService) PushStackBranch(ctx context.Context, path string, request gitclient.StackPushRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.PushStackBranch(ctx, path, request)
}
func (s *GitHubService) GetPullRequestStack(ctx context.Context, host, owner, repo string, number int) (githubclient.PullRequestStack, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.PullRequestStack{}, err
	}
	return client.GetPullRequestStack(ctx, owner, repo, number)
}
func (s *GitHubService) RetargetPullRequest(ctx context.Context, host, owner, repo string, number int, request githubclient.RetargetRequest) (githubclient.PullRequestSummary, error) {
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	return client.RetargetPullRequest(ctx, owner, repo, number, request)
}
