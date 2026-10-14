package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// PullRequestListRequest describes one page, including its server-side filters.
// Cursors are opaque and belong to the exact state, view, and query that produced them.
type PullRequestListRequest struct {
	State    string `json:"state"`
	View     string `json:"view"`
	Query    string `json:"query"`
	Cursor   string `json:"cursor"`
	PageSize int    `json:"pageSize"`
}

type PullRequestPage struct {
	Items       []PullRequestSummary `json:"items"`
	NextCursor  string               `json:"nextCursor"`
	HasNextPage bool                 `json:"hasNextPage"`
	TotalCount  int                  `json:"totalCount"`
	OpenCount   int                  `json:"openCount"`
	// ResultLimit is zero for repository connections. GitHub search exposes at
	// most 1,000 matching results, even when TotalCount is higher.
	ResultLimit int `json:"resultLimit"`
}

const maxPullRequestCursorLength = 2048

func (r PullRequestListRequest) normalized() (PullRequestListRequest, error) {
	if r.State == "" {
		r.State = "open"
	}
	if r.View == "" {
		r.View = "inbox"
	}
	switch r.State {
	case "open", "all", "merged", "closed":
	default:
		return r, errors.New("Pull request state must be open, all, merged, or closed")
	}
	switch r.View {
	case "inbox", "mine", "review-requested", "assigned", "drafts":
	default:
		return r, errors.New("Choose a valid pull request view")
	}
	if r.PageSize == 0 {
		r.PageSize = 25
	}
	if r.PageSize < 1 || r.PageSize > 100 {
		return r, errors.New("Pull request page size must be between 1 and 100")
	}
	if len(r.Cursor) > maxPullRequestCursorLength {
		return r, errors.New("Invalid pull request pagination cursor")
	}
	r.Query = strings.Join(strings.Fields(r.Query), " ")
	return r, nil
}

func (r PullRequestListRequest) states() []string {
	if r.State == "all" {
		return []string{"OPEN", "CLOSED", "MERGED"}
	}
	return []string{strings.ToUpper(r.State)}
}

func (r PullRequestListRequest) searchQuery(owner, repo string) string {
	parts := []string{"repo:" + owner + "/" + repo, "is:pr"}
	switch r.State {
	case "open":
		parts = append(parts, "is:open")
	case "merged":
		parts = append(parts, "is:merged")
	case "closed":
		parts = append(parts, "is:closed", "is:unmerged")
	}
	switch r.View {
	case "mine":
		parts = append(parts, "author:@me")
	case "review-requested":
		parts = append(parts, "review-requested:@me")
	case "assigned":
		parts = append(parts, "assignee:@me")
	case "drafts":
		parts = append(parts, "is:draft")
	}
	parts = append(parts, "sort:updated-desc")
	if r.Query != "" {
		// The search box accepts text, not qualifiers. Quoting it keeps words
		// such as repo: and OR from changing the repository or view scope.
		parts = append(parts, strconv.Quote(r.Query))
	}
	return strings.Join(parts, " ")
}

type graphTotalCount struct {
	TotalCount *int `json:"totalCount"`
}

type graphListPullRequest struct {
	ID             string `json:"id"`
	Number         int    `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	Merged         bool   `json:"merged"`
	Author         User   `json:"author"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	HeadRefName    string `json:"headRefName"`
	HeadRefOID     string `json:"headRefOid"`
	BaseRefName    string `json:"baseRefName"`
	BaseRefOID     string `json:"baseRefOid"`
	HeadRepository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"headRepository"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *struct {
				Login     string `json:"login"`
				Name      string `json:"name"`
				AvatarURL string `json:"avatarUrl"`
				URL       string `json:"url"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
		PageInfo pageInfo `json:"pageInfo"`
	} `json:"reviewRequests"`
	Assignees struct {
		Nodes    []User   `json:"nodes"`
		PageInfo pageInfo `json:"pageInfo"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
		PageInfo pageInfo `json:"pageInfo"`
	} `json:"labels"`
	Mergeable                 string           `json:"mergeable"`
	MergeStateStatus          string           `json:"mergeStateStatus"`
	Additions                 *int             `json:"additions"`
	Deletions                 *int             `json:"deletions"`
	ChangedFiles              *int             `json:"changedFiles"`
	Commits                   *graphTotalCount `json:"commits"`
	Comments                  *graphTotalCount `json:"comments"`
	ReviewThreads             *graphTotalCount `json:"reviewThreads"`
	ViewerLatestReviewRequest *struct {
		ID string `json:"id"`
	} `json:"viewerLatestReviewRequest"`
}

func (g graphListPullRequest) summary() PullRequestSummary {
	p := PullRequestSummary{
		Number: g.Number, NodeID: g.ID, Title: g.Title, Body: g.Body, URL: g.URL,
		Status: strings.ToLower(g.State), Author: g.Author, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		Head:  Branch{Ref: g.HeadRefName, SHA: g.HeadRefOID},
		Base:  Branch{Ref: g.BaseRefName, SHA: g.BaseRefOID, Repository: g.Repository.NameWithOwner},
		Draft: g.IsDraft, Merged: g.Merged || g.State == "MERGED", MergeableState: strings.ToLower(g.MergeStateStatus),
		Reviewers: []User{}, Assignees: append([]User{}, g.Assignees.Nodes...), Labels: []string{}, Warnings: []string{},
	}
	if g.HeadRepository != nil {
		p.Head.Repository = g.HeadRepository.NameWithOwner
	}
	if owner, _, ok := strings.Cut(p.Head.Repository, "/"); ok {
		p.Head.Label = owner + ":" + p.Head.Ref
	}
	if owner, _, ok := strings.Cut(p.Base.Repository, "/"); ok {
		p.Base.Label = owner + ":" + p.Base.Ref
	}
	if p.Merged {
		p.Status = "merged"
	} else if p.Draft && p.Status == "open" {
		p.Status = "draft"
	}
	if g.Mergeable == "MERGEABLE" || g.Mergeable == "CONFLICTING" {
		mergeable := g.Mergeable == "MERGEABLE"
		p.Mergeable = &mergeable
	}
	for _, request := range g.ReviewRequests.Nodes {
		if request.RequestedReviewer != nil && request.RequestedReviewer.Login != "" {
			r := request.RequestedReviewer
			p.Reviewers = append(p.Reviewers, User{Login: r.Login, Name: r.Name, AvatarURL: r.AvatarURL, URL: r.URL})
		}
	}
	for _, label := range g.Labels.Nodes {
		p.Labels = append(p.Labels, label.Name)
	}
	p.CountsKnown = g.Additions != nil && g.Deletions != nil && g.ChangedFiles != nil &&
		g.Commits != nil && g.Commits.TotalCount != nil && g.Comments != nil && g.Comments.TotalCount != nil && g.ReviewThreads != nil && g.ReviewThreads.TotalCount != nil
	if g.Additions != nil {
		p.Additions = *g.Additions
	}
	if g.Deletions != nil {
		p.Deletions = *g.Deletions
	}
	if g.ChangedFiles != nil {
		p.ChangedFiles = *g.ChangedFiles
	}
	if g.Commits != nil && g.Commits.TotalCount != nil {
		p.CommitsCount = *g.Commits.TotalCount
	}
	if g.Comments != nil && g.Comments.TotalCount != nil {
		p.CommentsCount = *g.Comments.TotalCount
	}
	if g.ReviewThreads != nil && g.ReviewThreads.TotalCount != nil {
		p.ThreadsCount = *g.ReviewThreads.TotalCount
	}
	if !p.CountsKnown {
		p.Warnings = append(p.Warnings, "Pull request totals unavailable; reload this pull request")
	}
	if g.ReviewRequests.PageInfo.HasNextPage || g.Assignees.PageInfo.HasNextPage || g.Labels.PageInfo.HasNextPage {
		p.Warnings = append(p.Warnings, "Some pull request metadata is not shown in the list; open the pull request to load its details")
	}
	return p
}

const listPullRequestFields = `id number title body url state isDraft merged createdAt updatedAt
author{login avatarUrl url ... on User{name}}
headRefName headRefOid headRepository{nameWithOwner} baseRefName baseRefOid repository{nameWithOwner}
reviewRequests(first:100){nodes{requestedReviewer{... on User{login name avatarUrl url} ... on Bot{login avatarUrl url}}}pageInfo{hasNextPage}}
assignees(first:100){nodes{login name avatarUrl url}pageInfo{hasNextPage}}
labels(first:100){nodes{name}pageInfo{hasNextPage}}
mergeable mergeStateStatus additions deletions changedFiles commits(first:1){totalCount} comments(first:1){totalCount} reviewThreads(first:1){totalCount}`

const repositoryPullRequestsPageQuery = `query($owner:String!,$repo:String!,$states:[PullRequestState!]!,$pageSize:Int!,$cursor:String){
repository(owner:$owner,name:$repo){
openPullRequests:pullRequests(first:1,states:[OPEN]){totalCount}
pullRequests(first:$pageSize,states:$states,after:$cursor,orderBy:{field:UPDATED_AT,direction:DESC}){
totalCount nodes{` + listPullRequestFields + `}pageInfo{hasNextPage endCursor}}}}`

const searchPullRequestsPageQuery = `query($owner:String!,$repo:String!,$query:String!,$pageSize:Int!,$cursor:String){
repository(owner:$owner,name:$repo){openPullRequests:pullRequests(first:1,states:[OPEN]){totalCount}}
search(query:$query,type:ISSUE,first:$pageSize,after:$cursor){issueCount nodes{... on PullRequest{` + listPullRequestFields + `}}pageInfo{hasNextPage endCursor}}}`

const numberedPullRequestPageQuery = `query($owner:String!,$repo:String!,$number:Int!){viewer{login}
repository(owner:$owner,name:$repo){openPullRequests:pullRequests(first:1,states:[OPEN]){totalCount}
pullRequest(number:$number){viewerLatestReviewRequest{id} ` + listPullRequestFields + `}}}`

func pullRequestSearchNumber(query string) (int, bool) {
	text := strings.TrimPrefix(query, "#")
	if text == "" {
		return 0, false
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	number, err := strconv.ParseInt(text, 10, 32)
	return int(number), err == nil && number > 0
}

func (r PullRequestListRequest) matchesNumbered(g *graphListPullRequest, viewer string) bool {
	if r.State != "all" && !strings.EqualFold(r.State, g.State) {
		return false
	}
	switch r.View {
	case "mine":
		return strings.EqualFold(g.Author.Login, viewer)
	case "assigned":
		for _, assignee := range g.Assignees.Nodes {
			if strings.EqualFold(assignee.Login, viewer) {
				return true
			}
		}
		return false
	case "review-requested":
		if g.ViewerLatestReviewRequest != nil && g.ViewerLatestReviewRequest.ID != "" {
			return true
		}
		for _, request := range g.ReviewRequests.Nodes {
			reviewer := request.RequestedReviewer
			if reviewer != nil && strings.EqualFold(reviewer.Login, viewer) {
				return true
			}
		}
		return false
	case "drafts":
		return g.IsDraft
	}
	return true
}

func (c *Client) numberedPullRequestPage(ctx context.Context, owner, repo string, number int, request PullRequestListRequest) (PullRequestPage, error) {
	result := PullRequestPage{Items: []PullRequestSummary{}}
	if request.Cursor != "" {
		return result, errors.New("Pull request number searches do not have additional pages")
	}
	envelope, err := c.graphResponse(ctx, numberedPullRequestPageQuery, map[string]any{"owner": owner, "repo": repo, "number": number})
	if err != nil {
		return result, err
	}
	var data struct {
		Viewer     *User `json:"viewer"`
		Repository *struct {
			OpenPullRequests *graphTotalCount      `json:"openPullRequests"`
			PullRequest      *graphListPullRequest `json:"pullRequest"`
		} `json:"repository"`
	}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return result, err
		}
	}
	for _, failure := range envelope.Errors {
		// A missing number is an empty search only when the repository and its
		// count are still accessible. Other missing resources and auth errors
		// retain the normal provider error handling and secret redaction.
		missingNumber := strings.EqualFold(failure.Type, "NOT_FOUND") || strings.EqualFold(failure.Extensions.Code, "NOT_FOUND")
		if !missingNumber || len(failure.Path) != 2 || failure.Path[0] != "repository" || failure.Path[1] != "pullRequest" ||
			data.Repository == nil || data.Repository.PullRequest != nil || data.Repository.OpenPullRequests == nil || data.Repository.OpenPullRequests.TotalCount == nil || *data.Repository.OpenPullRequests.TotalCount < 0 {
			return result, c.graphFailure(failure)
		}
	}
	if data.Repository == nil {
		return result, errors.New("GitHub repository is inaccessible")
	}
	if data.Repository.OpenPullRequests == nil || data.Repository.OpenPullRequests.TotalCount == nil || *data.Repository.OpenPullRequests.TotalCount < 0 {
		return result, errors.New("GitHub returned no repository pull request count")
	}
	result.OpenCount = *data.Repository.OpenPullRequests.TotalCount
	if data.Repository.PullRequest == nil {
		return result, nil
	}
	if data.Repository.PullRequest.Number != number {
		return result, errors.New("GitHub returned an invalid pull request number")
	}
	if data.Viewer == nil || data.Viewer.Login == "" {
		return result, errors.New("GitHub returned no viewer for the pull request search")
	}
	if request.matchesNumbered(data.Repository.PullRequest, data.Viewer.Login) {
		result.Items = append(result.Items, data.Repository.PullRequest.summary())
		result.TotalCount = 1
	}
	return result, nil
}

type graphPullRequestPageConnection struct {
	Nodes      []*graphListPullRequest `json:"nodes"`
	TotalCount *int                    `json:"totalCount"`
	IssueCount *int                    `json:"issueCount"`
	PageInfo   pageInfo                `json:"pageInfo"`
}

// ListPullRequestsPage makes one GraphQL request. It deliberately does not
// follow the returned cursor or enrich rows by scanning repository history.
func (c *Client) ListPullRequestsPage(ctx context.Context, owner, repo string, input PullRequestListRequest) (PullRequestPage, error) {
	result := PullRequestPage{Items: []PullRequestSummary{}}
	if _, err := repoPath(owner, repo); err != nil {
		return result, err
	}
	request, err := input.normalized()
	if err != nil {
		return result, err
	}
	if number, ok := pullRequestSearchNumber(request.Query); ok {
		return c.numberedPullRequestPage(ctx, owner, repo, number, request)
	}
	var cursor any
	if request.Cursor != "" {
		cursor = request.Cursor
	}
	variables := map[string]any{"owner": owner, "repo": repo, "pageSize": request.PageSize, "cursor": cursor}
	query := repositoryPullRequestsPageQuery
	search := request.View != "inbox" || request.Query != ""
	if search {
		query = searchPullRequestsPageQuery
		variables["query"] = request.searchQuery(owner, repo)
	} else {
		variables["states"] = request.states()
	}
	var data struct {
		Repository *struct {
			PullRequests     *graphPullRequestPageConnection `json:"pullRequests"`
			OpenPullRequests *graphTotalCount                `json:"openPullRequests"`
		} `json:"repository"`
		Search *graphPullRequestPageConnection `json:"search"`
	}
	if err := c.graph(ctx, query, variables, &data); err != nil {
		return result, err
	}
	if data.Repository == nil {
		return result, errors.New("GitHub repository is inaccessible")
	}
	if data.Repository.OpenPullRequests == nil || data.Repository.OpenPullRequests.TotalCount == nil {
		return result, errors.New("GitHub returned no repository pull request count")
	}
	result.OpenCount = *data.Repository.OpenPullRequests.TotalCount
	connection := data.Repository.PullRequests
	if search {
		connection = data.Search
	}
	if connection == nil {
		return result, errors.New("GitHub returned no pull request page")
	}
	total := connection.TotalCount
	if search {
		total = connection.IssueCount
	}
	if total == nil || *total < 0 || result.OpenCount < 0 {
		return result, errors.New("GitHub returned invalid pull request page totals")
	}
	result.TotalCount = *total
	if search && result.TotalCount > 1000 {
		result.ResultLimit = 1000
	}
	info := connection.PageInfo
	if info.HasNextPage {
		if info.EndCursor == "" || info.EndCursor == request.Cursor || len(info.EndCursor) > maxPullRequestCursorLength {
			return result, errors.New("GitHub returned an invalid pull request pagination cursor")
		}
		result.NextCursor = info.EndCursor
	}
	result.HasNextPage = info.HasNextPage
	for _, node := range connection.Nodes {
		if node == nil {
			continue
		}
		if node.Number < 1 {
			return result, errors.New("GitHub returned an invalid pull request in the page")
		}
		result.Items = append(result.Items, node.summary())
	}
	return result, nil
}
