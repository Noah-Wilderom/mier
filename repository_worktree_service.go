package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) Worktrees(ctx context.Context, path string) ([]gitclient.Worktree, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Worktrees(ctx, path)
}
func (s *RepositoryService) MutateWorktree(ctx context.Context, path string, request gitclient.WorktreeRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateWorktree(ctx, path, request)
}
