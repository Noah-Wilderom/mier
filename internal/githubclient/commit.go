package githubclient

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type CommitDiff struct {
	SHA      string        `json:"sha"`
	Files    []ChangedFile `json:"files"`
	Warnings []string      `json:"warnings"`
}

// GitHub paginates the files inside the commit object, rather than an array.
// Always inspect the immutable SHA selected from the PR's commit list.
func (c *Client) GetCommitDiff(ctx context.Context, owner, repo, sha string) (*CommitDiff, error) {
	if _, err := hex.DecodeString(sha); err != nil || (len(sha) != 40 && len(sha) != 64) {
		return nil, errors.New("Choose a full commit SHA to inspect")
	}
	path, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	result := &CommitDiff{SHA: sha, Files: []ChangedFile{}, Warnings: []string{}}
	for page := 1; page <= 30; page++ {
		var batch struct {
			SHA   string        `json:"sha"`
			Files []ChangedFile `json:"files"`
		}
		headers, err := c.request(ctx, http.MethodGet, path+"/commits/"+sha+"?per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(batch.SHA, sha) {
			return nil, errors.New("GitHub returned a different commit than the selected SHA")
		}
		result.Files = append(result.Files, batch.Files...)
		if len(result.Files) > 3000 {
			return nil, errors.New("GitHub commit diff exceeds the 3000-file limit")
		}
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			if len(result.Files) == 3000 {
				result.Warnings = append(result.Warnings, "GitHub returns at most 3000 changed files for a commit. Inspect the local repository for a complete comparison.")
			}
			return result, nil
		}
	}
	return nil, errors.New("GitHub commit diff exceeds the 30-page limit")
}
