package reviewproviders

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/githubclient"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BitbucketClient struct {
	api          *transport
	options      Options
	publisher    *Publisher
	mu           sync.Mutex
	profile      bbUser
	profileUntil time.Time
}
type bbLink struct {
	Href string `json:"href"`
}
type bbUser struct {
	UUID        string `json:"uuid"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	Links       struct {
		HTML   bbLink `json:"html"`
		Avatar bbLink `json:"avatar"`
	} `json:"links"`
}

func (u bbUser) user() githubclient.User {
	return githubclient.User{Login: u.UUID, Name: u.DisplayName, AvatarURL: u.Links.Avatar.Href, URL: u.Links.HTML.Href}
}

type bbRepository struct {
	FullName string `json:"full_name"`
	Links    struct {
		HTML bbLink `json:"html"`
	} `json:"links"`
}
type bbBranch struct {
	Repository bbRepository `json:"repository"`
	Branch     struct {
		Name            string   `json:"name"`
		MergeStrategies []string `json:"merge_strategies"`
	} `json:"branch"`
	Commit struct {
		Hash string `json:"hash"`
	} `json:"commit"`
}
type bbParticipant struct {
	User           bbUser `json:"user"`
	Approved       bool   `json:"approved"`
	State          string `json:"state"`
	ParticipatedOn string `json:"participated_on"`
}
type bbPR struct {
	ID           int             `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	State        string          `json:"state"`
	Author       bbUser          `json:"author"`
	Draft        bool            `json:"draft"`
	CreatedOn    string          `json:"created_on"`
	UpdatedOn    string          `json:"updated_on"`
	Source       bbBranch        `json:"source"`
	Destination  bbBranch        `json:"destination"`
	Reviewers    []bbUser        `json:"reviewers"`
	Participants []bbParticipant `json:"participants"`
	CommentCount int             `json:"comment_count"`
	Links        struct {
		HTML bbLink `json:"html"`
	} `json:"links"`
	MergeCommit struct {
		Hash string `json:"hash"`
	} `json:"merge_commit"`
}
type bbComment struct {
	ID        int64  `json:"id"`
	User      bbUser `json:"user"`
	CreatedOn string `json:"created_on"`
	UpdatedOn string `json:"updated_on"`
	Deleted   bool   `json:"deleted"`
	Content   struct {
		Raw string `json:"raw"`
	} `json:"content"`
	Parent *struct {
		ID int64 `json:"id"`
	} `json:"parent"`
	Resolution *struct {
		Type string `json:"type"`
	} `json:"resolution"`
	Inline *struct {
		Path     string `json:"path"`
		From     *int   `json:"from"`
		To       *int   `json:"to"`
		Outdated bool   `json:"outdated"`
	} `json:"inline"`
	Links struct {
		HTML bbLink `json:"html"`
	} `json:"links"`
}
type bbCommit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Date    string `json:"date"`
	Author  struct {
		Raw  string `json:"raw"`
		User bbUser `json:"user"`
	} `json:"author"`
	Links struct {
		HTML bbLink `json:"html"`
	} `json:"links"`
}
type bbDiffstat struct {
	Status       string `json:"status"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	Old          *struct {
		Path string `json:"path"`
	} `json:"old"`
	New *struct {
		Path string `json:"path"`
	} `json:"new"`
}
type bbPage[T any] struct {
	Values []T    `json:"values"`
	Size   *int   `json:"size"`
	Next   string `json:"next"`
}

func NewBitbucket(host, email, token string, options Options) (*BitbucketClient, error) {
	api, err := newTransport("bitbucket", host, token, email, options)
	if err != nil {
		return nil, err
	}
	publisher := options.Publisher
	if publisher == nil {
		publisher = NewPublisher(nil)
	}
	return &BitbucketClient{api: api, options: options, publisher: publisher}, nil
}
func (c *BitbucketClient) currentProfile(ctx context.Context) (bbUser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.profileUntil) {
		return c.profile, nil
	}
	var user bbUser
	_, err := c.api.request(ctx, "GET", "/user", nil, &user)
	if err == nil && user.UUID == "" {
		err = errors.New("Bitbucket did not identify the signed-in account")
	}
	if err == nil {
		c.profile = user
		c.profileUntil = time.Now().Add(2 * time.Minute)
	}
	return user, err
}
func (c *BitbucketClient) CurrentUser(ctx context.Context) (githubclient.User, error) {
	u, e := c.currentProfile(ctx)
	return u.user(), e
}
func bbRepoPath(owner, repo string) (string, error) {
	a, e := component(owner)
	if e != nil {
		return "", e
	}
	b, e := component(repo)
	return "/repositories/" + a + "/" + b, e
}
func bbPRPath(owner, repo string, number int) (string, error) {
	p, e := bbRepoPath(owner, repo)
	if e != nil {
		return "", e
	}
	if number < 1 {
		return "", errors.New("Choose a pull request")
	}
	return p + "/pullrequests/" + strconv.Itoa(number), nil
}
func (c *BitbucketClient) getPR(ctx context.Context, owner, repo string, number int) (bbPR, error) {
	var pr bbPR
	p, e := bbPRPath(owner, repo, number)
	if e != nil {
		return pr, e
	}
	_, e = c.api.request(ctx, "GET", p, nil, &pr)
	if e == nil && pr.ID != number {
		e = errors.New("Bitbucket returned a different pull request")
	}
	return pr, e
}
func (c *BitbucketClient) verifyHead(ctx context.Context, owner, repo string, number int, head string) (bbPR, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e == nil && (!immutable(head) || pr.Source.Commit.Hash != head) {
		e = errors.New("The pull request head changed. Refresh before continuing")
	}
	return pr, e
}
func bbSummary(pr bbPR) githubclient.PullRequestSummary {
	status := map[string]string{"OPEN": "open", "MERGED": "merged", "DECLINED": "closed", "SUPERSEDED": "closed"}[pr.State]
	if status == "" {
		status = "closed"
	}
	if pr.Draft && status == "open" {
		status = "draft"
	}
	reviewers := []githubclient.User{}
	for _, u := range pr.Reviewers {
		reviewers = append(reviewers, u.user())
	}
	return githubclient.PullRequestSummary{Provider: "bitbucket", Number: pr.ID, NodeID: strconv.Itoa(pr.ID), Title: pr.Title, Body: pr.Description, URL: pr.Links.HTML.Href, Status: status, Author: pr.Author.user(), CreatedAt: pr.CreatedOn, UpdatedAt: pr.UpdatedOn, Head: githubclient.Branch{Ref: pr.Source.Branch.Name, SHA: pr.Source.Commit.Hash, Repository: pr.Source.Repository.FullName}, Base: githubclient.Branch{Ref: pr.Destination.Branch.Name, SHA: pr.Destination.Commit.Hash, Repository: pr.Destination.Repository.FullName}, Reviewers: reviewers, Assignees: []githubclient.User{}, Labels: []string{}, Draft: pr.Draft, Merged: pr.State == "MERGED", CommentsCount: pr.CommentCount, Warnings: []string{}}
}
func (c *BitbucketClient) nextPath(link, endpoint string) (string, error) {
	u, e := url.Parse(link)
	base, _ := url.Parse(c.api.base)
	expected := strings.TrimRight(base.EscapedPath(), "/") + strings.Split(endpoint, "?")[0]
	// PR diffstat redirects to the repository's immutable comparison resource.
	redirectedStat := false
	if index := strings.Index(expected, "/pullrequests/"); index >= 0 && strings.HasSuffix(expected, "/diffstat") && e == nil {
		redirectedStat = strings.HasPrefix(u.EscapedPath(), expected[:index]+"/diffstat/")
	}
	if e != nil || u.User != nil || u.Fragment != "" || u.Scheme != base.Scheme || u.Host != base.Host || (u.EscapedPath() != expected && !redirectedStat) {
		return "", errors.New("Bitbucket returned a pagination link outside this resource")
	}
	return strings.TrimPrefix(u.EscapedPath(), strings.TrimRight(base.EscapedPath(), "/")) + "?" + u.RawQuery, nil
}
func bbArray[T any](ctx context.Context, c *BitbucketClient, endpoint string, limit int) ([]T, error) {
	items := []T{}
	path := endpoint
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	path += sep + "pagelen=100"
	seen := map[string]bool{}
	for {
		if seen[path] {
			return nil, errors.New("Bitbucket repeated a pagination link")
		}
		seen[path] = true
		var page bbPage[T]
		if _, err := c.api.request(ctx, "GET", path, nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Values...)
		if len(items) > limit {
			return nil, errors.New("Bitbucket data exceeds the supported review limit")
		}
		if page.Next == "" {
			return items, nil
		}
		var err error
		path, err = c.nextPath(page.Next, endpoint)
		if err != nil {
			return nil, err
		}
	}
}

type bbCursor struct {
	Scope string `json:"scope"`
	Next  string `json:"next"`
}

func (c *BitbucketClient) ListPullRequestsPage(ctx context.Context, owner, repo string, input githubclient.PullRequestListRequest) (githubclient.PullRequestPage, error) {
	result := githubclient.PullRequestPage{Items: []githubclient.PullRequestSummary{}, TotalCount: -1, OpenCount: -1}
	r, e := normalizeList(input)
	if e != nil {
		return result, e
	}
	path, e := bbRepoPath(owner, repo)
	if e != nil {
		return result, e
	}
	endpoint := path + "/pullrequests"
	scope := hash("bitbucket", c.api.host, owner, repo, r.State, r.View, r.Query, strconv.Itoa(r.PageSize))
	q := url.Values{"pagelen": {strconv.Itoa(r.PageSize)}, "sort": {"-updated_on"}}
	switch r.State {
	case "open":
		q.Add("state", "OPEN")
	case "closed":
		q.Add("state", "DECLINED")
		q.Add("state", "SUPERSEDED")
	case "merged":
		q.Add("state", "MERGED")
	case "all":
		for _, s := range []string{"OPEN", "MERGED", "DECLINED", "SUPERSEDED"} {
			q.Add("state", s)
		}
	}
	filters := []string{}
	quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	if r.Query != "" {
		if n, err := strconv.Atoi(strings.TrimPrefix(r.Query, "#")); err == nil && n > 0 {
			filters = append(filters, "id = "+strconv.Itoa(n))
		} else {
			filters = append(filters, "(title ~ "+quote(r.Query)+" OR description ~ "+quote(r.Query)+")")
		}
	}
	if r.View != "inbox" {
		u, err := c.currentProfile(ctx)
		if err != nil {
			return result, err
		}
		switch r.View {
		case "mine":
			filters = append(filters, "author.uuid = "+quote(u.UUID))
		case "assigned", "review-requested":
			filters = append(filters, "reviewers.uuid = "+quote(u.UUID))
		case "drafts":
			filters = append(filters, "draft = true")
		}
	}
	if len(filters) > 0 {
		q.Set("q", strings.Join(filters, " AND "))
	}
	path = endpoint + "?" + q.Encode()
	if r.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(r.Cursor)
		var cur bbCursor
		if err != nil || json.Unmarshal(data, &cur) != nil || cur.Scope != scope {
			return result, errors.New("This cursor belongs to another review query")
		}
		path, e = c.nextPath(cur.Next, endpoint)
		if e != nil {
			return result, e
		}
	}
	var page bbPage[bbPR]
	if _, e = c.api.request(ctx, "GET", path, nil, &page); e != nil {
		return result, e
	}
	for _, pr := range page.Values {
		result.Items = append(result.Items, bbSummary(pr))
	}
	if page.Size != nil {
		result.TotalCount = *page.Size
		if r.State == "open" && r.View == "inbox" && r.Query == "" {
			result.OpenCount = *page.Size
		}
	}
	if page.Next != "" {
		if _, e = c.nextPath(page.Next, endpoint); e != nil {
			return result, e
		}
		data, _ := json.Marshal(bbCursor{Scope: scope, Next: page.Next})
		result.HasNextPage = true
		result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return result, nil
}
func (c *BitbucketClient) ListPullRequests(ctx context.Context, owner, repo, state string) ([]githubclient.PullRequestSummary, error) {
	r, e := c.ListPullRequestsPage(ctx, owner, repo, githubclient.PullRequestListRequest{State: state, PageSize: 100})
	return r.Items, e
}
func bbMappedComment(note bbComment) githubclient.Comment {
	comment := githubclient.Comment{ID: note.ID, Body: cleanBody(note.Content.Raw), User: note.User.user(), CreatedAt: note.CreatedOn, UpdatedAt: note.UpdatedOn, URL: note.Links.HTML.Href}
	if note.Parent != nil {
		comment.InReplyToID = note.Parent.ID
	}
	if note.Inline != nil {
		comment.Path = note.Inline.Path
		comment.Line = note.Inline.To
		comment.Side = "RIGHT"
		if comment.Line == nil {
			comment.Line = note.Inline.From
			comment.Side = "LEFT"
		}
		comment.OriginalLine = comment.Line
	}
	return comment
}
func bbMappedCommit(commit bbCommit) githubclient.Commit {
	return githubclient.Commit{SHA: commit.Hash, Body: commit.Message, Subject: strings.Split(commit.Message, "\n")[0], CreatedAt: commit.Date, Author: commit.Author.User.user(), AuthorName: commit.Author.Raw, URL: commit.Links.HTML.Href}
}
func (c *BitbucketClient) files(ctx context.Context, path string) ([]githubclient.ChangedFile, []string, error) {
	index := strings.LastIndex(path, "/diff")
	if index < 0 {
		return nil, nil, errors.New("Invalid Bitbucket comparison path")
	}
	stats, e := bbArray[bbDiffstat](ctx, c, path[:index]+"/diffstat"+path[index+len("/diff"):], 3000)
	if e != nil {
		return nil, nil, e
	}
	data, _, e := c.api.raw(ctx, "GET", path, "text/plain", nil, 16*1024*1024)
	if e != nil {
		return nil, nil, e
	}
	patches, e := splitFilePatches(string(data))
	if e != nil {
		return nil, nil, e
	}
	files := []githubclient.ChangedFile{}
	warnings := []string{}
	for _, stat := range stats {
		file := githubclient.ChangedFile{Status: stat.Status, Additions: stat.LinesAdded, Deletions: stat.LinesRemoved}
		if stat.New != nil {
			file.Path = stat.New.Path
		}
		if stat.Old != nil {
			file.PreviousPath = stat.Old.Path
			if file.Path == "" {
				file.Path = stat.Old.Path
			}
		}
		if !validFile(file.Path) {
			return nil, nil, errors.New("Bitbucket returned an invalid file path")
		}
		if file.Status == "deleted" {
			file.Status = "removed"
		}
		file.Patch = patches[file.Path]
		if file.Patch == "" && (file.Additions > 0 || file.Deletions > 0) {
			warnings = append(warnings, "Bitbucket omitted a text patch for "+file.Path)
		}
		files = append(files, file)
	}
	return files, warnings, nil
}
func (c *BitbucketClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (*githubclient.PullRequestDetail, error) {
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
	path, _ := bbPRPath(owner, repo, number)
	write := !c.options.ReadOnly
	detail := &githubclient.PullRequestDetail{PullRequest: bbSummary(pr), Files: []githubclient.ChangedFile{}, Commits: []githubclient.Commit{}, Comments: []githubclient.Comment{}, Threads: []githubclient.Thread{}, Reviews: []githubclient.Review{}, Checks: []githubclient.CheckRun{}, Statuses: []githubclient.CommitStatus{}, Warnings: []string{}, AllowedMergeMethods: []string{}, RequiredStatusCheckContexts: []string{}, MergeStateStatus: "UNKNOWN", ViewerCanUpdate: write && profile.UUID == pr.Author.UUID, Capabilities: &githubclient.ReviewCapabilities{Comments: write, InlineComments: write, Approve: write, RequestChanges: write, ResolveThreads: write, Draft: write, Reviewers: write, Retarget: write}}
	files, warnings, e := c.files(ctx, path+"/diff")
	if e != nil {
		return nil, e
	}
	detail.Files = files
	detail.Warnings = append(detail.Warnings, warnings...)
	commits, e := bbArray[bbCommit](ctx, c, path+"/commits", 3000)
	if e != nil {
		return nil, e
	}
	for _, commit := range commits {
		detail.Commits = append(detail.Commits, bbMappedCommit(commit))
	}
	notes, e := bbArray[bbComment](ctx, c, path+"/comments", 5000)
	if e != nil {
		return nil, e
	}
	byRoot := map[int64][]githubclient.Comment{}
	for _, note := range notes {
		if note.Deleted {
			continue
		}
		root := note.ID
		parent := note.Parent
		seen := map[int64]bool{}
		for parent != nil {
			if seen[parent.ID] {
				return nil, errors.New("Bitbucket returned a cyclic comment conversation")
			}
			seen[parent.ID] = true
			root = parent.ID
			parent = nil
			for _, p := range notes {
				if p.ID == root {
					parent = p.Parent
					break
				}
			}
		}
		byRoot[root] = append(byRoot[root], bbMappedComment(note))
	}
	for _, note := range notes {
		if note.Parent != nil {
			continue
		}
		comments := byRoot[note.ID]
		if len(comments) == 0 {
			continue
		}
		if note.Inline == nil {
			detail.Comments = append(detail.Comments, comments...)
			continue
		}
		line, side := note.Inline.To, "RIGHT"
		if line == nil {
			line = note.Inline.From
			side = "LEFT"
		}
		detail.Threads = append(detail.Threads, githubclient.Thread{ID: encodedThread("bitbucket", owner, repo, number, strconv.FormatInt(note.ID, 10)), Path: note.Inline.Path, Line: line, OriginalLine: line, DiffSide: side, Resolved: note.Resolution != nil, Outdated: note.Inline.Outdated, ViewerCanResolve: write, ViewerCanUnresolve: write, Comments: comments})
	}
	for _, p := range pr.Participants {
		state := ""
		if p.Approved {
			state = "APPROVED"
		}
		if p.State == "changes_requested" {
			state = "CHANGES_REQUESTED"
		}
		if state != "" {
			detail.Reviews = append(detail.Reviews, githubclient.Review{ID: stableID(p.User.UUID), User: p.User.user(), State: state, SubmittedAt: p.ParticipatedOn, CommitID: pr.Source.Commit.Hash})
		}
	}
	var statuses []struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		State       string `json:"state"`
		Description string `json:"description"`
		URL         string `json:"url"`
		CreatedOn   string `json:"created_on"`
	}
	statuses, e = bbArray[struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		State       string `json:"state"`
		Description string `json:"description"`
		URL         string `json:"url"`
		CreatedOn   string `json:"created_on"`
	}](ctx, c, path+"/statuses", 2000)
	if e != nil {
		detail.Warnings = append(detail.Warnings, "Commit status checks could not be loaded: "+e.Error())
	} else {
		for _, s := range statuses {
			state := "pending"
			if s.State == "SUCCESSFUL" {
				state = "success"
			} else if s.State == "FAILED" || s.State == "STOPPED" {
				state = "failure"
			}
			name := s.Name
			if name == "" {
				name = s.Key
			}
			detail.Statuses = append(detail.Statuses, githubclient.CommitStatus{ID: stableID(s.Key), Context: name, State: state, Description: s.Description, TargetURL: s.URL, CreatedAt: s.CreatedOn})
		}
	}
	latest, e := c.verifyHead(ctx, owner, repo, number, pr.Source.Commit.Hash)
	if e != nil {
		return nil, e
	}
	if latest.Destination.Commit.Hash != pr.Destination.Commit.Hash || latest.Destination.Branch.Name != pr.Destination.Branch.Name {
		return nil, errors.New("The pull request base changed while loading. Refresh its review")
	}
	setDetailCounts(detail)
	detail.Warnings = append(detail.Warnings, "Merge in Bitbucket to enforce its latest merge checks; its REST merge endpoint has no expected-head precondition")
	return detail, nil
}
func (c *BitbucketClient) ReadFileAtCommit(ctx context.Context, owner, repo, file, sha string) (string, error) {
	if !immutable(sha) || !validFile(file) {
		return "", errors.New("Choose a relative source path and immutable commit")
	}
	path, e := bbRepoPath(owner, repo)
	if e != nil {
		return "", e
	}
	parts := strings.Split(file, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	data, _, e := c.api.raw(ctx, "GET", path+"/src/"+sha+"/"+strings.Join(parts, "/"), "application/octet-stream", nil, 2*1024*1024)
	if e != nil {
		return "", e
	}
	return textFile(data)
}
func (c *BitbucketClient) GetCommitDiff(ctx context.Context, owner, repo, sha string) (*githubclient.CommitDiff, error) {
	if !immutable(sha) {
		return nil, errors.New("Choose an immutable commit")
	}
	path, e := bbRepoPath(owner, repo)
	if e != nil {
		return nil, e
	}
	var commit bbCommit
	if _, e = c.api.request(ctx, "GET", path+"/commit/"+sha, nil, &commit); e != nil {
		return nil, e
	}
	files, _, e := c.files(ctx, path+"/diff/"+sha)
	if e != nil {
		return nil, e
	}
	return &githubclient.CommitDiff{SHA: sha, Files: files, Warnings: []string{}}, nil
}
func (c *BitbucketClient) PullRequestSource(ctx context.Context, owner, repo string, number int) (Source, error) {
	pr, e := c.getPR(ctx, owner, repo, number)
	if e != nil {
		return Source{}, e
	}
	return Source{HeadURL: bbCloneURL(pr.Source.Repository), HeadRef: "refs/heads/" + pr.Source.Branch.Name, HeadSHA: pr.Source.Commit.Hash, BaseURL: bbCloneURL(pr.Destination.Repository), BaseRef: "refs/heads/" + pr.Destination.Branch.Name}, nil
}
func bbCloneURL(repo bbRepository) string {
	if repo.FullName == "" {
		return ""
	}
	return "https://bitbucket.org/" + repo.FullName + ".git"
}
func (c *BitbucketClient) GetPullRequestStack(ctx context.Context, owner, repo string, number int) (githubclient.PullRequestStack, error) {
	pr, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return githubclient.PullRequestStack{}, err
	}
	return providerStack(ctx, c, owner, repo, bbSummary(pr))
}
func (c *BitbucketClient) ReviewChanges(ctx context.Context, owner, repo string, number int, head, base string) (githubclient.ReviewChanges, error) {
	ctx, cancel := reviewContext(ctx)
	defer cancel()
	pr, e := c.verifyHead(ctx, owner, repo, number, head)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	if !immutable(base) || pr.Destination.Commit.Hash != base {
		return githubclient.ReviewChanges{}, errors.New("The base changed. Refresh before starting a review")
	}
	path, _ := bbPRPath(owner, repo, number)
	files, _, e := c.files(ctx, path+"/diff")
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	repositoryPath, _ := bbRepoPath(owner, repo)
	var mergeBase struct {
		Hash string `json:"hash"`
	}
	_, e = c.api.request(ctx, "GET", repositoryPath+"/merge-base/"+base+".."+head, nil, &mergeBase)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	if !immutable(mergeBase.Hash) {
		return githubclient.ReviewChanges{}, errors.New("Bitbucket did not provide an immutable merge base")
	}
	latest, e := c.verifyHead(ctx, owner, repo, number, head)
	if e != nil {
		return githubclient.ReviewChanges{}, e
	}
	if latest.Destination.Commit.Hash != base {
		return githubclient.ReviewChanges{}, errors.New("The base changed while preparing the review")
	}
	return githubclient.ReviewChanges{PullRequest: bbSummary(pr), Files: files, MergeBaseSHA: mergeBase.Hash}, nil
}

var _ API = (*BitbucketClient)(nil)
var _ SourceReader = (*BitbucketClient)(nil)

// Keep errors concise when provider-specific metadata is not part of its API.
func unsupported(provider, feature string) error {
	return fmt.Errorf("%s does not expose %s for pull requests", provider, feature)
}
