package reviewproviders

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"net/url"
	"strconv"
	"strings"
)

func (c *AzureClient) updatePR(ctx context.Context, owner, repo string, number int, input any) (githubclient.PullRequestSummary, error) {
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	var pr azPR
	_, e = c.api.request(ctx, "PATCH", azVersion(path), input, &pr)
	return azSummary(pr, owner, repo), e
}
func (c *AzureClient) CreatePullRequest(ctx context.Context, owner, repo string, input githubclient.CreateRequest) (githubclient.PullRequestSummary, error) {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 512 || len(input.Body) > 65536 || input.Head == "" || input.Base == "" || input.Head == input.Base {
		return githubclient.PullRequestSummary{}, errors.New("Enter a title and different published source and base branches")
	}
	path, e := c.repoPath(owner, repo)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	var pr azPR
	_, e = c.api.request(ctx, "POST", azVersion(path+"/pullrequests"), map[string]any{"title": input.Title, "description": input.Body, "sourceRefName": "refs/heads/" + input.Head, "targetRefName": "refs/heads/" + input.Base, "isDraft": input.Draft}, &pr)
	return azSummary(pr, owner, repo), e
}
func (c *AzureClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, input githubclient.UpdateRequest) (githubclient.PullRequestSummary, error) {
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
		state := map[string]string{"closed": "abandoned", "open": "active"}[*input.State]
		if state == "" {
			return githubclient.PullRequestSummary{}, errors.New("Choose open or closed")
		}
		fields["status"] = state
	}
	return c.updatePR(ctx, owner, repo, number, fields)
}
func (c *AzureClient) SetDraft(ctx context.Context, owner, repo string, number int, draft bool) error {
	_, e := c.updatePR(ctx, owner, repo, number, map[string]bool{"isDraft": draft})
	return e
}
func (c *AzureClient) postThread(ctx context.Context, owner, repo string, number int, input any) (githubclient.Comment, error) {
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return githubclient.Comment{}, e
	}
	var thread azThread
	if _, e = c.api.request(ctx, "POST", azVersion(path+"/threads"), input, &thread); e != nil {
		return githubclient.Comment{}, e
	}
	if len(thread.Comments) == 0 {
		return githubclient.Comment{}, errors.New("Azure DevOps did not return the posted comment. Refresh before retrying")
	}
	return azMappedComment(thread, thread.Comments[0], azSummary(azPR{PullRequestID: number}, owner, repo).URL)
}
func (c *AzureClient) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) (githubclient.Comment, error) {
	if e := validBody(body); e != nil {
		return githubclient.Comment{}, e
	}
	return c.postThread(ctx, owner, repo, number, map[string]any{"comments": []map[string]any{{"parentCommentId": 0, "content": body, "commentType": 1}}, "status": 1})
}
func (c *AzureClient) postInline(ctx context.Context, owner, repo string, number int, pr azPR, iteration azIteration, changes []azChange, files []githubclient.ChangedFile, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	if e := validBody(input.Body); e != nil {
		return githubclient.Comment{}, e
	}
	latest, e := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if e != nil {
		return githubclient.Comment{}, e
	}
	if latest.LastMergeTargetCommit.CommitID != pr.LastMergeTargetCommit.CommitID || latest.TargetRefName != pr.TargetRefName {
		return githubclient.Comment{}, errors.New("The review comparison changed. Refresh before commenting")
	}
	var selected *githubclient.ChangedFile
	for i := range files {
		if files[i].Path == input.Path {
			selected = &files[i]
			break
		}
	}
	if selected == nil {
		return githubclient.Comment{}, errors.New("The file is no longer in this review")
	}
	if _, _, e = glLineNumbers(selected.Patch, input.Side, input.Line); e != nil {
		return githubclient.Comment{}, e
	}
	tracking := 0
	for _, change := range changes {
		if strings.TrimPrefix(change.Item.Path, "/") == input.Path {
			tracking = change.ChangeTrackingID
			break
		}
	}
	if tracking < 1 {
		return githubclient.Comment{}, errors.New("Azure DevOps did not return a tracked file for this comment")
	}
	filePath := input.Path
	if input.Side == "LEFT" && selected.PreviousPath != "" {
		filePath = selected.PreviousPath
	}
	point := &azPoint{Line: input.Line, Offset: 1}
	threadContext := azThreadContext{FilePath: "/" + filePath}
	if input.Side == "LEFT" {
		threadContext.LeftFileStart = point
		threadContext.LeftFileEnd = point
	} else {
		threadContext.RightFileStart = point
		threadContext.RightFileEnd = point
	}
	return c.postThread(ctx, owner, repo, number, map[string]any{"comments": []map[string]any{{"parentCommentId": 0, "content": input.Body, "commentType": 1}}, "status": 1, "threadContext": threadContext, "pullRequestThreadContext": map[string]any{"changeTrackingId": tracking, "iterationContext": map[string]int{"firstComparingIteration": 0, "secondComparingIteration": iteration.ID}}})
}
func (c *AzureClient) AddReviewComment(ctx context.Context, owner, repo string, number int, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	pr, e := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if e != nil {
		return githubclient.Comment{}, e
	}
	iteration, changes, e := c.comparison(ctx, owner, repo, pr)
	if e != nil {
		return githubclient.Comment{}, e
	}
	files, _, e := c.files(ctx, owner, repo, pr, iteration, changes)
	if e != nil {
		return githubclient.Comment{}, e
	}
	return c.postInline(ctx, owner, repo, number, pr, iteration, changes, files, input)
}
func (c *AzureClient) SubmitReview(ctx context.Context, owner, repo string, number int, input githubclient.ReviewRequest) (githubclient.Review, error) {
	if e := validateReview(input, true); e != nil {
		return githubclient.Review{}, e
	}
	pr, e := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if e != nil {
		return githubclient.Review{}, e
	}
	profile, e := c.currentProfile(ctx)
	if e != nil {
		return githubclient.Review{}, e
	}
	path, _ := c.prPath(owner, repo, number)
	var iteration azIteration
	var changes []azChange
	var files []githubclient.ChangedFile
	if len(input.Comments) > 0 {
		iteration, changes, e = c.comparison(ctx, owner, repo, pr)
		if e != nil {
			return githubclient.Review{}, e
		}
		files, _, e = c.files(ctx, owner, repo, pr, iteration, changes)
		if e != nil {
			return githubclient.Review{}, e
		}
	}
	return c.publisher.publish(ctx, hash("azure", c.api.host, owner, repo, strconv.Itoa(number)), input, profile.user(), publicationActions{
		Find: func(ctx context.Context, key string) (map[string]PublishedComment, error) {
			threads, e := c.threads(ctx, owner, repo, number)
			if e != nil {
				return nil, e
			}
			found := map[string]PublishedComment{}
			for _, thread := range threads {
				if thread.IsDeleted {
					continue
				}
				for _, note := range thread.Comments {
					if note.IsDeleted || note.Author.ID != profile.ID {
						continue
					}
					item, posted, ok := publicationMarker(note.Content, key)
					if ok {
						posted.ID, e = azCommentID(thread.ID, note.ID)
						if e != nil {
							return nil, e
						}
						found[item] = posted
					}
				}
			}
			return found, nil
		},
		Comment: func(ctx context.Context, comment githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
			return c.postInline(ctx, owner, repo, number, pr, iteration, changes, files, comment)
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
			vote := 10
			if event == "REQUEST_CHANGES" {
				vote = -10
			}
			_, e := c.api.request(ctx, "PUT", azVersion(path+"/reviewers/"+url.PathEscape(profile.ID)), map[string]any{"id": profile.ID, "vote": vote}, nil)
			return e
		},
	})
}
func (c *AzureClient) ReplyReviewComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) (githubclient.Comment, error) {
	if e := validBody(body); e != nil {
		return githubclient.Comment{}, e
	}
	threadID, parentID, e := azUnpackComment(commentID)
	if e != nil {
		return githubclient.Comment{}, e
	}
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return githubclient.Comment{}, e
	}
	var note azComment
	_, e = c.api.request(ctx, "POST", azVersion(path+"/threads/"+strconv.FormatInt(threadID, 10)+"/comments"), map[string]any{"content": body, "parentCommentId": parentID, "commentType": 1}, &note)
	if e != nil {
		return githubclient.Comment{}, e
	}
	return azMappedComment(azThread{ID: threadID}, note, azSummary(azPR{PullRequestID: number}, owner, repo).URL)
}
func (c *AzureClient) ResolveThread(ctx context.Context, id string, resolved bool) error {
	owner, repo, number, native, e := decodeThread(id, "azure")
	if e != nil {
		return e
	}
	thread, e := strconv.ParseInt(native, 10, 32)
	if e != nil || thread < 1 {
		return errors.New("Choose a valid Azure conversation")
	}
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return e
	}
	status := 1
	if resolved {
		status = 2
	}
	_, e = c.api.request(ctx, "PATCH", azVersion(path+"/threads/"+native), map[string]int{"status": status}, nil)
	return e
}
func (c *AzureClient) MergePullRequest(ctx context.Context, owner, repo string, number int, method, head string) (githubclient.MergeResult, error) {
	strategy := map[string]string{"merge": "noFastForward", "squash": "squash", "rebase": "rebase"}[method]
	if strategy == "" {
		return githubclient.MergeResult{}, errors.New("Choose merge, squash, or rebase")
	}
	if _, e := c.verifyHead(ctx, owner, repo, number, head); e != nil {
		return githubclient.MergeResult{}, e
	}
	path, _ := c.prPath(owner, repo, number)
	var pr azPR
	_, e := c.api.request(ctx, "PATCH", azVersion(path), map[string]any{"status": "completed", "lastMergeSourceCommit": map[string]string{"commitId": head}, "completionOptions": map[string]any{"mergeStrategy": strategy, "deleteSourceBranch": false, "bypassPolicy": false}}, &pr)
	if e != nil {
		return githubclient.MergeResult{}, e
	}
	if pr.Status != "completed" {
		return githubclient.MergeResult{}, errors.New("Azure DevOps accepted completion but has not confirmed the merge. Refresh its status before retrying")
	}
	return githubclient.MergeResult{Merged: true, SHA: pr.LastMergeCommit.CommitID, Message: "Pull request completed"}, nil
}
func (c *AzureClient) SetAssignees(context.Context, string, string, int, []string) error {
	return unsupported("Azure DevOps", "assignees")
}
func (c *AzureClient) RequestReviewers(ctx context.Context, owner, repo string, number int, names []string) (githubclient.PullRequestSummary, error) {
	if len(names) > 100 {
		return githubclient.PullRequestSummary{}, errors.New("Choose up to 100 reviewers")
	}
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	path, _ := c.prPath(owner, repo, number)
	existing := map[string]bool{}
	for _, u := range pr.Reviewers {
		existing[u.ID] = true
	}
	for _, name := range names {
		if _, e := component(name); e != nil {
			return githubclient.PullRequestSummary{}, e
		}
		if existing[name] {
			continue
		}
		if _, e = c.api.request(ctx, "PUT", azVersion(path+"/reviewers/"+url.PathEscape(name)), map[string]any{"id": name, "vote": 0}, nil); e != nil {
			return githubclient.PullRequestSummary{}, e
		}
	}
	pr, e = c.getPR(ctx, owner, repo, number)
	return azSummary(pr, owner, repo), e
}
func (c *AzureClient) RemoveReviewers(ctx context.Context, owner, repo string, number int, names []string) error {
	if len(names) > 100 {
		return errors.New("Choose up to 100 reviewers")
	}
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return e
	}
	for _, name := range names {
		if _, e = component(name); e != nil {
			return e
		}
		if _, e = c.api.request(ctx, "DELETE", azVersion(path+"/reviewers/"+url.PathEscape(name)), nil, nil); e != nil {
			return e
		}
	}
	return nil
}
func (c *AzureClient) SetLabels(ctx context.Context, owner, repo string, number int, labels []string) ([]string, error) {
	if len(labels) > 100 {
		return nil, errors.New("Choose up to 100 labels")
	}
	desired := map[string]bool{}
	for _, label := range labels {
		if strings.TrimSpace(label) == "" || len(label) > 255 || strings.ContainsAny(label, "\x00\r\n") {
			return nil, errors.New("Enter valid label names")
		}
		desired[label] = true
	}
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return nil, e
	}
	var data azEnvelope[azLabel]
	if _, e = c.api.request(ctx, "GET", azVersion(path+"/labels"), nil, &data); e != nil {
		return nil, e
	}
	existing := map[string]bool{}
	for _, label := range data.Value {
		existing[label.Name] = true
		if !desired[label.Name] {
			if _, e = component(label.ID); e != nil {
				return nil, e
			}
			if _, e = c.api.request(ctx, "DELETE", azVersion(path+"/labels/"+url.PathEscape(label.ID)), nil, nil); e != nil {
				return nil, e
			}
		}
	}
	for label := range desired {
		if !existing[label] {
			if _, e = c.api.request(ctx, "POST", azVersion(path+"/labels"), map[string]string{"name": label}, nil); e != nil {
				return nil, e
			}
		}
	}
	return labels, nil
}
func (c *AzureClient) RetargetPullRequest(ctx context.Context, owner, repo string, number int, input githubclient.RetargetRequest) (githubclient.PullRequestSummary, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	if e = validateRetarget(input, azSummary(pr, owner, repo)); e != nil {
		return githubclient.PullRequestSummary{}, e
	}
	return c.updatePR(ctx, owner, repo, number, map[string]string{"targetRefName": "refs/heads/" + input.Base})
}
