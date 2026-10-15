package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

func (s *RepositoryService) CompareExact(ctx context.Context, path, base, head string) ([]gitclient.ChangedFile, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.CompareExact(ctx, path, base, head)
}

func (s *RepositoryService) HistoryPage(ctx context.Context, path string, query gitclient.HistoryQuery) (gitclient.HistoryPage, error) {
	if s.initErr != nil {
		return gitclient.HistoryPage{}, s.initErr
	}
	return s.client.HistoryPage(ctx, path, query)
}
func (s *RepositoryService) RevisionFile(ctx context.Context, path, revision, file string) (gitclient.RevisionFile, error) {
	if s.initErr != nil {
		return gitclient.RevisionFile{}, s.initErr
	}
	return s.client.RevisionFile(ctx, path, revision, file)
}
func (s *RepositoryService) Blame(ctx context.Context, path, revision, file string) ([]gitclient.BlameLine, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Blame(ctx, path, revision, file)
}
