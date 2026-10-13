package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestListCountsPaginateGraphQLAndDoNotInventUnknownZeros(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			one, two := fixturePR(), fixturePR()
			two["number"] = 13
			respond(w, []any{one, two})
			return
		}
		var payload struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if !reflect.DeepEqual(payload.Variables["states"], []any{"OPEN", "CLOSED", "MERGED"}) {
			t.Error(payload.Variables)
		}
		number := 12
		info := map[string]any{"hasNextPage": true, "endCursor": "next"}
		if payload.Variables["cursor"] != nil {
			number = 13
			info = map[string]any{"hasNextPage": false}
		}
		respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": map[string]any{"nodes": []any{map[string]any{"number": number, "changedFiles": 7, "additions": 80, "deletions": 12, "comments": map[string]int{"totalCount": 4}, "reviewThreads": map[string]int{"totalCount": 2}, "commits": map[string]int{"totalCount": 3}}}, "pageInfo": info}}}})
	})
	items, err := client.ListPullRequests(context.Background(), "owner", "repo", "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if !item.CountsKnown || item.CommentsCount != 4 || item.ThreadsCount != 2 || item.CommitsCount != 3 || item.ChangedFiles != 7 {
			t.Fatal(item)
		}
	}
	unknown := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			respond(w, []any{fixturePR()})
			return
		}
		respond(w, map[string]any{"errors": []any{map[string]string{"message": "No permission"}}})
	})
	items, err = unknown.ListPullRequests(context.Background(), "owner", "repo", "all")
	if err != nil || len(items) != 1 || items[0].CountsKnown || len(items[0].Warnings) == 0 {
		t.Fatal(items, err)
	}
}

func TestClassicProtectionMetadataKeepsUnknownSeparate(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"baseRef": map[string]any{"branchProtectionRule": map[string]any{"requiresApprovingReviews": true, "requiredApprovingReviewCount": 2, "requiresStatusChecks": true, "requiredStatusCheckContexts": []string{"tests", "build"}}}}}}})
	})
	detail := &PullRequestDetail{}
	if err := client.loadProtection(context.Background(), "owner", "repo", 12, detail); err != nil {
		t.Fatal(err)
	}
	if detail.RequiredApprovals == nil || *detail.RequiredApprovals != 2 || !reflect.DeepEqual(detail.RequiredStatusCheckContexts, []string{"tests", "build"}) {
		t.Fatal(detail)
	}
	unknown := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"baseRef": nil}}}})
	})
	detail = &PullRequestDetail{}
	if err := unknown.loadProtection(context.Background(), "owner", "repo", 12, detail); err == nil || detail.RequiredApprovals != nil {
		t.Fatal("unknown protection invented", detail, err)
	}
}

func TestReviewerAssigneeLabelMetadataEndpoints(t *testing.T) {
	seen := map[string]map[string]any{}
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen[r.Method+" "+r.URL.Path] = body
		if r.URL.Path == "/repos/owner/repo/issues/12/labels" {
			respond(w, []any{map[string]string{"name": "bug"}})
		} else {
			respond(w, fixturePR())
		}
	})
	ctx := context.Background()
	if _, err := client.RequestReviewers(ctx, "owner", "repo", 12, []string{"reviewer"}); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveReviewers(ctx, "owner", "repo", 12, []string{"old-reviewer"}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetAssignees(ctx, "owner", "repo", 12, []string{"author"}); err != nil {
		t.Fatal(err)
	}
	if labels, err := client.SetLabels(ctx, "owner", "repo", 12, []string{"bug"}); err != nil || !reflect.DeepEqual(labels, []string{"bug"}) {
		t.Fatal(labels, err)
	}
	expected := map[string]map[string]any{"POST /repos/owner/repo/pulls/12/requested_reviewers": {"reviewers": []any{"reviewer"}}, "DELETE /repos/owner/repo/pulls/12/requested_reviewers": {"reviewers": []any{"old-reviewer"}}, "PATCH /repos/owner/repo/issues/12": {"assignees": []any{"author"}}, "PUT /repos/owner/repo/issues/12/labels": {"labels": []any{"bug"}}}
	if !reflect.DeepEqual(seen, expected) {
		t.Fatalf("wrong metadata endpoints: %#v", seen)
	}
}
