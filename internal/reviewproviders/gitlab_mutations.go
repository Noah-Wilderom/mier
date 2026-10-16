package reviewproviders

import (
	"context"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/githubclient"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var _ API = (*GitLabClient)(nil)

func (c *GitLabClient) PullRequestSource(ctx context.Context, owner, repo string, number int) (Source, error) {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return Source{}, err
	}
	return Source{HeadRef: "refs/merge-requests/" + strconv.Itoa(number) + "/head", HeadSHA: mr.SHA, BaseRef: "refs/heads/" + mr.TargetBranch}, nil
}

func (c *GitLabClient) updateMR(ctx context.Context, owner, repo string, number int, input any) (githubclient.PullRequestSummary, error) {
	path, err := glMRPath(owner, repo, number)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	var mr glMR
	_, err = c.api.request(ctx, "PUT", path, input, &mr)
	return glSummary(mr, owner, repo), err
}
func (c *GitLabClient) CreatePullRequest(ctx context.Context, owner, repo string, input githubclient.CreateRequest) (githubclient.PullRequestSummary, error) {
	if strings.TrimSpace(input.Title) == "" || input.Base == "" || input.Head == "" || input.Base == input.Head || strings.ContainsAny(input.Head, "\x00\r\n:") || strings.ContainsAny(input.Base, "\x00\r\n") {
		return githubclient.PullRequestSummary{}, errors.New("Choose a title and different published source/target branches in this project")
	}
	path, err := glProjectPath(owner, repo)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	title := glTitle(input.Title)
	if input.Draft {
		title = "Draft: " + title
	}
	var mr glMR
	_, err = c.api.request(ctx, "POST", path+"/merge_requests", map[string]any{"title": title, "description": input.Body, "source_branch": input.Head, "target_branch": input.Base, "remove_source_branch": false}, &mr)
	return glSummary(mr, owner, repo), err
}
func (c *GitLabClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, input githubclient.UpdateRequest) (githubclient.PullRequestSummary, error) {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	payload := map[string]any{}
	if input.Title != nil {
		title := glTitle(*input.Title)
		if strings.TrimSpace(title) == "" {
			return githubclient.PullRequestSummary{}, errors.New("A merge request needs a title")
		}
		if mr.Draft || mr.WIP {
			title = "Draft: " + title
		}
		payload["title"] = title
	}
	if input.Body != nil {
		payload["description"] = *input.Body
	}
	if input.State != nil {
		switch *input.State {
		case "open":
			payload["state_event"] = "reopen"
		case "closed":
			payload["state_event"] = "close"
		default:
			return githubclient.PullRequestSummary{}, errors.New("Choose open or closed")
		}
	}
	return c.updateMR(ctx, owner, repo, number, payload)
}
func (c *GitLabClient) SetDraft(ctx context.Context, owner, repo string, number int, draft bool) error {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	if mr.State != "opened" {
		return errors.New("Only an open merge request can change draft status")
	}
	title := glTitle(mr.Title)
	if draft {
		title = "Draft: " + title
	}
	_, err = c.updateMR(ctx, owner, repo, number, map[string]string{"title": title})
	return err
}
func validBody(body string) error {
	if strings.TrimSpace(body) == "" || len(body) > 100*1024 || strings.ContainsRune(body, 0) {
		return errors.New("Write a comment of at most 100 KiB")
	}
	return nil
}
func (c *GitLabClient) AddIssueComment(ctx context.Context, owner, repo string, number int, body string) (githubclient.Comment, error) {
	if err := validBody(body); err != nil {
		return githubclient.Comment{}, err
	}
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.Comment{}, err
	}
	path, _ := glMRPath(owner, repo, number)
	var note glNote
	_, err = c.api.request(ctx, "POST", path+"/notes", map[string]string{"body": body}, &note)
	return glComment(note, mr.WebURL), err
}

var glHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

func glLineNumbers(patch, side string, line int) (*int, *int, error) {
	old, next := 0, 0
	inHunk := false
	for _, row := range strings.Split(patch, "\n") {
		if match := glHunk.FindStringSubmatch(row); len(match) > 0 {
			old, _ = strconv.Atoi(match[1])
			next, _ = strconv.Atoi(match[2])
			inHunk = true
			continue
		}
		if !inHunk || row == "" || row[0] == '\\' {
			continue
		}
		left, right := old, next
		switch row[0] {
		case ' ':
			if side == "LEFT" && old == line || side == "RIGHT" && next == line {
				return &left, &right, nil
			}
			old++
			next++
		case '-':
			if side == "LEFT" && old == line {
				return &left, nil, nil
			}
			old++
		case '+':
			if side == "RIGHT" && next == line {
				return nil, &right, nil
			}
			next++
		}
	}
	return nil, nil, errors.New("This line is outside GitLab's current diff. Refresh before commenting")
}
func (c *GitLabClient) postInline(ctx context.Context, owner, repo string, number int, mr glMR, diffs []glDiff, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	if err := validBody(input.Body); err != nil {
		return githubclient.Comment{}, err
	}
	if !validFile(input.Path) || input.Line < 1 || (input.Side != "LEFT" && input.Side != "RIGHT") {
		return githubclient.Comment{}, errors.New("Choose a valid diff path, line, and side")
	}
	if !immutable(mr.DiffRefs.BaseSHA) || !immutable(mr.DiffRefs.StartSHA) || !immutable(mr.DiffRefs.HeadSHA) {
		return githubclient.Comment{}, errors.New("GitLab is still preparing this comparison. Refresh in a moment")
	}
	latest, err := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if err != nil {
		return githubclient.Comment{}, err
	}
	if latest.DiffRefs != mr.DiffRefs {
		return githubclient.Comment{}, errors.New("The merge request comparison changed. Refresh before commenting")
	}
	var selected *glDiff
	for _, diff := range diffs {
		if diff.NewPath == input.Path || input.Side == "LEFT" && diff.OldPath == input.Path {
			selected = &diff
			break
		}
	}
	if selected == nil {
		return githubclient.Comment{}, errors.New("This file is no longer part of the merge request")
	}
	old, next, err := glLineNumbers(selected.Diff, input.Side, input.Line)
	if err != nil {
		return githubclient.Comment{}, err
	}
	position := map[string]any{"position_type": "text", "base_sha": mr.DiffRefs.BaseSHA, "start_sha": mr.DiffRefs.StartSHA, "head_sha": mr.DiffRefs.HeadSHA, "old_path": selected.OldPath, "new_path": selected.NewPath}
	if old != nil {
		position["old_line"] = *old
	}
	if next != nil {
		position["new_line"] = *next
	}
	path, _ := glMRPath(owner, repo, number)
	var discussion glDiscussion
	if _, err = c.api.request(ctx, "POST", path+"/discussions", map[string]any{"body": input.Body, "position": position}, &discussion); err != nil {
		return githubclient.Comment{}, err
	}
	if len(discussion.Notes) == 0 {
		return githubclient.Comment{}, errors.New("GitLab accepted the discussion but returned no comment. Refresh before retrying")
	}
	return glComment(discussion.Notes[0], mr.WebURL), nil
}
func (c *GitLabClient) AddReviewComment(ctx context.Context, owner, repo string, number int, input githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
	mr, err := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if err != nil {
		return githubclient.Comment{}, err
	}
	path, _ := glMRPath(owner, repo, number)
	diffs, err := glArray[glDiff](ctx, c, path+"/diffs", 3000)
	if err != nil {
		return githubclient.Comment{}, err
	}
	return c.postInline(ctx, owner, repo, number, mr, diffs, input)
}
func (c *GitLabClient) SubmitReview(ctx context.Context, owner, repo string, number int, input githubclient.ReviewRequest) (githubclient.Review, error) {
	if err := validateReview(input, false); err != nil {
		return githubclient.Review{}, err
	}
	mr, err := c.verifyHead(ctx, owner, repo, number, input.CommitID)
	if err != nil {
		return githubclient.Review{}, err
	}
	profile, err := c.currentProfile(ctx)
	if err != nil {
		return githubclient.Review{}, err
	}
	path, _ := glMRPath(owner, repo, number)
	diffs := []glDiff{}
	if len(input.Comments) > 0 {
		diffs, err = glArray[glDiff](ctx, c, path+"/diffs", 3000)
		if err != nil {
			return githubclient.Review{}, err
		}
	}
	return c.publisher.publish(ctx, hash("gitlab", c.api.host, owner, repo, strconv.Itoa(number)), input, profile.user(), publicationActions{
		Find: func(ctx context.Context, key string) (map[string]PublishedComment, error) {
			discussions, err := glArray[glDiscussion](ctx, c, path+"/discussions", 2000)
			if err != nil {
				return nil, err
			}
			found := map[string]PublishedComment{}
			for _, discussion := range discussions {
				for _, note := range discussion.Notes {
					if note.Author.ID != profile.ID {
						continue
					}
					item, posted, ok := publicationMarker(note.Body, key)
					if ok {
						posted.ID = note.ID
						found[item] = posted
					}
				}
			}
			return found, nil
		},
		Comment: func(ctx context.Context, comment githubclient.ReviewCommentRequest) (githubclient.Comment, error) {
			return c.postInline(ctx, owner, repo, number, mr, diffs, comment)
		},
		Body: func(ctx context.Context, body string) (githubclient.Comment, error) {
			if _, err := c.verifyHead(ctx, owner, repo, number, input.CommitID); err != nil {
				return githubclient.Comment{}, err
			}
			return c.AddIssueComment(ctx, owner, repo, number, body)
		},
		Vote: func(ctx context.Context, event string) error {
			if _, err := c.verifyHead(ctx, owner, repo, number, input.CommitID); err != nil {
				return err
			}
			_, err := c.api.request(ctx, "POST", path+"/approve", map[string]string{"sha": input.CommitID}, nil)
			return err
		},
	})
}
func (c *GitLabClient) ReplyReviewComment(ctx context.Context, owner, repo string, number int, commentID int64, body string) (githubclient.Comment, error) {
	if err := validBody(body); err != nil {
		return githubclient.Comment{}, err
	}
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.Comment{}, err
	}
	path, _ := glMRPath(owner, repo, number)
	discussions, err := glArray[glDiscussion](ctx, c, path+"/discussions", 2000)
	if err != nil {
		return githubclient.Comment{}, err
	}
	for _, discussion := range discussions {
		for _, note := range discussion.Notes {
			if note.ID == commentID {
				var reply glNote
				_, err = c.api.request(ctx, "POST", path+"/discussions/"+url.PathEscape(discussion.ID)+"/notes", map[string]string{"body": body}, &reply)
				return glComment(reply, mr.WebURL), err
			}
		}
	}
	return githubclient.Comment{}, errors.New("The original discussion is no longer available; refresh its replies")
}
func (c *GitLabClient) ResolveThread(ctx context.Context, threadID string, resolved bool) error {
	owner, repo, number, id, err := decodeThread(threadID, "gitlab")
	if err != nil {
		return err
	}
	path, err := glMRPath(owner, repo, number)
	if err != nil {
		return err
	}
	_, err = c.api.request(ctx, "PUT", path+"/discussions/"+url.PathEscape(id), map[string]bool{"resolved": resolved}, nil)
	return err
}
func (c *GitLabClient) MergePullRequest(ctx context.Context, owner, repo string, number int, method, head string) (githubclient.MergeResult, error) {
	if method != "merge" && method != "squash" {
		return githubclient.MergeResult{}, errors.New("Choose merge or squash; GitLab applies the project's merge policy")
	}
	if _, err := c.verifyHead(ctx, owner, repo, number, head); err != nil {
		return githubclient.MergeResult{}, err
	}
	path, _ := glMRPath(owner, repo, number)
	var merged glMR
	_, err := c.api.request(ctx, "PUT", path+"/merge", map[string]any{"sha": head, "squash": method == "squash", "should_remove_source_branch": false}, &merged)
	if err != nil {
		return githubclient.MergeResult{}, err
	}
	if merged.State != "merged" {
		return githubclient.MergeResult{}, errors.New("GitLab has not confirmed the merge. Refresh its status before retrying")
	}
	sha := merged.MergeCommitSHA
	if sha == "" {
		sha = merged.SquashCommitSHA
	}
	if sha == "" {
		sha = merged.SHA
	}
	return githubclient.MergeResult{Merged: true, SHA: sha, Message: "Merge request merged"}, nil
}
func (c *GitLabClient) lookupUsers(ctx context.Context, names []string) ([]int64, error) {
	if len(names) > 50 {
		return nil, errors.New("Choose at most 50 people")
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "@")
		if name == "" {
			return nil, errors.New("Enter a GitLab username")
		}
		var users []glUser
		if _, err := c.api.request(ctx, "GET", "/users?username="+url.QueryEscape(name), nil, &users); err != nil {
			return nil, err
		}
		found := int64(0)
		for _, user := range users {
			if strings.EqualFold(user.Username, name) {
				found = user.ID
				break
			}
		}
		if found == 0 {
			return nil, fmt.Errorf("GitLab user %q was not found", name)
		}
		if !seen[found] {
			seen[found] = true
			ids = append(ids, found)
		}
	}
	return ids, nil
}
func (c *GitLabClient) RequestReviewers(ctx context.Context, owner, repo string, number int, names []string) (githubclient.PullRequestSummary, error) {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	ids, err := c.lookupUsers(ctx, names)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, user := range mr.Reviewers {
		if !seen[user.ID] {
			ids = append(ids, user.ID)
		}
	}
	return c.updateMR(ctx, owner, repo, number, map[string]any{"reviewer_ids": ids})
}
func (c *GitLabClient) RemoveReviewers(ctx context.Context, owner, repo string, number int, names []string) error {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	remove := map[string]bool{}
	for _, name := range names {
		remove[strings.ToLower(strings.TrimPrefix(name, "@"))] = true
	}
	ids := []int64{}
	for _, user := range mr.Reviewers {
		if !remove[strings.ToLower(user.Username)] {
			ids = append(ids, user.ID)
		}
	}
	_, err = c.updateMR(ctx, owner, repo, number, map[string]any{"reviewer_ids": ids})
	return err
}
func (c *GitLabClient) SetAssignees(ctx context.Context, owner, repo string, number int, names []string) error {
	ids, err := c.lookupUsers(ctx, names)
	if err != nil {
		return err
	}
	_, err = c.updateMR(ctx, owner, repo, number, map[string]any{"assignee_ids": ids})
	return err
}
func (c *GitLabClient) SetLabels(ctx context.Context, owner, repo string, number int, labels []string) ([]string, error) {
	if len(labels) > 100 {
		return nil, errors.New("Choose at most 100 labels")
	}
	for _, label := range labels {
		if strings.TrimSpace(label) == "" || strings.ContainsAny(label, ",\x00\r\n") {
			return nil, errors.New("GitLab labels cannot contain commas or line breaks")
		}
	}
	mr, err := c.updateMR(ctx, owner, repo, number, map[string]string{"labels": strings.Join(labels, ",")})
	return mr.Labels, err
}
func (c *GitLabClient) RetargetPullRequest(ctx context.Context, owner, repo string, number int, input githubclient.RetargetRequest) (githubclient.PullRequestSummary, error) {
	mr, err := c.verifyHead(ctx, owner, repo, number, input.ExpectedHead)
	if err != nil {
		return githubclient.PullRequestSummary{}, err
	}
	if mr.State != "opened" || mr.TargetBranch != input.ExpectedBase {
		return githubclient.PullRequestSummary{}, errors.New("This merge request changed. Refresh before changing its target")
	}
	if input.Base == "" || strings.ContainsAny(input.Base, "\x00\r\n") || input.Base == mr.SourceBranch && mr.SourceProjectID == mr.TargetProjectID {
		return githubclient.PullRequestSummary{}, errors.New("Choose a different target branch")
	}
	return c.updateMR(ctx, owner, repo, number, map[string]string{"target_branch": input.Base})
}
func (c *GitLabClient) GetPullRequestStack(ctx context.Context, owner, repo string, number int) (githubclient.PullRequestStack, error) {
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.PullRequestStack{}, err
	}
	items := []githubclient.PullRequestSummary{}
	request := githubclient.PullRequestListRequest{State: "all", PageSize: 100}
	truncated := false
	for page := 0; page < 10; page++ {
		result, err := c.ListPullRequestsPage(ctx, owner, repo, request)
		if err != nil {
			return githubclient.PullRequestStack{}, err
		}
		items = append(items, result.Items...)
		truncated = result.HasNextPage
		if !truncated {
			break
		}
		request.Cursor = result.NextCursor
	}
	result := githubclient.BuildPullRequestStack(glSummary(mr, owner, repo), items, owner+"/"+repo)
	if truncated {
		result.Warnings = append(result.Warnings, "Stack discovery includes the 1,000 most recently updated merge requests")
	}
	return result, nil
}
func (c *GitLabClient) ReviewChanges(ctx context.Context, owner, repo string, number int, head, base string) (githubclient.ReviewChanges, error) {
	ctx, cancel := reviewContext(ctx)
	defer cancel()
	mr, err := c.verifyHead(ctx, owner, repo, number, head)
	if err != nil {
		return githubclient.ReviewChanges{}, err
	}
	if !immutable(base) || mr.DiffRefs.StartSHA != base || !immutable(mr.DiffRefs.BaseSHA) {
		return githubclient.ReviewChanges{}, errors.New("The merge request base changed or is still being prepared. Refresh its review")
	}
	path, _ := glMRPath(owner, repo, number)
	diffs, err := glArray[glDiff](ctx, c, path+"/diffs", 3000)
	if err != nil {
		return githubclient.ReviewChanges{}, err
	}
	latest, err := c.verifyHead(ctx, owner, repo, number, head)
	if err != nil {
		return githubclient.ReviewChanges{}, err
	}
	if latest.DiffRefs != mr.DiffRefs {
		return githubclient.ReviewChanges{}, errors.New("The merge request comparison changed while preparing its review")
	}
	summary := glSummary(mr, owner, repo)
	if summary.Head.Repository == "" && mr.SourceProjectID != 0 {
		project, err := c.getProject(ctx, "/projects/"+strconv.FormatInt(mr.SourceProjectID, 10))
		if err != nil {
			return githubclient.ReviewChanges{}, err
		}
		summary.Head.Repository = project.PathWithNamespace
	}
	files, _ := glFiles(diffs)
	return githubclient.ReviewChanges{PullRequest: summary, Files: files, MergeBaseSHA: mr.DiffRefs.BaseSHA}, nil
}
