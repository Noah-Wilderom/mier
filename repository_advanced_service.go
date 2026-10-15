package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) ExportPatch(ctx context.Context, path string, request gitclient.ExportPatchRequest) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.ExportPatch(ctx, path, request)
}
func (s *RepositoryService) PreviewPatch(ctx context.Context, path string, request gitclient.ApplyPatchRequest) (gitclient.PatchPreview, error) {
	if s.initErr != nil {
		return gitclient.PatchPreview{}, s.initErr
	}
	return s.client.PreviewPatch(ctx, path, request)
}
func (s *RepositoryService) ApplyPatch(ctx context.Context, path string, request gitclient.ApplyPatchRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.ApplyPatch(ctx, path, request)
}
func (s *RepositoryService) Submodules(ctx context.Context, path string) ([]gitclient.Submodule, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Submodules(ctx, path)
}
func (s *RepositoryService) MutateSubmodule(ctx context.Context, path string, request gitclient.SubmoduleRequest) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.MutateSubmodule(ctx, path, request)
}
func (s *RepositoryService) LFS(ctx context.Context, path string) (gitclient.LFSState, error) {
	if s.initErr != nil {
		return gitclient.LFSState{}, s.initErr
	}
	return s.client.LFS(ctx, path)
}
func (s *RepositoryService) MutateLFS(ctx context.Context, path string, request gitclient.LFSRequest) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.MutateLFS(ctx, path, request)
}
func (s *RepositoryService) LFSLocks(ctx context.Context, path, remote string) ([]gitclient.LFSLock, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.LFSLocks(ctx, path, remote)
}
func (s *RepositoryService) Bisect(ctx context.Context, path string, request gitclient.BisectRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.Bisect(ctx, path, request)
}
func (s *RepositoryService) Flow(ctx context.Context, path string) (gitclient.FlowState, error) {
	if s.initErr != nil {
		return gitclient.FlowState{}, s.initErr
	}
	return s.client.Flow(ctx, path)
}
func (s *RepositoryService) MutateFlow(ctx context.Context, path string, request gitclient.FlowRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.MutateFlow(ctx, path, request)
}
