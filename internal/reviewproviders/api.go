// Package reviewproviders adapts native hosting APIs to M.I.E.R.'s review model.
package reviewproviders

import (
	"context"
	"mag-ik-een-review/internal/githubclient"
)

// API keeps GitHub's established service contract while allowing other native
// providers to participate in the same workspace and review lifecycle.
type API interface {
	CurrentUser(context.Context) (githubclient.User, error)
	ListPullRequests(context.Context, string, string, string) ([]githubclient.PullRequestSummary, error)
	ListPullRequestsPage(context.Context, string, string, githubclient.PullRequestListRequest) (githubclient.PullRequestPage, error)
	GetPullRequest(context.Context, string, string, int) (*githubclient.PullRequestDetail, error)
	GetCommitDiff(context.Context, string, string, string) (*githubclient.CommitDiff, error)
	CreatePullRequest(context.Context, string, string, githubclient.CreateRequest) (githubclient.PullRequestSummary, error)
	UpdatePullRequest(context.Context, string, string, int, githubclient.UpdateRequest) (githubclient.PullRequestSummary, error)
	SetDraft(context.Context, string, string, int, bool) error
	AddIssueComment(context.Context, string, string, int, string) (githubclient.Comment, error)
	AddReviewComment(context.Context, string, string, int, githubclient.ReviewCommentRequest) (githubclient.Comment, error)
	SubmitReview(context.Context, string, string, int, githubclient.ReviewRequest) (githubclient.Review, error)
	ReplyReviewComment(context.Context, string, string, int, int64, string) (githubclient.Comment, error)
	ResolveThread(context.Context, string, bool) error
	MergePullRequest(context.Context, string, string, int, string, string) (githubclient.MergeResult, error)
	RequestReviewers(context.Context, string, string, int, []string) (githubclient.PullRequestSummary, error)
	RemoveReviewers(context.Context, string, string, int, []string) error
	SetAssignees(context.Context, string, string, int, []string) error
	SetLabels(context.Context, string, string, int, []string) ([]string, error)
	GetPullRequestStack(context.Context, string, string, int) (githubclient.PullRequestStack, error)
	RetargetPullRequest(context.Context, string, string, int, githubclient.RetargetRequest) (githubclient.PullRequestSummary, error)
	ReviewChanges(context.Context, string, string, int, string, string) (githubclient.ReviewChanges, error)
	ReadFileAtCommit(context.Context, string, string, string, string) (string, error)
}

var _ API = (*githubclient.Client)(nil)

type Source struct{ HeadURL, HeadRef, HeadSHA, BaseURL, BaseRef string }
type SourceReader interface {
	PullRequestSource(context.Context, string, string, int) (Source, error)
}
