package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func graphListFixture() map[string]any {
	return map[string]any{
		"id": "PR_node", "number": 12, "title": "Review me", "body": "Description", "url": "https://github.com/owner/repo/pull/12",
		"state": "OPEN", "isDraft": false, "merged": false, "createdAt": "2026-10-01T00:00:00Z", "updatedAt": "2026-10-13T00:00:00Z",
		"author":      map[string]any{"login": "author", "avatarUrl": "https://avatar.test/author", "url": "https://github.com/author"},
		"headRefName": "feature", "headRefOid": "head-sha", "headRepository": map[string]string{"nameWithOwner": "fork/repo"},
		"baseRefName": "main", "baseRefOid": "base-sha", "repository": map[string]string{"nameWithOwner": "owner/repo"},
		"reviewRequests": map[string]any{"nodes": []any{map[string]any{"requestedReviewer": map[string]string{"login": "reviewer"}}}},
		"assignees":      map[string]any{"nodes": []map[string]string{{"login": "assignee"}}},
		"labels":         map[string]any{"nodes": []map[string]string{{"name": "bug"}}},
		"mergeable":      "MERGEABLE", "mergeStateStatus": "CLEAN", "additions": 8, "deletions": 2, "changedFiles": 3,
		"commits": map[string]int{"totalCount": 4}, "comments": map[string]int{"totalCount": 5}, "reviewThreads": map[string]int{"totalCount": 6},
	}
}

func pageRepository(connection map[string]any) map[string]any {
	return map[string]any{"pullRequests": connection, "openPullRequests": map[string]int{"totalCount": 75}}
}

func TestPullRequestPageStopsAfterOneRequestAndPreservesRowMetadata(t *testing.T) {
	var calls atomic.Int32
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Fatalf("page should need only GraphQL, got %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Variables["pageSize"] != float64(25) || request.Variables["cursor"] != nil || !reflect.DeepEqual(request.Variables["states"], []any{"OPEN", "CLOSED", "MERGED"}) {
			t.Fatalf("wrong initial page variables: %+v", request.Variables)
		}
		if !strings.Contains(request.Query, "totalCount") || strings.Contains(request.Query, "search(") {
			t.Fatal(request.Query)
		}
		connection := map[string]any{"nodes": []any{graphListFixture()}, "totalCount": 1772, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "next-page"}}
		respond(w, map[string]any{"data": map[string]any{"repository": pageRepository(connection)}})
	})
	page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{State: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(page.Items) != 1 || page.TotalCount != 1772 || page.OpenCount != 75 || !page.HasNextPage || page.NextCursor != "next-page" || page.ResultLimit != 0 {
		t.Fatalf("wrong page: %+v, calls %d", page, calls.Load())
	}
	pr := page.Items[0]
	if pr.NodeID != "PR_node" || pr.Author.Login != "author" || pr.Author.AvatarURL != "https://avatar.test/author" || pr.Head.SHA != "head-sha" || pr.Head.Ref != "feature" || pr.Head.Repository != "fork/repo" || pr.Base.Repository != "owner/repo" || pr.Base.SHA != "base-sha" || pr.Status != "open" || pr.Mergeable == nil || !*pr.Mergeable || pr.MergeableState != "clean" {
		t.Fatalf("wrong PR metadata: %+v", pr)
	}
	if !reflect.DeepEqual(pr.Labels, []string{"bug"}) || len(pr.Reviewers) != 1 || pr.Reviewers[0].Login != "reviewer" || len(pr.Assignees) != 1 || pr.Assignees[0].Login != "assignee" || !pr.CountsKnown || pr.CommitsCount != 4 || pr.CommentsCount != 5 || pr.ThreadsCount != 6 || pr.ChangedFiles != 3 || pr.Additions != 8 || pr.Deletions != 2 {
		t.Fatalf("wrong page enrichment: %+v", pr)
	}
}

func TestPullRequestPageForwardsCursorAndUsesDistinctStates(t *testing.T) {
	for _, state := range []string{"open", "merged", "closed"} {
		t.Run(state, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Variables map[string]any `json:"variables"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				if request.Variables["cursor"] != "previous-page" || request.Variables["pageSize"] != float64(100) || !reflect.DeepEqual(request.Variables["states"], []any{strings.ToUpper(state)}) {
					t.Fatalf("wrong variables %+v", request.Variables)
				}
				node := graphListFixture()
				node["state"] = strings.ToUpper(state)
				node["isDraft"] = true
				node["merged"] = state == "merged"
				respond(w, map[string]any{"data": map[string]any{"repository": pageRepository(map[string]any{"nodes": []any{node}, "totalCount": 1, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": "last-page"}})}})
			})
			page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{State: state, Cursor: "previous-page", PageSize: 100})
			if err != nil {
				t.Fatal(err)
			}
			want := state
			if state == "open" {
				want = "draft"
			}
			if page.Items[0].Status != want || page.HasNextPage || page.NextCursor != "" {
				t.Fatalf("wrong state or terminal page: %+v", page)
			}
		})
	}
}

func TestPullRequestPageSearchKeepsFiltersAndTextInRepositoryScope(t *testing.T) {
	for view, qualifier := range map[string]string{"inbox": "", "mine": "author:@me", "review-requested": "review-requested:@me", "assigned": "assignee:@me", "drafts": "is:draft"} {
		t.Run(view, func(t *testing.T) {
			var calls atomic.Int32
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				search, _ := request.Variables["query"].(string)
				if !strings.Contains(request.Query, "search(") || !strings.HasPrefix(search, "repo:owner/repo is:pr is:closed is:unmerged ") || !strings.Contains(search, qualifier) || !strings.HasSuffix(search, `"fix OR repo:other/repo \"escape\""`) || request.Variables["cursor"] != "search-cursor" {
					t.Fatalf("unsafe or incomplete search: %q %+v", search, request.Variables)
				}
				respond(w, map[string]any{"data": map[string]any{"repository": pageRepository(nil), "search": map[string]any{"nodes": []any{graphListFixture()}, "issueCount": 1772, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "more-search"}}}})
			})
			page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{State: "closed", View: view, Query: `fix OR repo:other/repo "escape"`, Cursor: "search-cursor"})
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || len(page.Items) != 1 || page.TotalCount != 1772 || page.ResultLimit != 1000 || page.OpenCount != 75 || page.NextCursor != "more-search" {
				t.Fatalf("wrong search page: %+v", page)
			}
		})
	}
}

func TestPullRequestPageRejectsInvalidInputWithoutNetwork(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached GitHub") })
	for _, request := range []PullRequestListRequest{{State: "invalid"}, {View: "invalid"}, {PageSize: -1}, {PageSize: 101}, {Cursor: strings.Repeat("x", 2049)}} {
		if _, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", request); err == nil {
			t.Errorf("accepted invalid request %+v", request)
		}
	}
	if _, err := client.ListPullRequestsPage(context.Background(), "../owner", "repo", PullRequestListRequest{}); err == nil {
		t.Error("accepted invalid repository")
	}
}

func TestPullRequestPageRejectsMissingRepositoryAndBrokenCursor(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository any
	}{
		{"missing repository", nil},
		{"missing cursor", pageRepository(map[string]any{"nodes": []any{}, "totalCount": 2, "pageInfo": map[string]any{"hasNextPage": true}})},
		{"unchanged cursor", pageRepository(map[string]any{"nodes": []any{}, "totalCount": 2, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "same"}})},
		{"oversized cursor", pageRepository(map[string]any{"nodes": []any{}, "totalCount": 2, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": strings.Repeat("x", 2049)}})},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				respond(w, map[string]any{"data": map[string]any{"repository": test.repository}})
			})
			if _, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{Cursor: "same"}); err == nil {
				t.Error("accepted malformed page")
			}
		})
	}
}

func TestPullRequestPageMissingCountsRemainUnknown(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		node := graphListFixture()
		delete(node, "comments")
		respond(w, map[string]any{"data": map[string]any{"repository": pageRepository(map[string]any{"nodes": []any{node}, "totalCount": 1, "pageInfo": map[string]any{"hasNextPage": false}})}})
	})
	page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].CountsKnown || len(page.Items[0].Warnings) == 0 {
		t.Fatalf("missing count presented as a known zero: %+v", page.Items[0])
	}
}

func TestPullRequestPageNumericSearchLoadsExactPullRequestInOneRequest(t *testing.T) {
	for _, query := range []string{"12", "#12"} {
		t.Run(query, func(t *testing.T) {
			var calls atomic.Int32
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				if !strings.Contains(request.Query, "pullRequest(number:$number)") || strings.Contains(request.Query, "search(") || request.Variables["number"] != float64(12) {
					t.Fatalf("number lookup should load exactly one PR: %+v", request)
				}
				respond(w, map[string]any{"data": map[string]any{"viewer": map[string]string{"login": "author"}, "repository": map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": graphListFixture()}}})
			})
			page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{State: "all", Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || len(page.Items) != 1 || page.Items[0].Number != 12 || page.TotalCount != 1 || page.OpenCount != 75 || page.HasNextPage || page.NextCursor != "" || page.ResultLimit != 0 {
				t.Fatalf("wrong number result %+v", page)
			}
		})
	}
}

func TestPullRequestPageNumericSearchHonorsStateAndPersonalViews(t *testing.T) {
	for _, test := range []struct {
		name, state, view, viewer string
		want                      int
	}{
		{"wrong state", "merged", "inbox", "author", 0},
		{"mine", "open", "mine", "author", 1},
		{"not mine", "open", "mine", "someone-else", 0},
		{"assigned", "open", "assigned", "assignee", 1},
		{"not assigned", "open", "assigned", "someone-else", 0},
		{"requested", "open", "review-requested", "reviewer", 1},
		{"not requested", "open", "review-requested", "someone-else", 0},
		{"not draft", "open", "drafts", "author", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				respond(w, map[string]any{"data": map[string]any{"viewer": map[string]string{"login": test.viewer}, "repository": map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": graphListFixture()}}})
			})
			page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{State: test.state, View: test.view, Query: "#12"})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != test.want || page.TotalCount != test.want {
				t.Fatalf("numeric search escaped state/view: %+v", page)
			}
		})
	}
}

func TestPullRequestPageNumericSearchHonorsViewerReviewRequest(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		node := graphListFixture()
		node["viewerLatestReviewRequest"] = map[string]string{"id": "team-request-for-viewer"}
		respond(w, map[string]any{"data": map[string]any{"viewer": map[string]string{"login": "team-member"}, "repository": map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": node}}})
	})
	page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{View: "review-requested", Query: "#12"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("viewer request omitted from numeric results: %+v", page)
	}
}

func TestPullRequestPageNumericSearchMissingNumberIsEmptyWithAccessibleRepository(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": nil}}, "errors": []any{map[string]any{"type": "NOT_FOUND", "path": []string{"repository", "pullRequest"}, "message": "Could not resolve pull request"}}})
	})
	page, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{Query: "#999"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.TotalCount != 0 || page.OpenCount != 75 {
		t.Fatalf("missing number not an empty result: %+v", page)
	}
}

func TestPullRequestPageNumericSearchDoesNotHideOtherGraphQLErrors(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		path       []string
		repository any
	}{
		{"missing repository", "NOT_FOUND", []string{"repository"}, nil},
		{"forbidden number", "FORBIDDEN", []string{"repository", "pullRequest"}, map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": nil}},
		{"missing count", "NOT_FOUND", []string{"repository", "pullRequest"}, map[string]any{"pullRequest": nil}},
		{"unknown path", "NOT_FOUND", nil, map[string]any{"openPullRequests": map[string]int{"totalCount": 75}, "pullRequest": nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				respond(w, map[string]any{"data": map[string]any{"repository": test.repository}, "errors": []any{map[string]any{"type": test.kind, "path": test.path, "message": "test-secret inaccessible"}}})
			})
			_, err := client.ListPullRequestsPage(context.Background(), "owner", "repo", PullRequestListRequest{Query: "#12"})
			if err == nil || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("provider error hidden or secret leaked: %v", err)
			}
		})
	}
}
