package reviewproviders

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"strconv"
	"strings"
	"time"
)

// Bound a complete review capture as well as each individual HTTP request.
// Large comparisons must not keep a desktop call alive for hours on a slow host.
func reviewContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 90*time.Second)
}

// Stack discovery uses normalized branch/repository identities on every host.
func providerStack(ctx context.Context, api API, owner, repo string, current githubclient.PullRequestSummary) (githubclient.PullRequestStack, error) {
	items := []githubclient.PullRequestSummary{}
	request := githubclient.PullRequestListRequest{State: "all", PageSize: 100}
	more := false
	for page := 0; page < 10; page++ {
		result, err := api.ListPullRequestsPage(ctx, owner, repo, request)
		if err != nil {
			return githubclient.PullRequestStack{}, err
		}
		items = append(items, result.Items...)
		more = result.HasNextPage
		if !more {
			break
		}
		request.Cursor = result.NextCursor
	}
	result := githubclient.BuildPullRequestStack(current, items, owner+"/"+repo)
	if more {
		result.Warnings = append(result.Warnings, "Stack discovery includes the 1,000 most recently updated pull requests")
	}
	return result, nil
}

func stableID(parts ...string) int64 {
	value, _ := strconv.ParseUint(hash(parts...)[:13], 16, 64)
	return int64(value)
}

func patchCounts(patch string) (int, int) {
	added, removed, hunk := 0, 0, false
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			hunk = true
			continue
		}
		if !hunk {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added++
		} else if strings.HasPrefix(line, "-") {
			removed++
		}
	}
	return added, removed
}

// Full diff paths can contain spaces and C-quoted Unicode. Match the explicit
// ---/+++ headers before the first hunk, never source lines inside a hunk.
func splitFilePatches(diff string) (map[string]string, error) {
	result := map[string]string{}
	for _, block := range strings.Split("\n"+diff, "\ndiff --git ")[1:] {
		lines := strings.Split(block, "\n")
		oldPath, newPath := "", ""
		start := -1
		for i, line := range lines {
			if strings.HasPrefix(line, "@@ ") {
				start = i
				break
			}
			for _, prefix := range []string{"--- ", "+++ "} {
				if !strings.HasPrefix(line, prefix) {
					continue
				}
				path := strings.TrimPrefix(line, prefix)
				if strings.HasPrefix(path, "\"") {
					var err error
					path, err = strconv.Unquote(path)
					if err != nil {
						return nil, errors.New("The provider returned an invalid quoted diff path")
					}
				}
				if path == "/dev/null" {
					path = ""
				} else {
					path = strings.TrimPrefix(strings.TrimPrefix(path, "a/"), "b/")
				}
				if prefix == "--- " {
					oldPath = path
				} else {
					newPath = path
				}
			}
		}
		path := newPath
		if path == "" {
			path = oldPath
		}
		if path == "" {
			continue
		}
		if !validFile(path) {
			return nil, errors.New("The provider returned an invalid diff path")
		}
		if start >= 0 {
			result[path] = strings.Join(lines[start:], "\n")
		}
	}
	return result, nil
}

func setDetailCounts(detail *githubclient.PullRequestDetail) {
	detail.PullRequest.ChangedFiles = len(detail.Files)
	detail.PullRequest.CommitsCount = len(detail.Commits)
	detail.PullRequest.CommentsCount = len(detail.Comments)
	detail.PullRequest.ThreadsCount = len(detail.Threads)
	detail.PullRequest.Additions = 0
	detail.PullRequest.Deletions = 0
	detail.PullRequest.ReviewCommentsCount = 0
	for _, file := range detail.Files {
		detail.PullRequest.Additions += file.Additions
		detail.PullRequest.Deletions += file.Deletions
	}
	for _, thread := range detail.Threads {
		detail.PullRequest.ReviewCommentsCount += len(thread.Comments)
	}
	detail.PullRequest.CountsKnown = len(detail.Warnings) == 0
}

func validateRetarget(input githubclient.RetargetRequest, summary githubclient.PullRequestSummary) error {
	if !immutable(input.ExpectedHead) || input.ExpectedHead != summary.Head.SHA || input.ExpectedBase != summary.Base.Ref {
		return errors.New("The pull request changed. Refresh before changing its base")
	}
	if strings.TrimSpace(input.Base) == "" || len(input.Base) > 1024 || strings.ContainsAny(input.Base, "\x00\r\n") || input.Base == summary.Head.Ref {
		return errors.New("Choose a different valid base branch")
	}
	return nil
}
