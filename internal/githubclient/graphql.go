package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type graphQueryError struct {
	Message    string `json:"message"`
	Type       string `json:"type"`
	Path       []any  `json:"path"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

type graphEnvelope struct {
	Data   json.RawMessage   `json:"data"`
	Errors []graphQueryError `json:"errors"`
}

func (c *Client) graphResponse(ctx context.Context, query string, variables map[string]any) (graphEnvelope, error) {
	var envelope graphEnvelope
	_, err := c.request(ctx, http.MethodPost, "@graphql", map[string]any{"query": query, "variables": variables}, &envelope)
	return envelope, err
}

func (c *Client) graphFailure(failure graphQueryError) error {
	message := failure.Message
	if c.token != "" {
		message = strings.ReplaceAll(message, c.token, "[redacted]")
	}
	kind := strings.ToUpper(failure.Type + " " + failure.Extensions.Code)
	lower := strings.ToLower(message)
	if strings.Contains(kind, "UNDEFINEDFIELD") || strings.Contains(kind, "GRAPHQL_VALIDATION_FAILED") || strings.Contains(lower, "doesn't exist on type") || strings.Contains(lower, "cannot query field") {
		return fmt.Errorf("GitHub GraphQL schema validation: %s. The application query is incompatible with this GitHub schema", message)
	}
	if strings.Contains(kind, "FORBIDDEN") || strings.Contains(kind, "NOT_FOUND") || strings.Contains(lower, "not accessible") || strings.Contains(lower, "permission") {
		return fmt.Errorf("GitHub GraphQL: %s. Verify repository access and Pull requests permission", message)
	}
	return fmt.Errorf("GitHub GraphQL: %s", message)
}

func (c *Client) graph(ctx context.Context, query string, variables map[string]any, output any) error {
	envelope, err := c.graphResponse(ctx, query, variables)
	if err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return c.graphFailure(envelope.Errors[0])
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("GitHub GraphQL returned no data")
	}
	if output != nil {
		return json.Unmarshal(envelope.Data, output)
	}
	return nil
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// GitHub's BigInt scalar may be a quoted decimal or a JSON number.
// Parse its decimal representation directly so IDs never pass through float64.
type graphDatabaseID int64

func (id *graphDatabaseID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = 0
		return nil
	}
	value := string(data)
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("Invalid GitHub review comment identifier: %w", err)
	}
	*id = graphDatabaseID(parsed)
	return nil
}

type graphComment struct {
	ID             string          `json:"id"`
	FullDatabaseID graphDatabaseID `json:"fullDatabaseId"`
	Body           string          `json:"body"`
	Author         User            `json:"author"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	URL            string          `json:"url"`
	Path           string          `json:"path"`
	Line           *int            `json:"line"`
	OriginalLine   *int            `json:"originalLine"`
	Commit         struct {
		OID string `json:"oid"`
	} `json:"commit"`
	ReplyTo *struct {
		FullDatabaseID graphDatabaseID `json:"fullDatabaseId"`
	} `json:"replyTo"`
}

func (g graphComment) model(side string) Comment {
	c := Comment{ID: int64(g.FullDatabaseID), NodeID: g.ID, Body: g.Body, User: g.Author, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt, URL: g.URL, Path: g.Path, Line: g.Line, OriginalLine: g.OriginalLine, Side: side, CommitID: g.Commit.OID}
	if g.ReplyTo != nil {
		c.InReplyToID = int64(g.ReplyTo.FullDatabaseID)
	}
	return c
}

type graphComments struct {
	Nodes    []graphComment `json:"nodes"`
	PageInfo pageInfo       `json:"pageInfo"`
}
type graphThread struct {
	ID                 string        `json:"id"`
	Path               string        `json:"path"`
	Line               *int          `json:"line"`
	OriginalLine       *int          `json:"originalLine"`
	DiffSide           string        `json:"diffSide"`
	IsResolved         bool          `json:"isResolved"`
	IsOutdated         bool          `json:"isOutdated"`
	ViewerCanResolve   bool          `json:"viewerCanResolve"`
	ViewerCanUnresolve bool          `json:"viewerCanUnresolve"`
	Comments           graphComments `json:"comments"`
}

const commentFields = `id fullDatabaseId body createdAt updatedAt url author { login avatarUrl url } path line originalLine commit { oid } replyTo { fullDatabaseId }`
const threadsQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){repository(owner:$owner,name:$repo){pullRequest(number:$number){mergeStateStatus reviewDecision viewerCanUpdate reviewThreads(first:100,after:$cursor){nodes{id path line originalLine diffSide isResolved isOutdated viewerCanResolve viewerCanUnresolve comments(first:100){nodes{` + commentFields + `}pageInfo{hasNextPage endCursor}}}pageInfo{hasNextPage endCursor}}}}}`

func (c *Client) loadThreads(ctx context.Context, owner, repo string, number int, detail *PullRequestDetail) error {
	var cursor any
	for page := 0; page < 1000; page++ {
		var data struct {
			Repository *struct {
				PullRequest *struct {
					MergeStateStatus string `json:"mergeStateStatus"`
					ReviewDecision   string `json:"reviewDecision"`
					ViewerCanUpdate  bool   `json:"viewerCanUpdate"`
					ReviewThreads    struct {
						Nodes    []graphThread `json:"nodes"`
						PageInfo pageInfo      `json:"pageInfo"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		if err := c.graph(ctx, threadsQuery, map[string]any{"owner": owner, "repo": repo, "number": number, "cursor": cursor}, &data); err != nil {
			return err
		}
		if data.Repository == nil || data.Repository.PullRequest == nil {
			return errors.New("GitHub repository or pull request is inaccessible")
		}
		pr := data.Repository.PullRequest
		detail.MergeStateStatus = pr.MergeStateStatus
		detail.ReviewDecision = pr.ReviewDecision
		detail.ViewerCanUpdate = pr.ViewerCanUpdate
		for _, wire := range pr.ReviewThreads.Nodes {
			thread := Thread{ID: wire.ID, Path: wire.Path, Line: wire.Line, OriginalLine: wire.OriginalLine, DiffSide: wire.DiffSide, Resolved: wire.IsResolved, Outdated: wire.IsOutdated, ViewerCanResolve: wire.ViewerCanResolve, ViewerCanUnresolve: wire.ViewerCanUnresolve, Comments: []Comment{}}
			for _, comment := range wire.Comments.Nodes {
				thread.Comments = append(thread.Comments, comment.model(thread.DiffSide))
			}
			comments := wire.Comments
			for commentPage := 0; comments.PageInfo.HasNextPage; commentPage++ {
				if commentPage >= 1000 {
					return errors.New("GitHub thread pagination exceeded 1000 pages")
				}
				var more struct {
					Node *struct {
						Comments graphComments `json:"comments"`
					} `json:"node"`
				}
				query := `query($id:ID!,$cursor:String!){node(id:$id){... on PullRequestReviewThread{comments(first:100,after:$cursor){nodes{` + commentFields + `}pageInfo{hasNextPage endCursor}}}}}`
				if comments.PageInfo.EndCursor == "" {
					return errors.New("GitHub returned an empty comments pagination cursor")
				}
				if err := c.graph(ctx, query, map[string]any{"id": wire.ID, "cursor": comments.PageInfo.EndCursor}, &more); err != nil {
					return err
				}
				if more.Node == nil {
					return errors.New("Review thread became inaccessible during loading")
				}
				comments = more.Node.Comments
				for _, comment := range comments.Nodes {
					thread.Comments = append(thread.Comments, comment.model(thread.DiffSide))
				}
			}
			detail.Threads = append(detail.Threads, thread)
		}
		if !pr.ReviewThreads.PageInfo.HasNextPage {
			return nil
		}
		if pr.ReviewThreads.PageInfo.EndCursor == "" || pr.ReviewThreads.PageInfo.EndCursor == cursor {
			return errors.New("GitHub returned an invalid review thread pagination cursor")
		}
		cursor = pr.ReviewThreads.PageInfo.EndCursor
	}
	return errors.New("GitHub review thread pagination exceeded 1000 pages")
}

func (c *Client) SetDraft(ctx context.Context, owner, repo string, number int, draft bool) error {
	pr, err := c.getPR(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	if pr.NodeID == "" {
		return errors.New("GitHub did not provide a pull request node ID")
	}
	name, inputType := "markPullRequestReadyForReview", "MarkPullRequestReadyForReviewInput"
	if draft {
		name, inputType = "convertPullRequestToDraft", "ConvertPullRequestToDraftInput"
	}
	query := fmt.Sprintf(`mutation($input:%s!){%s(input:$input){pullRequest{id isDraft}}}`, inputType, name)
	if err := c.graph(ctx, query, map[string]any{"input": map[string]any{"pullRequestId": pr.NodeID}}, nil); err != nil {
		return err
	}
	c.invalidate()
	return nil
}
func (c *Client) ResolveThread(ctx context.Context, threadID string, resolved bool) error {
	if strings.TrimSpace(threadID) == "" {
		return errors.New("Choose a review thread")
	}
	name, inputType := "unresolveReviewThread", "UnresolveReviewThreadInput"
	if resolved {
		name, inputType = "resolveReviewThread", "ResolveReviewThreadInput"
	}
	query := fmt.Sprintf(`mutation($input:%s!){%s(input:$input){thread{id isResolved}}}`, inputType, name)
	if err := c.graph(ctx, query, map[string]any{"input": map[string]any{"threadId": threadID}}, nil); err != nil {
		return err
	}
	c.invalidate()
	return nil
}
