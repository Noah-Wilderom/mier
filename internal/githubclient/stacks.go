package githubclient

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type StackNode struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Head    string `json:"head"`
	Base    string `json:"base"`
	HeadSHA string `json:"headSha"`
	BaseSHA string `json:"baseSha"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Parents []int  `json:"parents"`
	Depth   int    `json:"depth"`
}
type PullRequestStack struct {
	Nodes    []StackNode `json:"nodes"`
	Warnings []string    `json:"warnings"`
}
type RetargetRequest struct {
	Base         string `json:"base"`
	ExpectedBase string `json:"expectedBase"`
	ExpectedHead string `json:"expectedHead"`
}

func buildPullRequestStack(current PullRequestSummary, candidates []PullRequestSummary, target string) PullRequestStack {
	result := PullRequestStack{Nodes: []StackNode{}, Warnings: []string{}}
	byID := map[int]PullRequestSummary{current.Number: current}
	for _, pr := range candidates {
		if pr.Number != current.Number {
			byID[pr.Number] = pr
		}
	}
	edges := map[int][]int{}
	neighbors := map[int][]int{}
	for id, child := range byID {
		baseRepo := child.Base.Repository
		if baseRepo == "" {
			baseRepo = target
		}
		parents := []PullRequestSummary{}
		for otherID, parent := range byID {
			if id != otherID && parent.Head.Ref == child.Base.Ref && parent.Head.Repository != "" && strings.EqualFold(parent.Head.Repository, baseRepo) {
				parents = append(parents, parent)
			}
		}
		// Reusing a branch for a later PR must not link a child to every historical PR.
		exact := []PullRequestSummary{}
		open := []PullRequestSummary{}
		for _, parent := range parents {
			if parent.Head.SHA != "" && parent.Head.SHA == child.Base.SHA {
				exact = append(exact, parent)
			}
			if parent.Status == "open" || parent.Status == "draft" {
				open = append(open, parent)
			}
		}
		if len(exact) > 0 {
			parents = exact
		} else if len(open) > 0 {
			parents = open
		} else if len(parents) > 1 {
			sort.Slice(parents, func(i, j int) bool { return parents[i].Number > parents[j].Number })
			parents = parents[:1]
		}
		for _, parent := range parents {
			edges[id] = append(edges[id], parent.Number)
			neighbors[id] = append(neighbors[id], parent.Number)
			neighbors[parent.Number] = append(neighbors[parent.Number], id)
		}
	}
	included := map[int]bool{}
	queue := []int{current.Number}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if included[id] {
			continue
		}
		included[id] = true
		queue = append(queue, neighbors[id]...)
	}
	var depth func(int) int
	cycle := false
	visiting := map[int]bool{}
	depths := map[int]int{}
	depth = func(id int) int {
		if value, ok := depths[id]; ok {
			return value
		}
		if visiting[id] {
			cycle = true
			return 0
		}
		visiting[id] = true
		d := 0
		for _, parent := range edges[id] {
			value := depth(parent) + 1
			if value > d {
				d = value
			}
		}
		visiting[id] = false
		depths[id] = d
		return d
	}
	for id := range included {
		pr := byID[id]
		parents := append([]int{}, edges[id]...)
		sort.Ints(parents)
		if len(parents) > 1 {
			result.Warnings = append(result.Warnings, "PR #"+strconv.Itoa(id)+" has multiple possible parents with the same repository and head branch")
		}
		result.Nodes = append(result.Nodes, StackNode{Number: id, Title: pr.Title, Head: pr.Head.Ref, Base: pr.Base.Ref, HeadSHA: pr.Head.SHA, BaseSHA: pr.Base.SHA, URL: pr.URL, Status: pr.Status, Parents: parents, Depth: depth(id)})
	}
	if cycle {
		result.Warnings = append(result.Warnings, "These branch relationships contain a cycle; review the PR base branches")
	}
	sort.Strings(result.Warnings)
	sort.Slice(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].Depth != result.Nodes[j].Depth {
			return result.Nodes[i].Depth < result.Nodes[j].Depth
		}
		return result.Nodes[i].Number < result.Nodes[j].Number
	})
	return result
}
func (c *Client) GetPullRequestStack(ctx context.Context, owner, repo string, number int) (PullRequestStack, error) {
	current, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return PullRequestStack{}, err
	}
	path, err := repoPath(owner, repo)
	if err != nil {
		return PullRequestStack{}, err
	}
	items := []PullRequestSummary{}
	truncated := false
	for page := 1; page <= 10; page++ {
		var batch []PullRequestSummary
		headers, err := c.request(ctx, http.MethodGet, path+"/pulls?state=all&sort=updated&direction=desc&per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if err != nil {
			return PullRequestStack{}, err
		}
		items = append(items, batch...)
		truncated = strings.Contains(headers.Get("Link"), `rel="next"`)
		if !truncated {
			break
		}
	}
	result := buildPullRequestStack(current, items, owner+"/"+repo)
	if truncated {
		result.Warnings = append(result.Warnings, "Relationships are based on the 1,000 most recently updated PRs; older related PRs may be missing")
	}
	return result, nil
}
func (c *Client) RetargetPullRequest(ctx context.Context, owner, repo string, number int, input RetargetRequest) (PullRequestSummary, error) {
	if strings.TrimSpace(input.Base) == "" || len(input.Base) > 1024 || strings.ContainsAny(input.Base, "\x00\r\n ") || input.ExpectedHead == "" || input.ExpectedBase == "" {
		return PullRequestSummary{}, errors.New("Choose a base branch and the currently reviewed head/base")
	}
	current, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return PullRequestSummary{}, err
	}
	if current.Head.SHA != input.ExpectedHead || current.Base.Ref != input.ExpectedBase {
		return PullRequestSummary{}, errors.New("This PR changed. Refresh before changing its base")
	}
	if current.Status != "open" && current.Status != "draft" {
		return PullRequestSummary{}, errors.New("Only an open pull request can change its base")
	}
	if current.Head.Ref == input.Base && strings.EqualFold(current.Head.Repository, owner+"/"+repo) {
		return PullRequestSummary{}, errors.New("The base must differ from the head branch")
	}
	path, err := prPath(owner, repo, number)
	if err != nil {
		return PullRequestSummary{}, err
	}
	var result PullRequestSummary
	_, err = c.request(ctx, http.MethodPatch, path, map[string]string{"base": input.Base}, &result)
	return result, err
}
