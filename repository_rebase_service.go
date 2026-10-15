package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) PreviewRebase(ctx context.Context, path string, request gitclient.RebasePreviewRequest) (gitclient.RebasePreview, error) {
	if s.initErr != nil {
		return gitclient.RebasePreview{}, s.initErr
	}
	return s.client.PreviewRebase(ctx, path, request)
}
func (s *RepositoryService) Rebase(ctx context.Context, path string, request gitclient.RebaseRequest) (gitclient.OperationResult, error) {
	if s.initErr != nil {
		return gitclient.OperationResult{}, s.initErr
	}
	return s.client.Rebase(ctx, path, request)
}
