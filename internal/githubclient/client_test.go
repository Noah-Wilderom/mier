package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New("github.com", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	client.base = server.URL
	client.graphql = server.URL + "/graphql"
	return client
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func fixturePR() map[string]any {
	return map[string]any{"number": 12, "node_id": "PR_node", "title": "Review me", "body": "Description", "state": "open", "draft": false, "user": map[string]any{"login": "author", "avatar_url": "https://avatar.test/author", "html_url": "https://github.com/author"}, "head": map[string]any{"sha": "head-sha", "ref": "feature", "repo": map[string]any{"full_name": "owner/repo"}}, "base": map[string]any{"ref": "main", "sha": "base-sha"}, "labels": []map[string]string{{"name": "bug"}}, "requested_reviewers": []map[string]string{{"login": "reviewer"}}, "changed_files": 1, "commits": 1, "comments": 2, "review_comments": 1}
}

func TestHostNormalization(t *testing.T) {
	for input, want := range map[string]string{"": "github.com", "HTTPS://GHE.Example.test/api/v3/": "ghe.example.test", "https://api.github.com": "github.com", "ghe.example.test:8443": "ghe.example.test:8443"} {
		got, err := NormalizeHost(input)
		if err != nil || got != want {
			t.Fatalf("%q: got %q %v", input, got, err)
		}
	}
	for _, input := range []string{"http://github.com", "https://token@github.com", "https://github.com/org/repo", "-flag", "github.com?token=secret", "https://github.com/#secret"} {
		if _, err := NormalizeHost(input); err == nil {
			t.Errorf("accepted unsafe host %q", input)
		}
	}
}

func TestPublicServerAndEnterpriseCloudEndpoints(t *testing.T) {
	for _, test := range []struct{ host, normalized, rest, graph string }{
		{"github.com", "github.com", "https://api.github.com", "https://api.github.com/graphql"},
		{"https://ghe.example.test/api/v3/", "ghe.example.test", "https://ghe.example.test/api/v3", "https://ghe.example.test/api/graphql"},
		{"https://api.octocorp.ghe.com/graphql", "octocorp.ghe.com", "https://api.octocorp.ghe.com", "https://api.octocorp.ghe.com/graphql"},
	} {
		client, err := New(test.host, "token")
		if err != nil || client.host != test.normalized || client.base != test.rest || client.graphql != test.graph {
			t.Fatalf("wrong endpoints for %s: %+v %v", test.host, client, err)
		}
	}
}

func TestPaginatedListsAndConditionalCache(t *testing.T) {
	var calls atomic.Int32
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		page := r.URL.Query().Get("page")
		if page == "1" {
			w.Header().Set("Link", `<ignored>; rel="next"`)
		}
		w.Header().Set("ETag", `"page-`+page+`"`)
		if r.Header.Get("If-None-Match") != "" {
			w.Header().Del("Link")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		pr := fixturePR()
		if page == "2" {
			pr["number"] = 13
			pr["draft"] = true
			pr["state"] = "closed"
		}
		respond(w, []any{pr})
	})
	for i := 0; i < 2; i++ {
		prs, err := list[PullRequestSummary](context.Background(), client, "/repos/owner/repo/pulls?state=all")
		if err != nil {
			t.Fatal(err)
		}
		if len(prs) != 2 || prs[1].Status != "closed" || prs[0].Author.AvatarURL != "https://avatar.test/author" || prs[0].Head.Repository != "owner/repo" || prs[0].Labels[0] != "bug" {
			t.Fatalf("bad mapping: %+v", prs)
		}
	}
	if calls.Load() != 4 {
		t.Fatal(calls.Load())
	}
}

func TestErrorsRedactSecretsAndBackOff(t *testing.T) {
	var calls atomic.Int32
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(403)
		respond(w, map[string]string{"message": "rate limit test-secret"})
	})
	_, err := client.CurrentUser(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.RetryAt == "" || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("unsafe error %v", err)
	}
	_, err = client.CurrentUser(context.Background())
	if !errors.As(err, &api) || api.Status != 429 || calls.Load() != 1 {
		t.Fatalf("no backoff: %v calls %d", err, calls.Load())
	}
}

func TestCancellationAndRedirectDoNotLeakCredentials(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") == "yes" {
			<-r.Context().Done()
			return
		}
		http.Redirect(w, r, target.URL, 302)
	})
	_, err := client.CurrentUser(context.Background())
	if err == nil || redirected.Load() != 0 {
		t.Fatal("followed redirect", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.request(ctx, "GET", "/user?wait=yes", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestCompleteDetailUsesCurrentHeadAndPaginatesThreads(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/12":
			respond(w, fixturePR())
		case "/repos/owner/repo/pulls/12/files":
			respond(w, []any{map[string]any{"filename": "src/file.go", "status": "modified", "patch": "@@ -1 +1 @@\n-old\n+new", "additions": 1, "deletions": 1}})
		case "/repos/owner/repo/pulls/12/commits":
			respond(w, []any{map[string]any{"sha": "head-sha", "author": map[string]string{"login": "author"}, "commit": map[string]any{"message": "Subject\n\nBody", "author": map[string]string{"name": "Author", "date": "2026-10-13T01:00:00Z"}}}})
		case "/repos/owner/repo/issues/12/comments":
			respond(w, []any{map[string]any{"id": 123, "node_id": "IC_node", "body": "Conversation", "user": map[string]string{"login": "reader"}}})
		case "/repos/owner/repo/pulls/12/reviews":
			respond(w, []any{map[string]any{"id": 321, "state": "APPROVED", "commit_id": "head-sha", "user": map[string]string{"login": "reviewer"}}})
		case "/repos/owner/repo/commits/head-sha/check-runs":
			respond(w, map[string]any{"check_runs": []any{map[string]any{"id": 1, "name": "tests", "status": "completed", "conclusion": "failure"}}})
		case "/repos/owner/repo/commits/head-sha/statuses":
			respond(w, []any{map[string]any{"id": 2, "context": "build", "state": "success"}})
		case "/repos/owner/repo":
			respond(w, map[string]bool{"allow_merge_commit": true, "allow_squash_merge": true})
		case "/graphql":
			var request struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			comment := map[string]any{"id": "RC_node", "fullDatabaseId": "777", "body": "Inline", "author": map[string]string{"login": "reviewer"}, "path": "src/file.go", "line": 1, "commit": map[string]string{"oid": "head-sha"}}
			if strings.Contains(request.Query, "node(id:") {
				respond(w, map[string]any{"data": map[string]any{"node": map[string]any{"comments": map[string]any{"nodes": []any{comment}, "pageInfo": map[string]any{"hasNextPage": false}}}}})
				return
			}
			thread := map[string]any{"id": "T_first", "path": "src/file.go", "line": 1, "diffSide": "RIGHT", "isResolved": false, "viewerCanResolve": true, "comments": map[string]any{"nodes": []any{comment}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "C_next"}}}
			pageInfo := map[string]any{"hasNextPage": true, "endCursor": "T_next"}
			if request.Variables["cursor"] != nil {
				thread["id"] = "T_second"
				thread["comments"] = map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}
				pageInfo = map[string]any{"hasNextPage": false}
			}
			respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"mergeStateStatus": "BLOCKED", "reviewDecision": "REVIEW_REQUIRED", "viewerCanUpdate": true, "reviewThreads": map[string]any{"nodes": []any{thread}, "pageInfo": pageInfo}}}}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	detail, err := client.GetPullRequest(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatal(err)
	}
	if detail.PullRequest.Head.SHA != "head-sha" || detail.Files[0].Path != "src/file.go" || detail.Commits[0].Subject != "Subject" || detail.Reviews[0].CommitID != "head-sha" || detail.Checks[0].Conclusion != "failure" || detail.MergeStateStatus != "BLOCKED" || len(detail.Threads) != 2 || len(detail.Threads[0].Comments) != 2 || detail.Threads[0].Comments[0].ID != 777 {
		t.Fatalf("wrong full detail: %+v", detail)
	}
}

func TestMergedPullRequestGraphQLCommentSchemaAndThreadSides(t *testing.T) {
	// GitHub places diffSide on PullRequestReviewThread, not PullRequestReviewComment.
	commentSelection := regexp.MustCompile(`(?s)comments\([^)]*\)\s*\{\s*nodes\s*\{(.*?)\}\s*pageInfo`)
	// Snapshot of actual __type(PullRequestReviewComment) field names. Deprecated
	// databaseId is absent from the live schema, even though old docs still list it.
	allowedFields := map[string]bool{}
	for _, field := range strings.Fields(`author authorAssociation body bodyHTML bodyText commit createdAt createdViaEmail diffHunk draftedAt editor fullDatabaseId id includesCreatedEdit isMinimized lastEditedAt line minimizedReason originalCommit originalLine originalStartLine outdated path publishedAt pullRequest pullRequestReview reactions replyTo repository resourcePath startLine state subjectType updatedAt url viewerCanDelete viewerCanUpdate`) {
		allowedFields[field] = true
	}
	tokens := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*|[{}]`)
	removedID := regexp.MustCompile(`\bdatabaseId\b`)
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			var payload struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			for _, selection := range commentSelection.FindAllStringSubmatch(payload.Query, -1) {
				invalidField := ""
				depth := 0
				for _, token := range tokens.FindAllString(selection[1], -1) {
					switch token {
					case "{":
						depth++
					case "}":
						depth--
					default:
						if depth == 0 && !allowedFields[token] {
							invalidField = token
						}
					}
				}
				if removedID.MatchString(selection[1]) {
					invalidField = "databaseId"
				}
				if invalidField != "" {
					respond(w, map[string]any{"errors": []any{map[string]any{"message": "Field '" + invalidField + "' doesn't exist on type 'PullRequestReviewComment'", "extensions": map[string]string{"code": "undefinedField"}}}})
					return
				}
			}
			comment := map[string]any{"id": "comment_node", "fullDatabaseId": "9007199254740993", "body": "Feedback on removed code", "path": "src/file.go", "line": nil, "originalLine": 4, "author": map[string]string{"login": "reviewer"}, "replyTo": map[string]any{"fullDatabaseId": "9007199254740992"}}
			if strings.Contains(payload.Query, "node(id:") {
				comment["fullDatabaseId"] = json.Number("9223372036854775806")
				respond(w, map[string]any{"data": map[string]any{"node": map[string]any{"comments": map[string]any{"nodes": []any{comment}, "pageInfo": map[string]any{"hasNextPage": false}}}}})
				return
			}
			if strings.Contains(payload.Query, "branchProtectionRule") {
				respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"baseRef": nil}}}})
				return
			}
			thread := map[string]any{"id": "thread_node", "path": "src/file.go", "line": nil, "originalLine": 4, "diffSide": "LEFT", "isResolved": true, "isOutdated": true, "comments": map[string]any{"nodes": []any{comment}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "next_comment"}}}
			respond(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"mergeStateStatus": "UNKNOWN", "reviewThreads": map[string]any{"nodes": []any{thread}, "pageInfo": map[string]any{"hasNextPage": false}}}}}})
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/12":
			pr := fixturePR()
			pr["state"] = "closed"
			pr["merged"] = true
			pr["changed_files"] = 0
			respond(w, pr)
		case "/repos/owner/repo/commits/head-sha/check-runs":
			respond(w, map[string]any{"check_runs": []any{}})
		case "/repos/owner/repo":
			respond(w, map[string]bool{"allow_merge_commit": true})
		default:
			respond(w, []any{})
		}
	})
	detail, err := client.GetPullRequest(context.Background(), "owner", "repo", 12)
	if err != nil {
		t.Fatalf("merged PR changes query was rejected by the review-comment schema: %v", err)
	}
	if detail.PullRequest.Status != "merged" || len(detail.Threads) != 1 || len(detail.Threads[0].Comments) != 2 {
		t.Fatalf("missing merged review thread data: %+v", detail)
	}
	for index, comment := range detail.Threads[0].Comments {
		if comment.Side != "LEFT" || comment.ID != []int64{9007199254740993, 9223372036854775806}[index] || comment.InReplyToID != 9007199254740992 || comment.Line != nil || comment.OriginalLine == nil || *comment.OriginalLine != 4 {
			t.Fatalf("wrong original/paginated review comment location: %+v", comment)
		}
	}
}

func TestGraphQLBigIntIDsRejectMalformedOrOverflowValues(t *testing.T) {
	for _, value := range []string{`"not-an-id"`, `9223372036854775808`, `1.2`, `1e3`} {
		var id graphDatabaseID
		if err := json.Unmarshal([]byte(value), &id); err == nil {
			t.Errorf("accepted invalid identifier %s", value)
		}
	}
	var id graphDatabaseID
	if err := json.Unmarshal([]byte(`null`), &id); err != nil || id != 0 {
		t.Fatal(id, err)
	}
}

func TestGraphQLValidationErrorDoesNotSuggestChangingPermissions(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"errors": []any{map[string]any{"message": "Field 'diffSide' doesn't exist on type 'PullRequestReviewComment'", "extensions": map[string]string{"code": "undefinedField"}}}})
	})
	err := client.ResolveThread(context.Background(), "thread_node", true)
	if err == nil || strings.Contains(err.Error(), "Verify repository access") || strings.Contains(err.Error(), "permission") {
		t.Fatalf("schema error misclassified as authorization: %v", err)
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("schema failure has no actionable classification: %v", err)
	}
}

func TestGraphQLForbiddenErrorPreservesPermissionGuidance(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"errors": []any{map[string]any{"message": "Resource not accessible by integration", "type": "FORBIDDEN"}}})
	})
	if err := client.ResolveThread(context.Background(), "thread_node", true); err == nil || !strings.Contains(err.Error(), "Verify repository access") {
		t.Fatalf("permission error lost appropriate guidance: %v", err)
	}
}

func TestHeadGuardAndMutationPayload(t *testing.T) {
	var mutations atomic.Int32
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			respond(w, fixturePR())
			return
		}
		mutations.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/12/merge":
			if body["sha"] != "head-sha" || body["merge_method"] != "squash" {
				t.Error(body)
			}
			respond(w, map[string]any{"merged": true, "sha": "merged-sha", "message": "Merged"})
		case "/repos/owner/repo/pulls/12/reviews":
			if body["commit_id"] != "head-sha" || body["event"] != "COMMENT" {
				t.Error(body)
			}
			comments := body["comments"].([]any)
			if comments[0].(map[string]any)["side"] != "RIGHT" {
				t.Error(body)
			}
			respond(w, map[string]any{"id": 98, "state": "COMMENTED", "commit_id": "head-sha"})
		default:
			t.Error(r.URL.Path)
		}
	})
	if _, err := client.MergePullRequest(context.Background(), "owner", "repo", 12, "squash", ""); err == nil {
		t.Fatal("empty SHA allowed")
	}
	if _, err := client.MergePullRequest(context.Background(), "owner", "repo", 12, "squash", "stale"); err == nil {
		t.Fatal("stale SHA allowed")
	}
	if _, err := client.SubmitReview(context.Background(), "owner", "repo", 12, ReviewRequest{Event: "APPROVE", CommitID: "stale"}); err == nil {
		t.Fatal("stale review allowed")
	}
	if mutations.Load() != 0 {
		t.Fatal("mutated stale head")
	}
	if result, err := client.MergePullRequest(context.Background(), "owner", "repo", 12, "squash", "head-sha"); err != nil || !result.Merged {
		t.Fatal(result, err)
	}
	if _, err := client.SubmitReview(context.Background(), "owner", "repo", 12, ReviewRequest{Event: "COMMENT", CommitID: "head-sha", Comments: []ReviewCommentRequest{{Body: "Inline", Path: "src/file.go", Line: 1, Side: "RIGHT"}}}); err != nil {
		t.Fatal(err)
	}
	if mutations.Load() != 2 {
		t.Fatal(mutations.Load())
	}
}

func TestGraphQLMutationIDsAndErrors(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			respond(w, fixturePR())
			return
		}
		var payload struct {
			Query     string                    `json:"query"`
			Variables map[string]map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		input := payload.Variables["input"]
		if strings.Contains(payload.Query, "resolveReviewThread") {
			if input["threadId"] != "T_node" {
				t.Error(input)
			}
		} else if input["pullRequestId"] != "PR_node" {
			t.Error(input)
		}
		respond(w, map[string]any{"data": map[string]any{"result": map[string]any{"id": "node"}}})
	})
	if err := client.SetDraft(context.Background(), "owner", "repo", 12, true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetDraft(context.Background(), "owner", "repo", 12, false); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveThread(context.Background(), "T_node", true); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveThread(context.Background(), "T_node", false); err != nil {
		t.Fatal(err)
	}
	failure := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"data": map[string]any{}, "errors": []any{map[string]string{"message": "test-secret forbidden"}}})
	})
	if err := failure.ResolveThread(context.Background(), "T_node", true); err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("unsafe graphql error %v", err)
	}
}
