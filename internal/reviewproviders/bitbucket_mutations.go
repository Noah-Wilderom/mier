package reviewproviders

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"strconv"
	"strings"
)

func (c *BitbucketClient) updatePR(ctx context.Context, owner, repo string, number int, input any) (githubclient.PullRequestSummary, error) {
	path, e := bbPRPath(owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	var pr bbPR
	_, e = c.api.request(ctx, "PUT", path, input, &pr)
	return bbSummary(pr), e
}
func (c *BitbucketClient) CreatePullRequest(ctx context.Context, owner, repo string, input githubclient.CreateRequest) (githubclient.PullRequestSummary, error) {
	if strings.TrimSpace(input.Title) == "" || input.Head == input.Base || input.Head == "" || input.Base == "" || len(input.Title) > 512 || len(input.Body) > 65536 {
		return githubclient.PullRequestSummary{}, errors.New("Enter a title and different published base and source branches")
	}
	path, e := bbRepoPath(owner, repo)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	var pr bbPR
	_, e = c.api.request(ctx, "POST", path+"/pullrequests", map[string]any{"title": input.Title, "description": input.Body, "draft": input.Draft, "source": map[string]any{"branch": map[string]string{"name": input.Head}}, "destination": map[string]any{"branch": map[string]string{"name": input.Base}}, "close_source_branch": false}, &pr)
	return bbSummary(pr), e
}
func (c *BitbucketClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, input githubclient.UpdateRequest) (githubclient.PullRequestSummary, error) {
	fields := map[string]any{}
	if input.Title != nil {
		if strings.TrimSpace(*input.Title) == "" || len(*input.Title) > 512 {
			return githubclient.PullRequestSummary{}, errors.New("Enter a valid title")
		}
		fields["title"] = *input.Title
	}
	if input.Body != nil {
		if len(*input.Body) > 65536 {
			return githubclient.PullRequestSummary{}, errors.New("Description exceeds 64 KiB")
		}
		fields["description"] = *input.Body
	}
	if input.State != nil {
		if len(fields) > 0 {
			return githubclient.PullRequestSummary{}, errors.New("Change the title or description separately from closing the pull request")
		}
		if *input.State != "closed" {
			return githubclient.PullRequestSummary{}, unsupported("Bitbucket", "reopening a declined pull request")
		}
		path, e := bbPRPath(owner, repo, number)
		if e != nil {
			return githubclient.PullRequestSummary{}, e
		}
		var pr bbPR
		_, e = c.api.request(ctx, "POST", path+"/decline", nil, &pr)
		return bbSummary(pr), e
	}
	return c.updatePR(ctx, owner, repo, number, fields)
}
func (c *BitbucketClient) SetDraft(ctx context.Context, owner, repo string, number int, draft bool) error {
	_, e := c.updatePR(ctx, owner, repo, number, map[string]bool{"draft": draft})
	return e
}
func (c *BitbucketClient) postComment(ctx context.Context, owner, repo string, number int, input any) (githubclient.Comment, error) {
	path, e := bbPRPath(owner, repo, number)
	if e != nil {
		return githubclient.Comment{}, e
	}
	var note bbComment
	_, e = c.api.request(ctx, "POST", path+"/comments", input, &note)
	return bbMappedComment(note), e
}
func (c *BitbucketClient) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) (githubclient.Comment, error) {
	if e := validBody(body); e != nil {
		return githubclient.Comment{}, e
	}
	return c.postComment(ctx, owner, repo, number, map[string]any{"content": map[string]string{"raw": body}})
}
func (c *BitbucketClient) postInline(ctx context.Context, owner, repo string, number int, input githubclient.ReviewCommentRequest, files []githubclient.ChangedFile) (githubclient.Comment, error) {
	if e := validBody(input.Body); e != nil {
		return githubclient.Comment{}, e
	}
	if _, e := c.verifyHead(ctx, owner, repo, number, input.CommitID); e != nil {
		return githubclient.Comment{}, e
	}
	var file *githubclient.ChangedFile
	for i := range files {
		if files[i].Path == input.Path {
			file = &files[i]
			break
		}
	}
	if file == nil {
		return githubclient.Comment{}, errors.New("The selected file is no longer in the pull request")
	}
	if _, _, e := glLineNumbers(file.Patch, input.Side, input.Line); e != nil {
		return githubclient.Comment{}, e
	}
	inline := map[string]any{"path": input.Path}
	if input.Side == "LEFT" {
		inline["from"] = input.Line
	} else {
		inline["to"] = input.Line
	}
	return c.postComment(ctx, owner, repo, number, map[string]any{"content": map[string]string{"raw": input.Body}, "inline": inline})
}
func (c *BitbucketClient) AddReviewComment(ctx context.Context, owner, repo string, number int, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	if !immutable(input.CommitID) {
		return githubclient.Comment{}, errors.New("Choose the displayed head commit")
	}
	path, e := bbPRPath(owner, repo, number)
	if e != nil {
		return githubclient.Comment{}, e
	}
	files, _, e := c.files(ctx, path+"/diff")
	if e != nil {
		return githubclient.Comment{}, e
	}
	return c.postInline(ctx, owner, repo, number, input, files)
}
func (c *BitbucketClient) SubmitReview(ctx context.Context, owner, repo string, number int, input githubclient.ReviewRequest) (githubclient.Review, error) {
	if e := validateReview(input, true); e != nil {
		return githubclient.Review{}, e
	}
	if _, e := c.verifyHead(ctx, owner, repo, number, input.CommitID); e != nil {
		return githubclient.Review{}, e
	}
	profile, e := c.currentProfile(ctx)
	if e != nil {
		return githubclient.Review{}, e
	}
	path, _ := bbPRPath(owner, repo, number)
	files := []githubclient.ChangedFile{}
	if len(input.Comments) > 0 {
		files, _, e = c.files(ctx, path+"/diff")
		if e != nil {
			return githubclient.Review{}, e
		}
	}
	return c.publisher.publish(ctx, hash("bitbucket", c.api.host, owner, repo, strconv.Itoa(number)), input, profile.user(), publicationActions{
		Find: func(ctx context.Context, key string) (map[string]PublishedComment, error) {
			notes, e := bbArray[bbComment](ctx, c, path+"/comments", 5000)
			if e != nil {
				return nil, e
			}
			found := map[string]PublishedComment{}
			for _, note := range notes {
				if note.Deleted || note.User.UUID != profile.UUID {
					continue
				}
				item, posted, ok := publicationMarker(note.Content.Raw, key)
				if ok {
					posted.ID = note.ID
					found[item] = posted
				}
			}
			return found, nil
		},
		Comment: func(ctx context.Context, comment githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
			return c.postInline(ctx, owner, repo, number, comment, files)
		},
		Body: func(ctx context.Context, body string) (githubclient.Comment, error) {
			if _, e := c.verifyHead(ctx, owner, repo, number, input.CommitID); e != nil {
				return githubclient.Comment{}, e
			}
			return c.AddIssueComment(ctx, owner, repo, number, body)
		},
		Vote: func(ctx context.Context, event string) error {
			if _, e := c.verifyHead(ctx, owner, repo, number, input.CommitID); e != nil {
				return e
			}
			suffix := "/approve"
			if event == "REQUEST_CHANGES" {
				suffix = "/request-changes"
			}
			_, e := c.api.request(ctx, "POST", path+suffix, nil, nil)
			return e
		},
	})
}
func (c *BitbucketClient) ReplyReviewComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) (githubclient.Comment, error) {
	if e := validBody(body); e != nil {
		return githubclient.Comment{}, e
	}
	if commentID < 1 {
		return githubclient.Comment{}, errors.New("Choose a comment to reply to")
	}
	return c.postComment(ctx, owner, repo, number, map[string]any{"content": map[string]string{"raw": body}, "parent": map[string]int64{"id": commentID}})
}
func (c *BitbucketClient) ResolveThread(ctx context.Context, id string, resolved bool) error {
	owner, repo, number, native, e := decodeThread(id, "bitbucket")
	if e != nil {
		return e
	}
	n, e := strconv.ParseInt(native, 10, 64)
	if e != nil || n < 1 {
		return errors.New("Choose a valid Bitbucket comment")
	}
	path, e := bbPRPath(owner, repo, number)
	if e != nil {
		return e
	}
	method := "POST"
	if !resolved {
		method = "DELETE"
	}
	_, e = c.api.request(ctx, method, path+"/comments/"+native+"/resolve", nil, nil)
	return e
}
func (c *BitbucketClient) MergePullRequest(context.Context, string, string, int, string, string) (githubclient.MergeResult, error) {
	return githubclient.MergeResult{}, errors.New("Merge in Bitbucket: its REST endpoint cannot atomically enforce the displayed head commit")
}
func (c *BitbucketClient) SetAssignees(context.Context, string, string, int, []string) error {
	return unsupported("Bitbucket", "assignees")
}
func (c *BitbucketClient) SetLabels(context.Context, string, string, int, []string) ([]string, error) {
	return nil, unsupported("Bitbucket", "labels")
}
func (c *BitbucketClient) reviewers(ctx context.Context, owner, repo string, number int, names []string, remove bool) (githubclient.PullRequestSummary, error) {
	if len(names) > 100 {
		return githubclient.PullRequestSummary{}, errors.New("Choose up to 100 reviewers")
	}
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	members := map[string]bool{}
	for _, u := range pr.Reviewers {
		members[u.UUID] = true
	}
	for _, name := range names {
		if len(name) < 3 || len(name) > 512 || strings.ContainsAny(name, "\x00\r\n/\\") {
			return githubclient.PullRequestSummary{}, errors.New("Enter reviewer UUIDs from Bitbucket")
		}
		if remove {
			delete(members, name)
		} else {
			members[name] = true
		}
	}
	users := []map[string]string{}
	for id := range members {
		users = append(users, map[string]string{"uuid": id})
	}
	return c.updatePR(ctx, owner, repo, number, map[string]any{"reviewers": users})
}
func (c *BitbucketClient) RequestReviewers(ctx context.Context, owner, repo string, number int, names []string) (githubclient.PullRequestSummary, error) {
	return c.reviewers(ctx, owner, repo, number, names, false)
}
func (c *BitbucketClient) RemoveReviewers(ctx context.Context, owner, repo string, number int, names []string) error {
	_, e := c.reviewers(ctx, owner, repo, number, names, true)
	return e
}
func (c *BitbucketClient) RetargetPullRequest(ctx context.Context, owner, repo string, number int, input githubclient.RetargetRequest) (githubclient.PullRequestSummary, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	if e = validateRetarget(input, bbSummary(pr)); e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	return c.updatePR(ctx, owner, repo, number, map[string]any{"destination": map[string]any{"branch": map[string]string{"name": input.Base}}})
}
