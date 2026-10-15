package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) Stashes(ctx context.Context, path string) ([]gitclient.Stash, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Stashes(ctx, path)
}
func (s *RepositoryService) StashDiff(ctx context.Context, path, sha string) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.StashDiff(ctx, path, sha)
}
func (s *RepositoryService) SaveStash(ctx context.Context, path string, request gitclient.SaveStashRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.SaveStash(ctx, path, request)
}
func (s *RepositoryService) MutateStash(ctx context.Context, path string, request gitclient.StashRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateStash(ctx, path, request)
}
func (s *RepositoryService) Amend(ctx context.Context, path string, request gitclient.AmendRequest) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.Amend(ctx, path, request)
}
func (s *RepositoryService) PreviewDiscard(ctx context.Context, path, file string) (gitclient.DiscardPreview, error) {
	if s.initErr != nil {
		return gitclient.DiscardPreview{}, s.initErr
	}
	return s.client.PreviewDiscard(ctx, path, file)
}
func (s *RepositoryService) Discard(ctx context.Context, path string, request gitclient.DiscardRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Discard(ctx, path, request)
}
func (s *RepositoryService) DiscardBackups(ctx context.Context, path string) ([]gitclient.DiscardBackup, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.DiscardBackups(ctx, path)
}
func (s *RepositoryService) RestoreDiscard(ctx context.Context, path, id string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.RestoreDiscard(ctx, path, id)
}
func (s *RepositoryService) IgnoreFile(ctx context.Context, path, file string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.IgnoreFile(ctx, path, file)
}
