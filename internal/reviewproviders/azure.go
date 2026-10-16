package reviewproviders

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AzureClient struct {
	api          *transport
	options      Options
	publisher    *Publisher
	mu           sync.Mutex
	profile      azUser
	profileUntil time.Time
}
type azUser struct {
	ID                  string `json:"id"`
	DisplayName         string `json:"displayName"`
	ProviderDisplayName string `json:"providerDisplayName"`
	UniqueName          string `json:"uniqueName"`
	ImageURL            string `json:"imageUrl"`
	URL                 string `json:"url"`
}

func (u azUser) user() githubclient.User {
	name := u.DisplayName
	if name == "" {
		name = u.ProviderDisplayName
	}
	return githubclient.User{Login: u.ID, Name: name, AvatarURL: u.ImageURL, URL: u.URL}
}

type azRepo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RemoteURL string `json:"remoteUrl"`
	WebURL    string `json:"webUrl"`
	Project   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
}
type azCommit struct {
	CommitID string   `json:"commitId"`
	Comment  string   `json:"comment"`
	Parents  []string `json:"parents"`
	Author   struct {
		Name string `json:"name"`
		Date string `json:"date"`
	} `json:"author"`
}
type azReviewer struct {
	azUser
	Vote       int  `json:"vote"`
	IsRequired bool `json:"isRequired"`
}
type azLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type azPR struct {
	PullRequestID         int          `json:"pullRequestId"`
	Repository            azRepo       `json:"repository"`
	Title                 string       `json:"title"`
	Description           string       `json:"description"`
	Status                string       `json:"status"`
	IsDraft               bool         `json:"isDraft"`
	CreatedBy             azUser       `json:"createdBy"`
	CreationDate          string       `json:"creationDate"`
	ClosedDate            string       `json:"closedDate"`
	SourceRefName         string       `json:"sourceRefName"`
	TargetRefName         string       `json:"targetRefName"`
	LastMergeSourceCommit azCommit     `json:"lastMergeSourceCommit"`
	LastMergeTargetCommit azCommit     `json:"lastMergeTargetCommit"`
	LastMergeCommit       azCommit     `json:"lastMergeCommit"`
	MergeStatus           string       `json:"mergeStatus"`
	SupportsIterations    bool         `json:"supportsIterations"`
	Reviewers             []azReviewer `json:"reviewers"`
	Labels                []azLabel    `json:"labels"`
	ForkSource            *struct {
		Repository azRepo `json:"repository"`
		Name       string `json:"name"`
	} `json:"forkSource"`
}
type azIteration struct {
	ID              int      `json:"id"`
	SourceRefCommit azCommit `json:"sourceRefCommit"`
	TargetRefCommit azCommit `json:"targetRefCommit"`
	CommonRefCommit azCommit `json:"commonRefCommit"`
}
type azChange struct {
	ChangeTrackingID int    `json:"changeTrackingId"`
	ChangeType       string `json:"changeType"`
	OriginalPath     string `json:"originalPath"`
	Item             struct {
		Path          string `json:"path"`
		GitObjectType string `json:"gitObjectType"`
		IsFolder      bool   `json:"isFolder"`
	} `json:"item"`
}
type azPoint struct {
	Line   int `json:"line"`
	Offset int `json:"offset"`
}
type azThreadContext struct {
	FilePath       string   `json:"filePath"`
	LeftFileStart  *azPoint `json:"leftFileStart,omitempty"`
	LeftFileEnd    *azPoint `json:"leftFileEnd,omitempty"`
	RightFileStart *azPoint `json:"rightFileStart,omitempty"`
	RightFileEnd   *azPoint `json:"rightFileEnd,omitempty"`
}
type azComment struct {
	ID              int64  `json:"id"`
	ParentCommentID int64  `json:"parentCommentId"`
	Author          azUser `json:"author"`
	Content         string `json:"content"`
	PublishedDate   string `json:"publishedDate"`
	LastUpdatedDate string `json:"lastUpdatedDate"`
	IsDeleted       bool   `json:"isDeleted"`
	CommentType     string `json:"commentType"`
}
type azThread struct {
	ID                       int64            `json:"id"`
	Status                   string           `json:"status"`
	IsDeleted                bool             `json:"isDeleted"`
	Comments                 []azComment      `json:"comments"`
	ThreadContext            *azThreadContext `json:"threadContext"`
	PullRequestThreadContext *struct {
		ChangeTrackingID int `json:"changeTrackingId"`
		IterationContext struct {
			FirstComparingIteration  int `json:"firstComparingIteration"`
			SecondComparingIteration int `json:"secondComparingIteration"`
		} `json:"iterationContext"`
	} `json:"pullRequestThreadContext"`
}
type azEnvelope[T any] struct {
	Value []T `json:"value"`
	Count int `json:"count"`
}

func NewAzure(host, token string, options Options) (*AzureClient, error) {
	if _, e := component(options.Organization); e != nil {
		return nil, errors.New("Enter your Azure DevOps organization name")
	}
	api, e := newTransport("azure", host, token, "", options)
	if e != nil {
		return nil, e
	}
	publisher := options.Publisher
	if publisher == nil {
		publisher = NewPublisher(nil)
	}
	return &AzureClient{api: api, options: options, publisher: publisher}, nil
}
func (c *AzureClient) currentProfile(ctx context.Context) (azUser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.profileUntil) {
		return c.profile, nil
	}
	var response struct {
		AuthenticatedUser azUser `json:"authenticatedUser"`
	}
	_, e := c.api.request(ctx, "GET", "/"+url.PathEscape(c.options.Organization)+"/_apis/connectionData?api-version=7.1-preview.1", nil, &response)
	if e == nil && response.AuthenticatedUser.ID == "" {
		e = errors.New("Azure DevOps did not identify the signed-in account")
	}
	if e == nil {
		c.profile = response.AuthenticatedUser
		c.profileUntil = time.Now().Add(2 * time.Minute)
	}
	return response.AuthenticatedUser, e
}
func (c *AzureClient) CurrentUser(ctx context.Context) (githubclient.User, error) {
	u, e := c.currentProfile(ctx)
	return u.user(), e
}
func (c *AzureClient) repoPath(owner, repo string) (string, error) {
	parts := strings.Split(owner, "/")
	if len(parts) != 2 || parts[0] != c.options.Organization {
		return "", errors.New("Connect the Azure DevOps organization matching this repository")
	}
	encoded := make([]string, 2)
	for i, part := range parts {
		value, e := component(part)
		if e != nil {
			return "", e
		}
		encoded[i] = value
	}
	r, e := component(repo)
	return "/" + strings.Join(encoded, "/") + "/_apis/git/repositories/" + r, e
}
func (c *AzureClient) prPath(owner, repo string, number int) (string, error) {
	path, e := c.repoPath(owner, repo)
	if e != nil {
		return "", e
	}
	if number < 1 {
		return "", errors.New("Choose a pull request")
	}
	return path + "/pullRequests/" + strconv.Itoa(number), nil
}
func azVersion(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "api-version=7.1"
}
func (c *AzureClient) getPR(ctx context.Context, owner, repo string, number int) (azPR, error) {
	var pr azPR
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return pr, e
	}
	_, e = c.api.request(ctx, "GET", azVersion(path), nil, &pr)
	if e == nil && pr.PullRequestID != number {
		e = errors.New("Azure DevOps returned a different pull request")
	}
	return pr, e
}
func (c *AzureClient) verifyHead(ctx context.Context, owner, repo string, number int, head string) (azPR, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e == nil && (!immutable(head) || pr.LastMergeSourceCommit.CommitID != head) {
		e = errors.New("The source head changed or its merge preview is still being prepared. Refresh the pull request")
	}
	return pr, e
}
func azFullName(repository azRepo, owner string) string {
	org := strings.Split(owner, "/")[0]
	project := repository.Project.Name
	if project == "" {
		project = strings.TrimPrefix(owner, org+"/")
	}
	return org + "/" + project + "/" + repository.Name
}
func azSummary(pr azPR, owner, repo string) githubclient.PullRequestSummary {
	status := map[string]string{"active": "open", "completed": "merged", "abandoned": "closed"}[pr.Status]
	if status == "" {
		status = "closed"
	}
	if pr.IsDraft && status == "open" {
		status = "draft"
	}
	reviewers := []githubclient.User{}
	for _, u := range pr.Reviewers {
		reviewers = append(reviewers, u.user())
	}
	labels := []string{}
	for _, l := range pr.Labels {
		labels = append(labels, l.Name)
	}
	headRepo := owner + "/" + repo
	if pr.ForkSource != nil {
		headRepo = azFullName(pr.ForkSource.Repository, owner)
	}
	updated := pr.CreationDate
	if pr.ClosedDate != "" {
		updated = pr.ClosedDate
	}
	parts := strings.Split(owner, "/")
	web := "https://dev.azure.com/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[len(parts)-1]) + "/_git/" + url.PathEscape(repo) + "/pullrequest/" + strconv.Itoa(pr.PullRequestID)
	return githubclient.PullRequestSummary{Provider: "azure", Number: pr.PullRequestID, NodeID: strconv.Itoa(pr.PullRequestID), Title: pr.Title, Body: pr.Description, URL: web, Status: status, Author: pr.CreatedBy.user(), CreatedAt: pr.CreationDate, UpdatedAt: updated, Head: githubclient.Branch{Ref: strings.TrimPrefix(pr.SourceRefName, "refs/heads/"), SHA: pr.LastMergeSourceCommit.CommitID, Repository: headRepo}, Base: githubclient.Branch{Ref: strings.TrimPrefix(pr.TargetRefName, "refs/heads/"), SHA: pr.LastMergeTargetCommit.CommitID, Repository: owner + "/" + repo}, Reviewers: reviewers, Assignees: []githubclient.User{}, Labels: labels, Draft: pr.IsDraft, Merged: pr.Status == "completed", Warnings: []string{}}
}
func (c *AzureClient) ListPullRequestsPage(ctx context.Context, owner, repo string, input githubclient.PullRequestListRequest) (githubclient.PullRequestPage, error) {
	result := githubclient.PullRequestPage{Items: []githubclient.PullRequestSummary{}, TotalCount: -1, OpenCount: -1}
	r, e := normalizeList(input)
	if e != nil {
		return result, e
	}
	path, e := c.repoPath(owner, repo)
	if e != nil {
		return result, e
	}
	scope := hash("azure", c.options.Organization, owner, repo, r.State, r.View, r.Query, strconv.Itoa(r.PageSize))
	page, e := readCursor(r.Cursor, scope)
	if e != nil {
		return result, e
	}
	q := url.Values{"$top": {strconv.Itoa(r.PageSize)}, "$skip": {strconv.Itoa((page - 1) * r.PageSize)}, "searchCriteria.status": {map[string]string{"open": "active", "closed": "abandoned", "merged": "completed", "all": "all"}[r.State]}}
	userID := ""
	if r.View == "mine" || r.View == "assigned" || r.View == "review-requested" {
		u, e := c.currentProfile(ctx)
		if e != nil {
			return result, e
		}
		userID = u.ID
		if r.View == "mine" {
			q.Set("searchCriteria.creatorId", u.ID)
		} else {
			q.Set("searchCriteria.reviewerId", u.ID)
		}
	}
	matches := func(pr azPR) bool {
		if r.View == "drafts" && !pr.IsDraft {
			return false
		}
		if userID != "" {
			found := pr.CreatedBy.ID == userID && r.View == "mine"
			if r.View != "mine" {
				for _, u := range pr.Reviewers {
					if u.ID == userID {
						found = true
						break
					}
				}
			}
			if !found {
				return false
			}
		}
		if r.Query != "" {
			n, err := strconv.Atoi(strings.TrimPrefix(r.Query, "#"))
			if err == nil && n > 0 {
				return pr.PullRequestID == n
			}
			text := strings.ToLower(pr.Title + "\n" + pr.Description)
			if !strings.Contains(text, strings.ToLower(r.Query)) {
				return false
			}
		}
		return r.State == "all" || azSummary(pr, owner, repo).Status == r.State || r.State == "open" && pr.Status == "active"
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(r.Query, "#")); err == nil && n > 0 {
		if page != 1 {
			return result, nil
		}
		pr, err := c.getPR(ctx, owner, repo, n)
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		if matches(pr) {
			result.Items = append(result.Items, azSummary(pr, owner, repo))
		}
		result.TotalCount = len(result.Items)
		return result, nil
	}
	var data azEnvelope[azPR]
	_, e = c.api.request(ctx, "GET", azVersion(path+"/pullrequests?"+q.Encode()), nil, &data)
	if e != nil {
		return result, e
	}
	for _, pr := range data.Value {
		if matches(pr) {
			result.Items = append(result.Items, azSummary(pr, owner, repo))
		}
	}
	if len(data.Value) == r.PageSize {
		result.HasNextPage = true
		result.NextCursor = makeCursor(page+1, scope)
	}
	return result, nil
}
func (c *AzureClient) ListPullRequests(ctx context.Context, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
	page, e := c.ListPullRequestsPage(ctx, owner, repo, githubclient.PullRequestListRequest{State: state, PageSize: 100})
	return page.Items, e
}
func (c *AzureClient) iterations(ctx context.Context, owner, repo string, number int) ([]azIteration, error) {
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return nil, e
	}
	var data azEnvelope[azIteration]
	_, e = c.api.request(ctx, "GET", azVersion(path+"/iterations"), nil, &data)
	if len(data.Value) > 3000 {
		return nil, errors.New("This pull request exceeds 3,000 iterations")
	}
	return data.Value, e
}
func (c *AzureClient) comparison(ctx context.Context, owner, repo string, pr azPR) (azIteration, []azChange, error) {
	iterations, e := c.iterations(ctx, owner, repo, pr.PullRequestID)
	if e != nil {
		return azIteration{}, nil, e
	}
	var latest azIteration
	for _, i := range iterations {
		if i.ID > latest.ID {
			latest = i
		}
	}
	if latest.ID < 1 || latest.SourceRefCommit.CommitID != pr.LastMergeSourceCommit.CommitID || !immutable(latest.CommonRefCommit.CommitID) {
		return latest, nil, errors.New("Azure DevOps is still preparing the current source comparison. Refresh the pull request")
	}
	path, _ := c.prPath(owner, repo, pr.PullRequestID)
	items := []azChange{}
	skip := 0
	for {
		var data struct {
			ChangeEntries []azChange `json:"changeEntries"`
			NextSkip      int        `json:"nextSkip"`
		}
		q := url.Values{"$compareTo": {"0"}, "$top": {"1000"}, "$skip": {strconv.Itoa(skip)}}
		_, e = c.api.request(ctx, "GET", azVersion(path+"/iterations/"+strconv.Itoa(latest.ID)+"/changes?"+q.Encode()), nil, &data)
		if e != nil {
			return latest, nil, e
		}
		items = append(items, data.ChangeEntries...)
		if len(items) > 3000 {
			return latest, nil, errors.New("This comparison exceeds 3,000 files")
		}
		if data.NextSkip == 0 {
			break
		}
		if data.NextSkip <= skip {
			return latest, nil, errors.New("Azure DevOps repeated a comparison page")
		}
		skip = data.NextSkip
	}
	return latest, items, nil
}
func azFile(change azChange) (githubclient.ChangedFile, error) {
	path := strings.TrimPrefix(change.Item.Path, "/")
	if !validFile(path) {
		return githubclient.ChangedFile{}, errors.New("Azure DevOps returned an invalid file path")
	}
	status := "modified"
	if strings.Contains(strings.ToLower(change.ChangeType), "add") {
		status = "added"
	} else if strings.Contains(strings.ToLower(change.ChangeType), "delete") {
		status = "removed"
	} else if strings.Contains(strings.ToLower(change.ChangeType), "rename") {
		status = "renamed"
	}
	return githubclient.ChangedFile{Path: path, PreviousPath: strings.TrimPrefix(change.OriginalPath, "/"), Status: status}, nil
}
func (c *AzureClient) files(ctx context.Context, owner, repo string, pr azPR, iteration azIteration, changes []azChange) ([]githubclient.ChangedFile, []string, error) {
	files := []githubclient.ChangedFile{}
	for _, change := range changes {
		if change.Item.IsFolder || change.Item.GitObjectType == "tree" {
			continue
		}
		file, e := azFile(change)
		if e != nil {
			return nil, nil, e
		}
		files = append(files, file)
	}
	warnings := []string{}
	if len(files) > 200 {
		warnings = append(warnings, "Text patches are limited to the first 200 changed files")
	}
	if c.options.Diff == nil {
		return files, []string{"A Git executable is required to build Azure DevOps source diffs"}, nil
	}
	sourceOwner, sourceRepo := owner, repo
	if pr.ForkSource != nil {
		sourceRepo = pr.ForkSource.Repository.Name
		org := strings.Split(owner, "/")[0]
		sourceOwner = org + "/" + pr.ForkSource.Repository.Project.Name
	}
	var mu sync.Mutex
	var group sync.WaitGroup
	queue := make(chan int)
	budget := 0
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range queue {
				file := &files[index]
				before, after := "", ""
				var e error
				if index >= 200 {
					continue
				}
				if file.Status != "added" {
					old := file.PreviousPath
					if old == "" {
						old = file.Path
					}
					before, e = c.ReadFileAtCommit(ctx, owner, repo, old, iteration.CommonRefCommit.CommitID)
				}
				if e == nil && file.Status != "removed" {
					after, e = c.ReadFileAtCommit(ctx, sourceOwner, sourceRepo, file.Path, iteration.SourceRefCommit.CommitID)
				}
				mu.Lock()
				budget += len(before) + len(after)
				exceeded := budget > 32*1024*1024
				mu.Unlock()
				if e == nil && exceeded {
					e = errors.New("Source patches exceed the 32 MiB review budget")
				}
				if e == nil {
					file.Patch, e = c.options.Diff(ctx, before, after)
					file.Additions, file.Deletions = patchCounts(file.Patch)
				}
				if e != nil {
					mu.Lock()
					warnings = append(warnings, "Text patch unavailable for "+file.Path+": "+e.Error())
					mu.Unlock()
				}
			}
		}()
	}
	for i := range files {
		select {
		case queue <- i:
		case <-ctx.Done():
			close(queue)
			group.Wait()
			return nil, nil, ctx.Err()
		}
	}
	close(queue)
	group.Wait()
	return files, warnings, nil
}
func (c *AzureClient) ReadFileAtCommit(ctx context.Context, owner, repo, file, sha string) (string, error) {
	if !validFile(file) || !immutable(sha) {
		return "", errors.New("Choose an immutable source commit and relative path")
	}
	path, e := c.repoPath(owner, repo)
	if e != nil {
		return "", e
	}
	q := url.Values{"path": {"/" + file}, "versionDescriptor.versionType": {"commit"}, "versionDescriptor.version": {sha}, "$format": {"octetStream"}, "resolveLfs": {"false"}}
	data, _, e := c.api.raw(ctx, "GET", azVersion(path+"/items?"+q.Encode()), "application/octet-stream", nil, 2*1024*1024)
	if e != nil {
		return "", e
	}
	return textFile(data)
}

// Thread-local comment IDs are packed below Number.MAX_SAFE_INTEGER. Azure's
// thread and comment IDs are int32; this representation supports 2^21 comments
// per thread without collisions or frontend precision loss.
func azCommentID(thread, comment int64) (int64, error) {
	if thread < 1 || thread > 2147483647 || comment < 1 || comment >= 1<<21 {
		return 0, errors.New("Azure comment identifier exceeds the supported range")
	}
	return thread<<21 | comment, nil
}
func azUnpackComment(id int64) (int64, int64, error) {
	thread, comment := id>>21, id&((1<<21)-1)
	packed, e := azCommentID(thread, comment)
	if e != nil || packed != id {
		return 0, 0, errors.New("Choose a valid Azure comment")
	}
	return thread, comment, nil
}
func azMappedComment(thread azThread, note azComment, web string) (githubclient.Comment, error) {
	id, e := azCommentID(thread.ID, note.ID)
	if e != nil {
		return githubclient.Comment{}, e
	}
	comment := githubclient.Comment{ID: id, User: note.Author.user(), Body: cleanBody(note.Content), CreatedAt: note.PublishedDate, UpdatedAt: note.LastUpdatedDate, URL: web + "?_a=files&discussionId=" + strconv.FormatInt(thread.ID, 10)}
	if note.ParentCommentID > 0 {
		comment.InReplyToID, e = azCommentID(thread.ID, note.ParentCommentID)
		if e != nil {
			return comment, e
		}
	}
	if thread.ThreadContext != nil {
		comment.Path = strings.TrimPrefix(thread.ThreadContext.FilePath, "/")
		point := thread.ThreadContext.RightFileStart
		comment.Side = "RIGHT"
		if point == nil {
			point = thread.ThreadContext.LeftFileStart
			comment.Side = "LEFT"
		}
		if point != nil {
			line := point.Line
			comment.Line = &line
			comment.OriginalLine = &line
		}
	}
	return comment, nil
}
func (c *AzureClient) threads(ctx context.Context, owner, repo string, number int) ([]azThread, error) {
	path, e := c.prPath(owner, repo, number)
	if e != nil {
		return nil, e
	}
	var data azEnvelope[azThread]
	_, e = c.api.request(ctx, "GET", azVersion(path+"/threads"), nil, &data)
	if len(data.Value) > 5000 {
		return nil, errors.New("This review exceeds 5,000 conversations")
	}
	return data.Value, e
}

func (c *AzureClient) commits(ctx context.Context, path string) ([]azCommit, error) {
	items := []azCommit{}
	token := ""
	seen := map[string]bool{}
	for {
		q := url.Values{"$top": {"100"}}
		if token != "" {
			q.Set("continuationToken", token)
		}
		var data azEnvelope[azCommit]
		headers, err := c.api.request(ctx, "GET", azVersion(path+"/commits?"+q.Encode()), nil, &data)
		if err != nil {
			return nil, err
		}
		items = append(items, data.Value...)
		if len(items) > 3000 {
			return nil, errors.New("This review exceeds 3,000 commits")
		}
		token = headers.Get("x-ms-continuationtoken")
		if token == "" {
			return items, nil
		}
		if seen[token] || len(token) > 4096 || len(data.Value) == 0 {
			return nil, errors.New("Azure DevOps returned an invalid commit continuation token")
		}
		seen[token] = true
	}
}
func (c *AzureClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
	ctx, cancel := reviewContext(ctx)
	defer cancel()
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return nil, e
	}
	profile, e := c.currentProfile(ctx)
	if e != nil {
		return nil, e
	}
	iteration, changes, e := c.comparison(ctx, owner, repo, pr)
	if e != nil {
		return nil, e
	}
	files, warnings, e := c.files(ctx, owner, repo, pr, iteration, changes)
	if e != nil {
		return nil, e
	}
	write := !c.options.ReadOnly
	summary := azSummary(pr, owner, repo)
	detail := &githubclient.PullRequestDetail{PullRequest: summary, Files: files, Commits: []githubclient.Commit{}, Comments: []githubclient.Comment{}, Threads: []githubclient.Thread{}, Reviews: []githubclient.Review{}, Checks: []githubclient.CheckRun{}, Statuses: []githubclient.CommitStatus{}, Warnings: warnings, AllowedMergeMethods: []string{"merge", "squash", "rebase"}, RequiredStatusCheckContexts: []string{}, MergeStateStatus: "UNKNOWN", ViewerCanUpdate: write && pr.CreatedBy.ID == profile.ID, Capabilities: &githubclient.ReviewCapabilities{Comments: write, InlineComments: write, Approve: write, RequestChanges: write, ResolveThreads: write, Draft: write, Merge: write, Reviewers: write, Labels: write, Retarget: write, Reopen: write}}
	path, _ := c.prPath(owner, repo, number)
	commits, e := c.commits(ctx, path)
	if e != nil {
		return nil, e
	}
	for _, commit := range commits {
		detail.Commits = append(detail.Commits, githubclient.Commit{SHA: commit.CommitID, Subject: strings.Split(commit.Comment, "\n")[0], Body: commit.Comment, AuthorName: commit.Author.Name, CreatedAt: commit.Author.Date})
	}
	threads, e := c.threads(ctx, owner, repo, number)
	if e != nil {
		return nil, e
	}
	for _, thread := range threads {
		if thread.IsDeleted {
			continue
		}
		comments := []githubclient.Comment{}
		for _, note := range thread.Comments {
			if note.IsDeleted || note.CommentType != "text" && note.CommentType != "" {
				continue
			}
			comment, e := azMappedComment(thread, note, summary.URL)
			if e != nil {
				return nil, e
			}
			comments = append(comments, comment)
		}
		if len(comments) == 0 {
			continue
		}
		if thread.ThreadContext == nil {
			detail.Comments = append(detail.Comments, comments...)
			continue
		}
		first := comments[0]
		outdated := false
		if thread.PullRequestThreadContext != nil {
			outdated = thread.PullRequestThreadContext.IterationContext.SecondComparingIteration != 0 && thread.PullRequestThreadContext.IterationContext.SecondComparingIteration != iteration.ID
		}
		detail.Threads = append(detail.Threads, githubclient.Thread{ID: encodedThread("azure", owner, repo, number, strconv.FormatInt(thread.ID, 10)), Path: first.Path, Line: first.Line, OriginalLine: first.OriginalLine, DiffSide: first.Side, Resolved: thread.Status == "fixed" || thread.Status == "closed" || thread.Status == "wontFix" || thread.Status == "byDesign", Outdated: outdated, ViewerCanResolve: write, ViewerCanUnresolve: write, Comments: comments})
	}
	for _, u := range pr.Reviewers {
		state := ""
		if u.Vote >= 5 {
			state = "APPROVED"
		} else if u.Vote < 0 {
			state = "CHANGES_REQUESTED"
		}
		if state != "" {
			detail.Reviews = append(detail.Reviews, githubclient.Review{ID: stableID(u.ID), User: u.user(), State: state, SubmittedAt: pr.CreationDate, CommitID: summary.Head.SHA})
		}
	}
	var statuses azEnvelope[struct {
		ID           int64  `json:"id"`
		State        string `json:"state"`
		Description  string `json:"description"`
		TargetURL    string `json:"targetUrl"`
		CreationDate string `json:"creationDate"`
		Context      struct {
			Name  string `json:"name"`
			Genre string `json:"genre"`
		} `json:"context"`
	}]
	if _, e = c.api.request(ctx, "GET", azVersion(path+"/statuses"), nil, &statuses); e != nil {
		detail.Warnings = append(detail.Warnings, "PR checks could not be loaded: "+e.Error())
	} else {
		for _, s := range statuses.Value {
			state := "pending"
			if s.State == "succeeded" || s.State == "notApplicable" {
				state = "success"
			} else if s.State == "failed" || s.State == "error" {
				state = "failure"
			}
			detail.Statuses = append(detail.Statuses, githubclient.CommitStatus{ID: s.ID, Context: s.Context.Name, State: state, Description: s.Description, TargetURL: s.TargetURL, CreatedAt: s.CreationDate})
		}
	}
	latest, e := c.verifyHead(ctx, owner, repo, number, summary.Head.SHA)
	if e != nil {
		return nil, e
	}
	if latest.LastMergeTargetCommit.CommitID != summary.Base.SHA || latest.TargetRefName != pr.TargetRefName {
		return nil, errors.New("The target changed while loading the review")
	}
	if pr.MergeStatus == "succeeded" {
		detail.MergeStateStatus = "CLEAN"
	} else if pr.MergeStatus == "conflicts" {
		detail.MergeStateStatus = "DIRTY"
		conflict := false
		detail.PullRequest.Mergeable = &conflict
	} else if pr.MergeStatus == "failure" || pr.MergeStatus == "rejectedByPolicy" {
		detail.MergeStateStatus = "BLOCKED"
	}
	setDetailCounts(detail)
	return detail, nil
}
func (c *AzureClient) PullRequestSource(ctx context.Context, owner, repo string, number int) (Source, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return Source{}, e
	}
	source := Source{HeadRef: pr.SourceRefName, HeadSHA: pr.LastMergeSourceCommit.CommitID, BaseRef: pr.TargetRefName}
	if pr.ForkSource != nil {
		source.HeadURL = pr.ForkSource.Repository.RemoteURL
	}
	return source, nil
}
func (c *AzureClient) GetPullRequestStack(ctx context.Context, owner, repo string, number int) (githubclient.PullRequestStack, error) {
	pr, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.PullRequestStack{}, err
	}
	return providerStack(ctx, c, owner, repo, azSummary(pr, owner, repo))
}
func (c *AzureClient) ReviewChanges(ctx context.Context, owner, repo string, number int, head, base string) (githubclient.ReviewChanges, error) {
	ctx, cancel := reviewContext(ctx)
	defer cancel()
	pr, e := c.verifyHead(ctx, owner, repo, number, head)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	if !immutable(base) || base != pr.LastMergeTargetCommit.CommitID {
		return githubclient.ReviewChanges{}, errors.New("The review base changed. Refresh the pull request")
	}
	iteration, changes, e := c.comparison(ctx, owner, repo, pr)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	files, _, e := c.files(ctx, owner, repo, pr, iteration, changes)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	latest, e := c.verifyHead(ctx, owner, repo, number, head)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	if latest.LastMergeTargetCommit.CommitID != base {
		return githubclient.ReviewChanges{}, errors.New("The base changed while preparing the review")
	}
	return githubclient.ReviewChanges{PullRequest: azSummary(pr, owner, repo), MergeBaseSHA: iteration.CommonRefCommit.CommitID, Files: files}, nil
}
func (c *AzureClient) GetCommitDiff(ctx context.Context, owner, repo, sha string) (*githubclient.CommitDiff, error) {
	if !immutable(sha) {
		return nil, errors.New("Choose an immutable commit")
	}
	path, e := c.repoPath(owner, repo)
	if e != nil {
		return nil, e
	}
	var commit azCommit
	if _, e = c.api.request(ctx, "GET", azVersion(path+"/commits/"+sha), nil, &commit); e != nil {
		return nil, e
	}
	parent := ""
	if len(commit.Parents) > 0 {
		parent = commit.Parents[0]
	}
	changes := []azChange{}
	for skip := 0; ; {
		var data struct {
			Changes []azChange `json:"changes"`
		}
		q := url.Values{"top": {"1000"}, "skip": {strconv.Itoa(skip)}}
		if _, e = c.api.request(ctx, "GET", azVersion(path+"/commits/"+sha+"/changes?"+q.Encode()), nil, &data); e != nil {
			return nil, e
		}
		changes = append(changes, data.Changes...)
		if len(changes) > 3000 {
			return nil, errors.New("This commit exceeds 3,000 changed files")
		}
		if len(data.Changes) < 1000 {
			break
		}
		skip += len(data.Changes)
	}
	pr := azPR{LastMergeSourceCommit: azCommit{CommitID: sha}}
	iteration := azIteration{SourceRefCommit: azCommit{CommitID: sha}, CommonRefCommit: azCommit{CommitID: parent}}
	if parent == "" {
		for i := range changes {
			changes[i].ChangeType = "add"
		}
	}
	files, warnings, e := c.files(ctx, owner, repo, pr, iteration, changes)
	if e != nil {
		return nil, e
	}
	return &githubclient.CommitDiff{SHA: sha, Files: files, Warnings: warnings}, nil
}

var _ API = (*AzureClient)(nil)
var _ SourceReader = (*AzureClient)(nil)
