package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) Tags(ctx context.Context, path string) ([]gitclient.Tag, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Tags(ctx, path)
}
func (s *RepositoryService) MutateTag(ctx context.Context, path string, request gitclient.TagRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateTag(ctx, path, request)
}
func (s *RepositoryService) MutateRemote(ctx context.Context, path string, request gitclient.RemoteRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateRemote(ctx, path, request)
}
func (s *RepositoryService) SigningConfig(ctx context.Context, path string) (gitclient.SigningConfig, error) {
	if s.initErr != nil {
		return gitclient.SigningConfig{}, s.initErr
	}
	return s.client.SigningConfig(ctx, path)
}
func (s *RepositoryService) SaveSigningConfig(ctx context.Context, path string, request gitclient.SigningConfig) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.SaveSigningConfig(ctx, path, request)
}
func (s *RepositoryService) CommitSignature(ctx context.Context, path, sha string) (gitclient.CommitSignature, error) {
	if s.initErr != nil {
		return gitclient.CommitSignature{}, s.initErr
	}
	return s.client.CommitSignature(ctx, path, sha)
}
func (s *RepositoryService) Checkpoints(ctx context.Context, path string) ([]gitclient.Checkpoint, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Checkpoints(ctx, path)
}
func (s *RepositoryService) Reflog(ctx context.Context, path string) ([]gitclient.ReflogEntry, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Reflog(ctx, path)
}
func (s *RepositoryService) Reset(ctx context.Context, path string, request gitclient.ResetRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.Reset(ctx, path, request)
}
func (s *RepositoryService) RecoverBranch(ctx context.Context, path string, request gitclient.RecoverBranchRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.RecoverBranch(ctx, path, request)
}
func (s *RepositoryService) CherryPickMany(ctx context.Context, path string, request gitclient.CherryPickManyRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.CherryPickMany(ctx, path, request)
}
func (s *RepositoryService) Revert(ctx context.Context, path string, request gitclient.CherryPickRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.Revert(ctx, path, request)
}
