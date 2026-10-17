package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) PreviewMerge(ctx context.Context, path string, request gitclient.MergeBranchRequest) (gitclient.MergePreflight, error) {
	if s.initErr != nil {
		return gitclient.MergePreflight{}, s.initErr
	}
	return s.client.PreviewMerge(ctx, path, request)
}

func (s *RepositoryService) CherryPick(ctx context.Context, path string, request gitclient.CherryPickRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.CherryPick(ctx, path, request)
}
func (s *RepositoryService) MergeBranch(ctx context.Context, path string, request gitclient.MergeBranchRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.MergeBranch(ctx, path, request)
}
func (s *RepositoryService) ControlOperation(ctx context.Context, path, token, action string) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.ControlOperation(ctx, path, token, action)
}
func (s *RepositoryService) Conflict(ctx context.Context, path, file string) (gitclient.ConflictFile, error) {
	if s.initErr != nil {
		return gitclient.ConflictFile{}, s.initErr
	}
	return s.client.Conflict(ctx, path, file)
}
func (s *RepositoryService) ResolveConflict(ctx context.Context, path string, request gitclient.ResolveConflictRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.ResolveConflict(ctx, path, request)
}
func (s *RepositoryService) StageHunk(ctx context.Context, path string, request gitclient.StageHunkRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.StageHunk(ctx, path, request)
}
func (s *RepositoryService) BranchCommits(ctx context.Context, path, branch, expectedTip string) ([]gitclient.Commit, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.BranchCommits(ctx, path, branch, expectedTip)
}

func (s *RepositoryService) ForecastConflict(ctx context.Context, path string, request gitclient.ConflictForecastRequest) (gitclient.ConflictForecast, error) {
	if s.initErr != nil {
		return gitclient.ConflictForecast{}, s.initErr
	}
	return s.client.ForecastConflict(ctx, path, request)
}
