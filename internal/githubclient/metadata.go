package githubclient

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

func (c *Client) RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers []string) (PullRequestSummary, error) {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return PullRequestSummary{}, err
	}
	for _, login := range reviewers {
		if !repositoryPattern.MatchString(login) {
			return PullRequestSummary{}, errors.New("Reviewer logins must be valid GitHub usernames")
		}
	}
	if reviewers == nil {
		reviewers = []string{}
	}
	var result PullRequestSummary
	_, err = c.request(ctx, http.MethodPost, path+"/requested_reviewers", map[string]any{"reviewers": reviewers}, &result)
	return result, err
}

func (c *Client) RemoveReviewers(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	path, err := prPath(owner, repo, number)
	if err != nil {
		return err
	}
	for _, login := range reviewers {
		if !repositoryPattern.MatchString(login) {
			return errors.New("Reviewer logins must be valid GitHub usernames")
		}
	}
	if reviewers == nil {
		reviewers = []string{}
	}
	_, err = c.request(ctx, http.MethodDelete, path+"/requested_reviewers", map[string]any{"reviewers": reviewers}, nil)
	return err
}
func (c *Client) SetAssignees(ctx context.Context, owner, repo string, number int, assignees []string) error {
	path, err := repoPath(owner, repo)
	if err != nil {
		return err
	}
	if number < 1 {
		return errors.New("Pull request number must be positive")
	}
	for _, login := range assignees {
		if !repositoryPattern.MatchString(login) {
			return errors.New("Assignee logins must be valid GitHub usernames")
		}
	}
	if assignees == nil {
		assignees = []string{}
	}
	_, err = c.request(ctx, http.MethodPatch, path+"/issues/"+strconv.Itoa(number), map[string]any{"assignees": assignees}, nil)
	return err
}
func (c *Client) SetLabels(ctx context.Context, owner, repo string, number int, labels []string) ([]string, error) {
	path, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if number < 1 {
		return nil, errors.New("Pull request number must be positive")
	}
	if labels == nil {
		labels = []string{}
	}
	var result []struct {
		Name string `json:"name"`
	}
	_, err = c.request(ctx, http.MethodPut, path+"/issues/"+strconv.Itoa(number)+"/labels", map[string]any{"labels": labels}, &result)
	if err != nil {
		return nil, err
	}
	values := []string{}
	for _, label := range result {
		values = append(values, label.Name)
	}
	return values, nil
}

func (c *Client) loadProtection(ctx context.Context, owner, repo string, number int, detail *PullRequestDetail) error {
	query := `query($owner:String!,$repo:String!,$number:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$number){baseRef{branchProtectionRule{requiresApprovingReviews requiredApprovingReviewCount requiresStatusChecks requiredStatusCheckContexts}}}}}`
	var data struct {
		Repository *struct {
			PullRequest *struct {
				BaseRef *struct {
					Rule *struct {
						RequiresApprovingReviews     bool     `json:"requiresApprovingReviews"`
						RequiredApprovingReviewCount int      `json:"requiredApprovingReviewCount"`
						RequiresStatusChecks         bool     `json:"requiresStatusChecks"`
						RequiredStatusCheckContexts  []string `json:"requiredStatusCheckContexts"`
					} `json:"branchProtectionRule"`
				} `json:"baseRef"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := c.graph(ctx, query, map[string]any{"owner": owner, "repo": repo, "number": number}, &data); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil || data.Repository.PullRequest.BaseRef == nil || data.Repository.PullRequest.BaseRef.Rule == nil {
		return errors.New("Classic branch protection metadata is unavailable; GitHub merge policy remains authoritative (including rulesets)")
	}
	rule := data.Repository.PullRequest.BaseRef.Rule
	count := 0
	if rule.RequiresApprovingReviews {
		count = rule.RequiredApprovingReviewCount
	}
	detail.RequiredApprovals = &count
	if rule.RequiresStatusChecks {
		detail.RequiredStatusCheckContexts = append([]string{}, rule.RequiredStatusCheckContexts...)
	}
	return nil
}

func (c *Client) loadListCounts(ctx context.Context, owner, repo, state string, items []PullRequestSummary) error {
	states := []string{"OPEN"}
	if state == "closed" {
		states = []string{"CLOSED", "MERGED"}
	} else if state == "all" {
		states = []string{"OPEN", "CLOSED", "MERGED"}
	}
	indices := map[int]int{}
	for i := range items {
		indices[items[i].Number] = i
		items[i].CountsKnown = false
	}
	var cursor any
	query := `query($owner:String!,$repo:String!,$states:[PullRequestState!]!,$cursor:String){repository(owner:$owner,name:$repo){pullRequests(first:100,states:$states,after:$cursor,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{number additions deletions changedFiles comments{totalCount} reviewThreads{totalCount} commits{totalCount}}pageInfo{hasNextPage endCursor}}}}`
	for page := 0; page < 1000; page++ {
		var data struct {
			Repository *struct {
				PullRequests struct {
					Nodes []struct {
						Number       int `json:"number"`
						Additions    int `json:"additions"`
						Deletions    int `json:"deletions"`
						ChangedFiles int `json:"changedFiles"`
						Comments     struct {
							TotalCount int `json:"totalCount"`
						} `json:"comments"`
						Threads struct {
							TotalCount int `json:"totalCount"`
						} `json:"reviewThreads"`
						Commits struct {
							TotalCount int `json:"totalCount"`
						} `json:"commits"`
					} `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"pullRequests"`
			} `json:"repository"`
		}
		if err := c.graph(ctx, query, map[string]any{"owner": owner, "repo": repo, "states": states, "cursor": cursor}, &data); err != nil {
			return err
		}
		if data.Repository == nil {
			return errors.New("GitHub repository totals are inaccessible")
		}
		for _, node := range data.Repository.PullRequests.Nodes {
			if index, ok := indices[node.Number]; ok {
				item := &items[index]
				item.Additions = node.Additions
				item.Deletions = node.Deletions
				item.ChangedFiles = node.ChangedFiles
				item.CommentsCount = node.Comments.TotalCount
				item.ThreadsCount = node.Threads.TotalCount
				item.CommitsCount = node.Commits.TotalCount
				item.CountsKnown = true
			}
		}
		info := data.Repository.PullRequests.PageInfo
		if !info.HasNextPage {
			for i := range items {
				if !items[i].CountsKnown {
					items[i].Warnings = append(items[i].Warnings, "Pull request totals changed while refreshing; reload this pull request")
				}
			}
			return nil
		}
		if info.EndCursor == "" || info.EndCursor == cursor {
			return errors.New("GitHub returned an invalid pull request totals cursor")
		}
		cursor = info.EndCursor
	}
	return errors.New("GitHub pull request totals pagination exceeded 1000 pages")
}
