package githubclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

func (c *Client) getPR(ctx context.Context, owner, repo string, number int) (PullRequestSummary, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return PullRequestSummary{}, err
	}
	var pr PullRequestSummary
	_, err = c.request(ctx, http.MethodGet, path, nil, &pr)
	return pr, err
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequestDetail, error) {
	pr, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	path, _ := prPath(owner, repo, number)
	repository, _ := repoPath(owner, repo)
	detail := &PullRequestDetail{PullRequest: pr, Files: []ChangedFile{}, Commits: []Commit{}, Comments: []Comment{}, Reviews: []Review{}, Checks: []CheckRun{}, Statuses: []CommitStatus{}, Threads: []Thread{}, Warnings: []string{}, AllowedMergeMethods: []string{}, RequiredStatusCheckContexts: []string{}, MergeStateStatus: "UNKNOWN"}
	var mutex sync.Mutex
	var firstError error
	var group sync.WaitGroup
	run := func(optional bool, fn func() error) {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := fn(); err != nil {
				mutex.Lock()
				defer mutex.Unlock()
				if optional && permissionError(err) {
					detail.Warnings = append(detail.Warnings, err.Error())
				} else if firstError == nil {
					firstError = err
				}
			}
		}()
	}
	run(false, func() error { var err error; detail.Files, err = list[ChangedFile](ctx, c, path+"/files"); return err })
	run(false, func() error { var err error; detail.Commits, err = list[Commit](ctx, c, path+"/commits"); return err })
	run(false, func() error {
		var err error
		detail.Comments, err = list[Comment](ctx, c, repository+"/issues/"+strconv.Itoa(number)+"/comments")
		return err
	})
	run(false, func() error { var err error; detail.Reviews, err = list[Review](ctx, c, path+"/reviews"); return err })
	run(false, func() error { return c.loadThreads(ctx, owner, repo, number, detail) })
	run(false, func() error {
		if err := c.loadProtection(ctx, owner, repo, number, detail); err != nil {
			mutex.Lock()
			detail.Warnings = append(detail.Warnings, err.Error())
			mutex.Unlock()
		}
		return nil
	})
	run(true, func() error {
		var err error
		detail.Statuses, err = list[CommitStatus](ctx, c, repository+"/commits/"+url.PathEscape(pr.Head.SHA)+"/statuses")
		if err != nil {
			detail.Statuses = []CommitStatus{}
		}
		return err
	})
	run(true, func() error {
		for page := 1; page <= 1000; page++ {
			var batch struct {
				Runs []CheckRun `json:"check_runs"`
			}
			headers, err := c.request(ctx, http.MethodGet, repository+"/commits/"+url.PathEscape(pr.Head.SHA)+"/check-runs?per_page=100&page="+strconv.Itoa(page), nil, &batch)
			if err != nil {
				return err
			}
			detail.Checks = append(detail.Checks, batch.Runs...)
			if !strings.Contains(headers.Get("Link"), `rel="next"`) {
				return nil
			}
		}
		return errors.New("GitHub check pagination exceeded 1000 pages")
	})
	run(true, func() error {
		var metadata struct {
			Merge  bool `json:"allow_merge_commit"`
			Squash bool `json:"allow_squash_merge"`
			Rebase bool `json:"allow_rebase_merge"`
		}
		_, err := c.request(ctx, http.MethodGet, repository, nil, &metadata)
		if err != nil {
			return err
		}
		if metadata.Merge {
			detail.AllowedMergeMethods = append(detail.AllowedMergeMethods, "merge")
		}
		if metadata.Squash {
			detail.AllowedMergeMethods = append(detail.AllowedMergeMethods, "squash")
		}
		if metadata.Rebase {
			detail.AllowedMergeMethods = append(detail.AllowedMergeMethods, "rebase")
		}
		return nil
	})
	group.Wait()
	if firstError != nil {
		return nil, firstError
	}
	latest, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	if latest.Head.SHA != pr.Head.SHA {
		return nil, errors.New("Pull request changed while loading. Refresh to review the current head")
	}
	detail.PullRequest = latest
	detail.PullRequest.ThreadsCount = len(detail.Threads)
	if pr.ChangedFiles > len(detail.Files) {
		detail.Warnings = append(detail.Warnings, "GitHub omitted some changed files (the REST API returns at most 3000). Open the pull request on GitHub for the complete file list")
	}
	for _, file := range detail.Files {
		if file.Patch == "" {
			detail.Warnings = append(detail.Warnings, "No text patch supplied for "+file.Path+"; it may be binary or too large for GitHub's patch response")
		}
	}
	return detail, nil
}

func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, input CreateRequest) (PullRequestSummary, error) {
	path, err := repoPath(owner, repo)
	if err != nil {
		return PullRequestSummary{}, err
	}
	if strings.TrimSpace(input.Title) == "" || input.Head == "" || input.Base == "" || input.Head == input.Base {
		return PullRequestSummary{}, errors.New("Provide a title and different base and head branches")
	}
	var result PullRequestSummary
	_, err = c.request(ctx, http.MethodPost, path+"/pulls", input, &result)
	return result, err
}
func (c *Client) UpdatePullRequest(ctx context.Context, owner, repo string, number int, input UpdateRequest) (PullRequestSummary, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return PullRequestSummary{}, err
	}
	if input.State != nil && *input.State != "open" && *input.State != "closed" {
		return PullRequestSummary{}, errors.New("State must be open or closed")
	}
	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		return PullRequestSummary{}, errors.New("Pull request title cannot be empty")
	}
	var result PullRequestSummary
	_, err = c.request(ctx, http.MethodPatch, path, input, &result)
	return result, err
}
func (c *Client) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) (Comment, error) {
	path, err := repoPath(owner, repo)
	if err != nil {
		return Comment{}, err
	}
	if number < 1 || strings.TrimSpace(body) == "" {
		return Comment{}, errors.New("Provide a pull request number and comment")
	}
	var result Comment
	_, err = c.request(ctx, http.MethodPost, path+"/issues/"+strconv.Itoa(number)+"/comments", map[string]string{"body": body}, &result)
	return result, err
}
func validateInline(input ReviewCommentRequest) error {
	if strings.TrimSpace(input.Body) == "" || input.Path == "" || input.Line < 1 || (input.Side != "LEFT" && input.Side != "RIGHT") {
		return errors.New("Inline comments require a body, file path, positive line, and LEFT or RIGHT diff side")
	}
	return nil
}
func (c *Client) verifyHead(ctx context.Context, owner, repo string, number int, sha string) (PullRequestSummary, error) {
	if strings.TrimSpace(sha) == "" {
		return PullRequestSummary{}, errors.New("An expected head commit SHA is required. Refresh the pull request first")
	}
	pr, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return pr, err
	}
	if !strings.EqualFold(pr.Head.SHA, sha) {
		return pr, errors.New("The pull request head changed. Refresh before submitting a review or merging")
	}
	return pr, nil
}
func (c *Client) AddReviewComment(ctx context.Context, owner, repo string, number int, input ReviewCommentRequest) (Comment, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return Comment{}, err
	}
	if err := validateInline(input); err != nil {
		return Comment{}, err
	}
	if _, err := c.verifyHead(ctx, owner, repo, number, input.CommitID); err != nil {
		return Comment{}, err
	}
	var result Comment
	_, err = c.request(ctx, http.MethodPost, path+"/comments", map[string]any{"body": input.Body, "path": input.Path, "line": input.Line, "side": input.Side, "commit_id": input.CommitID}, &result)
	return result, err
}
func (c *Client) SubmitReview(ctx context.Context, owner, repo string, number int, input ReviewRequest) (Review, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return Review{}, err
	}
	if input.Event != "APPROVE" && input.Event != "REQUEST_CHANGES" && input.Event != "COMMENT" {
		return Review{}, errors.New("Review event must be APPROVE, REQUEST_CHANGES, or COMMENT")
	}
	if (input.Event == "REQUEST_CHANGES" || (input.Event == "COMMENT" && len(input.Comments) == 0)) && strings.TrimSpace(input.Body) == "" {
		return Review{}, errors.New("A review summary is required for comments without inline feedback or requested changes")
	}
	if _, err := c.verifyHead(ctx, owner, repo, number, input.CommitID); err != nil {
		return Review{}, err
	}
	comments := []map[string]any{}
	for _, comment := range input.Comments {
		if err := validateInline(comment); err != nil {
			return Review{}, err
		}
		comments = append(comments, map[string]any{"body": comment.Body, "path": comment.Path, "line": comment.Line, "side": comment.Side})
	}
	var result Review
	_, err = c.request(ctx, http.MethodPost, path+"/reviews", map[string]any{"event": input.Event, "body": input.Body, "commit_id": input.CommitID, "comments": comments}, &result)
	return result, err
}
func (c *Client) ReplyReviewComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) (Comment, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return Comment{}, err
	}
	if commentID < 1 || strings.TrimSpace(body) == "" {
		return Comment{}, errors.New("Provide a review comment ID and reply")
	}
	var result Comment
	_, err = c.request(ctx, http.MethodPost, path+"/comments/"+strconv.FormatInt(commentID, 10)+"/replies", map[string]string{"body": body}, &result)
	return result, err
}
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, method, expectedHeadSHA string) (MergeResult, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return MergeResult{}, err
	}
	if method != "merge" && method != "squash" && method != "rebase" {
		return MergeResult{}, errors.New("Choose merge, squash, or rebase")
	}
	pr, err := c.verifyHead(ctx, owner, repo, number, expectedHeadSHA)
	if err != nil {
		return MergeResult{}, err
	}
	if pr.Status != "open" || pr.Draft {
		return MergeResult{}, errors.New("Only an open pull request ready for review can be merged")
	}
	var result MergeResult
	_, err = c.request(ctx, http.MethodPut, path+"/merge", map[string]string{"merge_method": method, "sha": expectedHeadSHA}, &result)
	if err == nil && !result.Merged {
		err = fmt.Errorf("GitHub did not merge this pull request: %s", result.Message)
	}
	return result, err
}
