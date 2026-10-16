package main

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/reviewproviders"
	"strings"
	"time"
)

// Both sides are captured from the PR's immutable comparison, including fork
// heads and renamed paths. This deliberately bypasses LSP workspace preparation.
type ReviewFileRequest struct {
	Host        string `json:"host"`
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
	PullRequest int    `json:"pullRequest"`
	HeadSHA     string `json:"headSha"`
	BaseSHA     string `json:"baseSha"`
	Path        string `json:"path"`
}
type ReviewFileContent struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	HeadSHA      string `json:"headSha"`
	BaseSHA      string `json:"baseSha"`
	MergeBaseSHA string `json:"mergeBaseSha"`
	Old          string `json:"old"`
	New          string `json:"new"`
	OldExists    bool   `json:"oldExists"`
	NewExists    bool   `json:"newExists"`
}

func (s *GitHubService) GetReviewFile(ctx context.Context, request ReviewFileRequest) (ReviewFileContent, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client, err := s.reviewClient(ctx, request.Host)
	if err != nil {
		return ReviewFileContent{}, err
	}
	return readCapturedReviewFile(ctx, client, request)
}
func readCapturedReviewFile(ctx context.Context, client reviewproviders.API, request ReviewFileRequest) (ReviewFileContent, error) {
	changes, err := client.ReviewChanges(ctx, request.Owner, request.Repository, request.PullRequest, request.HeadSHA, request.BaseSHA)
	if err != nil {
		return ReviewFileContent{}, err
	}
	result := ReviewFileContent{Path: request.Path, HeadSHA: request.HeadSHA, BaseSHA: request.BaseSHA, MergeBaseSHA: changes.MergeBaseSHA}
	for _, file := range changes.Files {
		if file.Path != request.Path {
			continue
		}
		result.PreviousPath = file.PreviousPath
		result.OldExists = file.Status != "added"
		result.NewExists = file.Status != "removed" && file.Status != "deleted"
		if result.OldExists {
			path := file.PreviousPath
			if path == "" {
				path = file.Path
			}
			result.Old, err = client.ReadFileAtCommit(ctx, request.Owner, request.Repository, path, changes.MergeBaseSHA)
			if err != nil {
				return ReviewFileContent{}, err
			}
		}
		if result.NewExists {
			owner, repo := request.Owner, request.Repository
			if full := changes.PullRequest.Head.Repository; strings.LastIndex(full, "/") > 0 {
				last := strings.LastIndex(full, "/")
				owner, repo = full[:last], full[last+1:]
			}
			result.New, err = client.ReadFileAtCommit(ctx, owner, repo, file.Path, request.HeadSHA)
			if err != nil {
				return ReviewFileContent{}, err
			}
		}
		return result, nil
	}
	return ReviewFileContent{}, errors.New("This file is not part of the captured pull request")
}
