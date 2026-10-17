package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
	"time"
)

type IncrementalReviewRequest struct {
	Root         string `json:"root"`
	Host         string `json:"host"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
	Number       int    `json:"number"`
	PreviousHead string `json:"previousHead"`
	Head         string `json:"head"`
}

func (s *RepositoryService) CompareReviewHeads(ctx context.Context, request IncrementalReviewRequest) ([]ChangedFile, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	files, err := s.client.CompareExact(ctx, request.Root, request.PreviousHead, request.Head)
	if err == nil {
		return files, nil
	}
	// If a reviewed commit has fallen out of the local object database, fetch only
	// from a configured matching PR source; never change HEAD, index or FETCH_HEAD.
	for _, sha := range []string{request.PreviousHead, request.Head} {
		if _, err = s.client.ArchiveCommit(ctx, request.Root, gitclient.CommitArchiveRequest{Revision: sha, Host: request.Host, Owner: request.Owner, Repo: request.Repo, PullRequest: request.Number}); err != nil {
			return nil, err
		}
	}
	return s.client.CompareExact(ctx, request.Root, request.PreviousHead, request.Head)
}
