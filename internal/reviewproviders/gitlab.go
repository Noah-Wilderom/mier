package reviewproviders

import (
	"context"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/githubclient"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GitLabClient struct {
	api          *transport
	options      Options
	publisher    *Publisher
	mu           sync.Mutex
	profile      glUser
	profileUntil time.Time
}
type glUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

func (u glUser) user() githubclient.User {
	return githubclient.User{Login: u.Username, Name: u.Name, AvatarURL: u.AvatarURL, URL: u.WebURL}
}

type glRefs struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
}
type glMR struct {
	ID                  int64    `json:"id"`
	IID                 int      `json:"iid"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	State               string   `json:"state"`
	Draft               bool     `json:"draft"`
	WIP                 bool     `json:"work_in_progress"`
	WebURL              string   `json:"web_url"`
	Author              glUser   `json:"author"`
	Assignees           []glUser `json:"assignees"`
	Reviewers           []glUser `json:"reviewers"`
	Labels              []string `json:"labels"`
	SourceBranch        string   `json:"source_branch"`
	TargetBranch        string   `json:"target_branch"`
	SourceProjectID     int64    `json:"source_project_id"`
	TargetProjectID     int64    `json:"target_project_id"`
	SHA                 string   `json:"sha"`
	MergeCommitSHA      string   `json:"merge_commit_sha"`
	SquashCommitSHA     string   `json:"squash_commit_sha"`
	DiffRefs            glRefs   `json:"diff_refs"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	MergedAt            *string  `json:"merged_at"`
	ChangesCount        string   `json:"changes_count"`
	UserNotesCount      int      `json:"user_notes_count"`
	HasConflicts        bool     `json:"has_conflicts"`
	MergeStatus         string   `json:"merge_status"`
	DetailedMergeStatus string   `json:"detailed_merge_status"`
	User                struct {
		CanMerge bool `json:"can_merge"`
	} `json:"user"`
}
type glProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	HTTPURL           string `json:"http_url_to_repo"`
	MergeMethod       string `json:"merge_method"`
	SquashOption      string `json:"squash_option"`
	Permissions       struct {
		ProjectAccess *struct {
			Level int `json:"access_level"`
		} `json:"project_access"`
		GroupAccess *struct {
			Level int `json:"access_level"`
		} `json:"group_access"`
	} `json:"permissions"`
}
type glDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	DeletedFile bool   `json:"deleted_file"`
	RenamedFile bool   `json:"renamed_file"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
}
type glCommit struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Message      string `json:"message"`
	AuthorName   string `json:"author_name"`
	AuthoredDate string `json:"authored_date"`
	WebURL       string `json:"web_url"`
}
type glPosition struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
	OldPath  string `json:"old_path"`
	NewPath  string `json:"new_path"`
	OldLine  *int   `json:"old_line"`
	NewLine  *int   `json:"new_line"`
	Type     string `json:"position_type"`
}
type glNote struct {
	ID         int64       `json:"id"`
	Body       string      `json:"body"`
	Author     glUser      `json:"author"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
	System     bool        `json:"system"`
	Resolvable bool        `json:"resolvable"`
	Resolved   bool        `json:"resolved"`
	Position   *glPosition `json:"position"`
}
type glDiscussion struct {
	ID         string   `json:"id"`
	Individual bool     `json:"individual_note"`
	Notes      []glNote `json:"notes"`
}
type glStatus struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	CreatedAt   string `json:"created_at"`
}
type glApprovals struct {
	Approved   bool `json:"approved"`
	Required   *int `json:"approvals_required"`
	Left       *int `json:"approvals_left"`
	ApprovedBy []struct {
		User glUser `json:"user"`
		At   string `json:"approved_at"`
	} `json:"approved_by"`
}

func NewGitLab(host, token string, options Options) (*GitLabClient, error) {
	api, err := newTransport("gitlab", host, token, "", options)
	if err != nil {
		return nil, err
	}
	publisher := options.Publisher
	if publisher == nil {
		publisher = NewPublisher(nil)
	}
	return &GitLabClient{api: api, options: options, publisher: publisher}, nil
}
func (c *GitLabClient) currentProfile(ctx context.Context) (glUser, error) {
	c.mu.Lock()
	if time.Now().Before(c.profileUntil) {
		user := c.profile
		c.mu.Unlock()
		return user, nil
	}
	c.mu.Unlock()
	var user glUser
	if _, err := c.api.request(ctx, "GET", "/user", nil, &user); err != nil {
		return user, err
	}
	if user.ID < 1 || user.Username == "" {
		return user, errors.New("GitLab did not return an authenticated user")
	}
	c.mu.Lock()
	c.profile = user
	c.profileUntil = time.Now().Add(2 * time.Minute)
	c.mu.Unlock()
	return user, nil
}
func (c *GitLabClient) CurrentUser(ctx context.Context) (githubclient.User, error) {
	user, err := c.currentProfile(ctx)
	return user.user(), err
}
func glProjectPath(owner, repo string) (string, error) {
	if _, err := namespace(owner); err != nil {
		return "", err
	}
	if _, err := component(repo); err != nil {
		return "", err
	}
	return "/projects/" + url.PathEscape(owner+"/"+repo), nil
}
func glMRPath(owner, repo string, number int) (string, error) {
	path, err := glProjectPath(owner, repo)
	if err != nil {
		return "", err
	}
	if number < 1 {
		return "", errors.New("Choose a positive merge request number")
	}
	return path + "/merge_requests/" + strconv.Itoa(number), nil
}
func (c *GitLabClient) getMR(ctx context.Context, owner, repo string, number int) (glMR, error) {
	path, err := glMRPath(owner, repo, number)
	var mr glMR
	if err != nil {
		return mr, err
	}
	_, err = c.api.request(ctx, "GET", path, nil, &mr)
	if err == nil && mr.IID != number {
		return mr, errors.New("GitLab returned another merge request")
	}
	return mr, err
}
func (c *GitLabClient) verifyHead(ctx context.Context, owner, repo string, number int, head string) (glMR, error) {
	if !immutable(head) {
		return glMR{}, errors.New("Review requires the displayed head commit")
	}
	mr, err := c.getMR(ctx, owner, repo, number)
	if err == nil && mr.SHA != head && mr.DiffRefs.HeadSHA != head {
		return mr, errors.New("This merge request changed. Refresh before publishing your review")
	}
	return mr, err
}
func glTitle(title string) string {
	for _, prefix := range []string{"Draft:", "WIP:"} {
		if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)) {
			return strings.TrimSpace(title[len(prefix):])
		}
	}
	return title
}
func glSummary(mr glMR, owner, repo string) githubclient.PullRequestSummary {
	state := map[string]string{"opened": "open", "closed": "closed", "merged": "merged"}[mr.State]
	if state == "" {
		state = "open"
	}
	draft := mr.Draft || mr.WIP
	if draft && state == "open" {
		state = "draft"
	}
	head := mr.SHA
	if head == "" {
		head = mr.DiffRefs.HeadSHA
	}
	headRepo := owner + "/" + repo
	if mr.SourceProjectID != 0 && mr.SourceProjectID != mr.TargetProjectID {
		headRepo = ""
	}
	result := githubclient.PullRequestSummary{Provider: "gitlab", Number: mr.IID, NodeID: strconv.FormatInt(mr.ID, 10), Title: glTitle(mr.Title), Body: mr.Description, URL: mr.WebURL, Status: state, Author: mr.Author.user(), CreatedAt: mr.CreatedAt, UpdatedAt: mr.UpdatedAt, Head: githubclient.Branch{Ref: mr.SourceBranch, SHA: head, Repository: headRepo, Label: mr.SourceBranch}, Base: githubclient.Branch{Ref: mr.TargetBranch, SHA: mr.DiffRefs.StartSHA, Repository: owner + "/" + repo, Label: mr.TargetBranch}, Reviewers: []githubclient.User{}, Assignees: []githubclient.User{}, Labels: append([]string{}, mr.Labels...), Draft: draft, Merged: mr.State == "merged" || mr.MergedAt != nil, CommentsCount: mr.UserNotesCount, Warnings: []string{}}
	for _, user := range mr.Reviewers {
		result.Reviewers = append(result.Reviewers, user.user())
	}
	for _, user := range mr.Assignees {
		result.Assignees = append(result.Assignees, user.user())
	}
	if count, err := strconv.Atoi(mr.ChangesCount); err == nil {
		result.ChangedFiles = count
	}
	if mr.HasConflicts {
		value := false
		result.Mergeable = &value
		result.MergeableState = "dirty"
	} else if mr.DetailedMergeStatus == "mergeable" || mr.DetailedMergeStatus == "" && mr.MergeStatus == "can_be_merged" {
		value := true
		result.Mergeable = &value
		result.MergeableState = "clean"
	}
	return result
}
func glFiles(diffs []glDiff) ([]githubclient.ChangedFile, []string) {
	files := []githubclient.ChangedFile{}
	warnings := []string{}
	for _, diff := range diffs {
		path := diff.NewPath
		if path == "" {
			path = diff.OldPath
		}
		if !validFile(path) {
			warnings = append(warnings, "GitLab returned an unsupported source path")
			continue
		}
		status := "modified"
		if diff.NewFile {
			status = "added"
		} else if diff.DeletedFile {
			status = "removed"
		} else if diff.RenamedFile {
			status = "renamed"
		}
		file := githubclient.ChangedFile{Path: path, PreviousPath: diff.OldPath, Status: status, Patch: diff.Diff}
		inHunk := false
		for _, line := range strings.Split(diff.Diff, "\n") {
			if strings.HasPrefix(line, "@@ ") {
				inHunk = true
				continue
			}
			if !inHunk {
				continue
			}
			if strings.HasPrefix(line, "+") {
				file.Additions++
			} else if strings.HasPrefix(line, "-") {
				file.Deletions++
			}
		}
		if diff.TooLarge || diff.Collapsed || diff.Diff == "" {
			warnings = append(warnings, "No complete text patch supplied for "+path+"; open the merge request in GitLab for omitted or binary content")
		}
		files = append(files, file)
	}
	return files, warnings
}
func glComment(note glNote, webURL string) githubclient.Comment {
	result := githubclient.Comment{ID: note.ID, NodeID: strconv.FormatInt(note.ID, 10), Body: cleanBody(note.Body), User: note.Author.user(), CreatedAt: note.CreatedAt, UpdatedAt: note.UpdatedAt, URL: webURL + "#note_" + strconv.FormatInt(note.ID, 10)}
	if position := note.Position; position != nil {
		result.Path = position.NewPath
		result.Line = position.NewLine
		result.Side = "RIGHT"
		if result.Line == nil {
			result.Path = position.OldPath
			result.Line = position.OldLine
			result.Side = "LEFT"
		}
		result.OriginalLine = result.Line
		result.CommitID = position.HeadSHA
	}
	return result
}
func glArray[T any](ctx context.Context, c *GitLabClient, path string, limit int) ([]T, error) {
	items := []T{}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	for page := 1; page <= 100; page++ {
		var batch []T
		headers, err := c.api.request(ctx, "GET", path+separator+"per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(items) > limit {
			return nil, errors.New("This GitLab result exceeds its analysis limit")
		}
		next := integerHeader(headers, "X-Next-Page", 0)
		if next == 0 {
			return items, nil
		}
		if next != page+1 {
			return nil, errors.New("Invalid GitLab pagination response")
		}
	}
	return nil, errors.New("GitLab pagination exceeds 100 pages")
}

func (c *GitLabClient) ListPullRequestsPage(ctx context.Context, owner, repo string, input githubclient.PullRequestListRequest) (githubclient.PullRequestPage, error) {
	result := githubclient.PullRequestPage{Items: []githubclient.PullRequestSummary{}, TotalCount: -1, OpenCount: -1}
	request, err := normalizeList(input)
	if err != nil {
		return result, err
	}
	path, err := glProjectPath(owner, repo)
	if err != nil {
		return result, err
	}
	scope := hash("gitlab", c.api.host, owner, repo, request.State, request.View, request.Query, strconv.Itoa(request.PageSize), c.api.token)
	page, err := readCursor(request.Cursor, scope)
	if err != nil {
		return result, err
	}
	query := url.Values{"state": {map[string]string{"open": "opened", "closed": "closed", "merged": "merged", "all": "all"}[request.State]}, "scope": {"all"}, "order_by": {"updated_at"}, "sort": {"desc"}, "per_page": {strconv.Itoa(request.PageSize)}, "page": {strconv.Itoa(page)}}
	if request.Query != "" {
		query.Set("search", request.Query)
	}
	if request.View != "inbox" {
		user, err := c.currentProfile(ctx)
		if err != nil {
			return result, err
		}
		switch request.View {
		case "mine":
			query.Set("author_id", strconv.FormatInt(user.ID, 10))
		case "assigned":
			query.Set("assignee_id", strconv.FormatInt(user.ID, 10))
		case "review-requested":
			query.Set("reviewer_id", strconv.FormatInt(user.ID, 10))
		case "drafts":
			query.Set("draft", "yes")
		}
	}
	var batch []glMR
	headers, err := c.api.request(ctx, "GET", path+"/merge_requests?"+query.Encode(), nil, &batch)
	if err != nil {
		return result, err
	}
	for _, mr := range batch {
		result.Items = append(result.Items, glSummary(mr, owner, repo))
	}
	result.TotalCount = integerHeader(headers, "X-Total", -1)
	next := integerHeader(headers, "X-Next-Page", 0)
	result.HasNextPage = next > page
	if result.HasNextPage {
		result.NextCursor = makeCursor(next, scope)
	}
	if request.State == "open" && request.View == "inbox" && request.Query == "" {
		result.OpenCount = result.TotalCount
	}
	return result, nil
}
func (c *GitLabClient) ListPullRequests(ctx context.Context, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
	items := []githubclient.PullRequestSummary{}
	request := githubclient.PullRequestListRequest{State: state, PageSize: 100}
	for page := 0; page < 10; page++ {
		result, err := c.ListPullRequestsPage(ctx, owner, repo, request)
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if !result.HasNextPage {
			return items, nil
		}
		request.Cursor = result.NextCursor
	}
	return nil, errors.New("More than 1,000 merge requests match; narrow the query")
}
func (c *GitLabClient) GetCommitDiff(ctx context.Context, owner, repo, sha string) (*githubclient.CommitDiff, error) {
	if !immutable(sha) {
		return nil, errors.New("Choose a full commit ID")
	}
	path, err := glProjectPath(owner, repo)
	if err != nil {
		return nil, err
	}
	diffs, err := glArray[glDiff](ctx, c, path+"/repository/commits/"+sha+"/diff", 3000)
	if err != nil {
		return nil, err
	}
	files, warnings := glFiles(diffs)
	return &githubclient.CommitDiff{SHA: sha, Files: files, Warnings: warnings}, nil
}
func (c *GitLabClient) ReadFileAtCommit(ctx context.Context, owner, repo, file, sha string) (string, error) {
	if !immutable(sha) || !validFile(file) {
		return "", errors.New("Choose a repository file at a full commit ID")
	}
	path, err := glProjectPath(owner, repo)
	if err != nil {
		return "", err
	}
	data, _, err := c.api.raw(ctx, "GET", path+"/repository/files/"+url.PathEscape(file)+"/raw?ref="+sha, "application/octet-stream", nil, 2*1024*1024)
	if err != nil {
		return "", err
	}
	return textFile(data)
}
func (c *GitLabClient) getProject(ctx context.Context, path string) (glProject, error) {
	var project glProject
	_, err := c.api.request(ctx, "GET", path, nil, &project)
	return project, err
}
func (c *GitLabClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
	ctx, cancel := reviewContext(ctx)
	defer cancel()
	mr, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	path, _ := glMRPath(owner, repo, number)
	projectPath, _ := glProjectPath(owner, repo)
	profile, err := c.currentProfile(ctx)
	if err != nil {
		return nil, err
	}
	project, err := c.getProject(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	summary := glSummary(mr, owner, repo)
	if mr.SourceProjectID != 0 && mr.SourceProjectID != mr.TargetProjectID {
		source, err := c.getProject(ctx, "/projects/"+strconv.FormatInt(mr.SourceProjectID, 10))
		if err != nil {
			return nil, fmt.Errorf("Load source repository: %w", err)
		}
		summary.Head.Repository = source.PathWithNamespace
	}
	if summary.Base.SHA == "" {
		var branch struct {
			Commit glCommit `json:"commit"`
		}
		if _, err = c.api.request(ctx, "GET", projectPath+"/repository/branches/"+url.PathEscape(mr.TargetBranch), nil, &branch); err == nil {
			summary.Base.SHA = branch.Commit.ID
		}
	}
	write := !c.options.ReadOnly
	caps := &githubclient.ReviewCapabilities{Comments: write, InlineComments: write, Approve: write, ResolveThreads: write, Draft: write, Merge: write, Reviewers: write, Assignees: write, Labels: write, Retarget: write, Reopen: write}
	detail := &githubclient.PullRequestDetail{PullRequest: summary, Capabilities: caps, Files: []githubclient.ChangedFile{}, Commits: []githubclient.Commit{}, Comments: []githubclient.Comment{}, Reviews: []githubclient.Review{}, Checks: []githubclient.CheckRun{}, Statuses: []githubclient.CommitStatus{}, Threads: []githubclient.Thread{}, Warnings: []string{}, AllowedMergeMethods: []string{"merge", "squash"}, RequiredStatusCheckContexts: []string{}, MergeStateStatus: "UNKNOWN"}
	level := 0
	if project.Permissions.ProjectAccess != nil {
		level = project.Permissions.ProjectAccess.Level
	}
	if project.Permissions.GroupAccess != nil && project.Permissions.GroupAccess.Level > level {
		level = project.Permissions.GroupAccess.Level
	}
	detail.ViewerCanUpdate = write && (profile.ID == mr.Author.ID || mr.User.CanMerge || level >= 30)
	if project.SquashOption == "never" {
		detail.AllowedMergeMethods = []string{"merge"}
	} else if project.SquashOption == "always" {
		detail.AllowedMergeMethods = []string{"squash"}
	}
	var firstError error
	var mu sync.Mutex
	var group sync.WaitGroup
	run := func(optional bool, fn func() error) {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := fn(); err != nil {
				mu.Lock()
				defer mu.Unlock()
				var apiError *HTTPError
				if optional && errors.As(err, &apiError) && (apiError.StatusCode == 403 || apiError.StatusCode == 404) {
					detail.Warnings = append(detail.Warnings, err.Error())
				} else if firstError == nil {
					firstError = err
				}
			}
		}()
	}
	run(false, func() error {
		diffs, err := glArray[glDiff](ctx, c, path+"/diffs", 3000)
		if err != nil {
			return err
		}
		files, warnings := glFiles(diffs)
		detail.Files = files
		mu.Lock()
		detail.Warnings = append(detail.Warnings, warnings...)
		mu.Unlock()
		return nil
	})
	run(false, func() error {
		commits, err := glArray[glCommit](ctx, c, path+"/commits", 3000)
		if err != nil {
			return err
		}
		for _, commit := range commits {
			detail.Commits = append(detail.Commits, githubclient.Commit{SHA: commit.ID, Subject: commit.Title, Body: commit.Message, AuthorName: commit.AuthorName, CreatedAt: commit.AuthoredDate, URL: commit.WebURL})
		}
		return nil
	})
	run(false, func() error {
		discussions, err := glArray[glDiscussion](ctx, c, path+"/discussions", 2000)
		if err != nil {
			return err
		}
		for _, discussion := range discussions {
			if len(discussion.Notes) == 0 {
				continue
			}
			first := discussion.Notes[0]
			if first.Position == nil {
				for _, note := range discussion.Notes {
					detail.Comments = append(detail.Comments, glComment(note, mr.WebURL))
				}
				continue
			}
			thread := githubclient.Thread{ID: encodedThread("gitlab", owner, repo, number, discussion.ID), Path: first.Position.NewPath, Line: first.Position.NewLine, OriginalLine: first.Position.NewLine, DiffSide: "RIGHT", Resolved: first.Resolved, Outdated: first.Position.HeadSHA != "" && first.Position.HeadSHA != summary.Head.SHA, ViewerCanResolve: write && first.Resolvable, ViewerCanUnresolve: write && first.Resolvable, Comments: []githubclient.Comment{}}
			if thread.Line == nil {
				thread.Path = first.Position.OldPath
				thread.Line = first.Position.OldLine
				thread.OriginalLine = first.Position.OldLine
				thread.DiffSide = "LEFT"
			}
			for _, note := range discussion.Notes {
				thread.Comments = append(thread.Comments, glComment(note, mr.WebURL))
			}
			detail.Threads = append(detail.Threads, thread)
		}
		return nil
	})
	run(true, func() error {
		statuses, err := glArray[glStatus](ctx, c, projectPath+"/repository/commits/"+summary.Head.SHA+"/statuses", 2000)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			state := "pending"
			switch status.Status {
			case "success", "skipped":
				state = "success"
			case "failed", "canceled":
				state = "failure"
			}
			detail.Statuses = append(detail.Statuses, githubclient.CommitStatus{ID: status.ID, Context: status.Name, State: state, Description: status.Description, TargetURL: status.TargetURL, CreatedAt: status.CreatedAt})
		}
		return nil
	})
	run(true, func() error {
		var approvals glApprovals
		if _, err := c.api.request(ctx, "GET", path+"/approvals", nil, &approvals); err != nil {
			return err
		}
		detail.RequiredApprovals = approvals.Required
		for _, approved := range approvals.ApprovedBy {
			at := approved.At
			if at == "" {
				at = mr.UpdatedAt
			}
			detail.Reviews = append(detail.Reviews, githubclient.Review{ID: approved.User.ID, User: approved.User.user(), State: "APPROVED", SubmittedAt: at, CommitID: summary.Head.SHA})
		}
		if approvals.Approved {
			detail.ReviewDecision = "APPROVED"
		} else {
			detail.ReviewDecision = "REVIEW_REQUIRED"
		}
		return nil
	})
	group.Wait()
	if firstError != nil {
		return nil, firstError
	}
	latest, err := c.getMR(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	if latest.SHA != mr.SHA || latest.TargetBranch != mr.TargetBranch || latest.DiffRefs.StartSHA != mr.DiffRefs.StartSHA {
		return nil, errors.New("The merge request changed while loading; refresh its review")
	}
	detail.PullRequest.CommitsCount = len(detail.Commits)
	detail.PullRequest.ChangedFiles = len(detail.Files)
	detail.PullRequest.ThreadsCount = len(detail.Threads)
	detail.PullRequest.CountsKnown = len(detail.Warnings) == 0
	for _, file := range detail.Files {
		detail.PullRequest.Additions += file.Additions
		detail.PullRequest.Deletions += file.Deletions
	}
	if mr.HasConflicts {
		detail.MergeStateStatus = "DIRTY"
	} else if mr.DetailedMergeStatus == "mergeable" || mr.DetailedMergeStatus == "" && mr.MergeStatus == "can_be_merged" {
		detail.MergeStateStatus = "CLEAN"
	} else if mr.DetailedMergeStatus != "checking" && mr.DetailedMergeStatus != "preparing" && mr.DetailedMergeStatus != "unchecked" && mr.DetailedMergeStatus != "" {
		detail.MergeStateStatus = "BLOCKED"
		detail.Warnings = append(detail.Warnings, "GitLab merge state: "+strings.ReplaceAll(mr.DetailedMergeStatus, "_", " "))
	}
	return detail, nil
}
