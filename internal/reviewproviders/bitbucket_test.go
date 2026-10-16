package reviewproviders

import (
	"context"
	"encoding/json"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func bitbucketPR() map[string]any {
	return map[string]any{"id": 7, "title": "Review changes", "description": "Description", "state": "OPEN", "author": map[string]string{"uuid": "{reviewer}", "display_name": "Review Person"}, "created_on": "2026-10-16T12:00:00Z", "updated_on": "2026-10-16T13:00:00Z", "source": map[string]any{"branch": map[string]string{"name": "feature"}, "commit": map[string]string{"hash": glHead}, "repository": map[string]string{"full_name": "fork/app"}}, "destination": map[string]any{"branch": map[string]any{"name": "main", "merge_strategies": []string{"merge_commit", "squash"}}, "commit": map[string]string{"hash": glBase}, "repository": map[string]string{"full_name": "team/app"}}, "reviewers": []any{}, "participants": []any{map[string]any{"user": map[string]string{"uuid": "{other}", "display_name": "Other Reviewer"}, "state": "changes_requested", "participated_on": "2026-10-16T13:00:00Z"}}, "links": map[string]any{"html": map[string]string{"href": "https://bitbucket.org/team/app/pull-requests/7"}}}
}

const providerPatch = "diff --git a/src/app.ts b/src/app.ts\nindex 1111111..2222222 100644\n--- a/src/app.ts\n+++ b/src/app.ts\n@@ -1,2 +1,2 @@\n const stable = 1\n-const value = 1\n+const value = 2\n"

func fakeBitbucket(t *testing.T, handler http.HandlerFunc, options Options) *BitbucketClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "review@example.test" || pass != "synthetic-token" {
			t.Error("incorrect API token authentication")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	options.BaseURL = server.URL
	options.HTTPClient = server.Client()
	client, e := NewBitbucket("bitbucket.org", "review@example.test", "synthetic-token", options)
	if e != nil {
		t.Fatal(e)
	}
	return client
}
func bbFixture(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/user":
		glRespond(w, map[string]string{"uuid": "{reviewer}", "display_name": "Review Person"})
	case "/repositories/team/app/pullrequests/7":
		glRespond(w, bitbucketPR())
	case "/repositories/team/app/pullrequests/7/diffstat":
		glRespond(w, map[string]any{"values": []any{map[string]any{"status": "modified", "lines_added": 1, "lines_removed": 1, "old": map[string]string{"path": "src/app.ts"}, "new": map[string]string{"path": "src/app.ts"}}}})
	case "/repositories/team/app/pullrequests/7/diff":
		w.Write([]byte(providerPatch))
	case "/repositories/team/app/pullrequests/7/commits":
		glRespond(w, map[string]any{"values": []any{map[string]any{"hash": glHead, "message": "Change value", "author": map[string]string{"raw": "Review Person"}, "date": "2026-10-16T12:00:00Z"}}})
	case "/repositories/team/app/pullrequests/7/statuses":
		glRespond(w, map[string]any{"values": []any{map[string]string{"key": "tests", "name": "Tests", "state": "SUCCESSFUL"}}})
	default:
		return false
	}
	return true
}
func TestBitbucketNativeDetailCapabilitiesAndThreadReplies(t *testing.T) {
	client := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if bbFixture(w, r) {
			return
		}
		if r.URL.Path == "/repositories/team/app/pullrequests/7/comments" {
			glRespond(w, map[string]any{"values": []any{map[string]any{"id": 100, "user": map[string]string{"uuid": "{other}"}, "content": map[string]string{"raw": "Check this"}, "inline": map[string]any{"path": "src/app.ts", "to": 2}, "resolution": map[string]string{"type": "comment_resolution"}}, map[string]any{"id": 101, "user": map[string]string{"uuid": "{reviewer}"}, "content": map[string]string{"raw": "Fixed"}, "parent": map[string]int{"id": 100}}}})
			return
		}
		t.Errorf("unexpected %s", r.URL)
		http.NotFound(w, r)
	}, Options{})
	detail, e := client.GetPullRequest(context.Background(), "team", "app", 7)
	if e != nil {
		t.Fatal(e)
	}
	if detail.PullRequest.Provider != "bitbucket" || detail.PullRequest.Head.Repository != "fork/app" || len(detail.Files) != 1 || detail.Files[0].Additions != 1 || !strings.HasPrefix(detail.Files[0].Patch, "@@") {
		t.Fatalf("wrong detail: %+v", detail)
	}
	if !detail.Capabilities.RequestChanges || !detail.Capabilities.Draft || detail.Capabilities.Merge || detail.Capabilities.Labels || detail.Capabilities.Assignees || detail.Capabilities.Actions {
		t.Fatalf("wrong capabilities: %+v", detail.Capabilities)
	}
	if len(detail.Threads) != 1 || !detail.Threads[0].Resolved || len(detail.Threads[0].Comments) != 2 || detail.Threads[0].Comments[1].InReplyToID != 100 {
		t.Fatalf("wrong threads: %+v", detail.Threads)
	}
	if len(detail.Reviews) != 1 || detail.Reviews[0].State != "CHANGES_REQUESTED" || detail.Statuses[0].State != "success" {
		t.Fatal("lost review/check state")
	}
}
func TestBitbucketPaginationScopeAndExternalLinks(t *testing.T) {
	var base string
	calls := 0
	client := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			bbFixture(w, r)
			return
		}
		calls++
		if r.URL.Query().Get("q") != "reviewers.uuid = \"{reviewer}\"" {
			t.Errorf("missing reviewer filter: %s", r.URL)
		}
		next := base + "/repositories/team/app/pullrequests?page=opaque-cursor&q=reviewers.uuid%20%3D%20%22%7Breviewer%7D%22"
		if calls > 1 {
			next = ""
		}
		glRespond(w, map[string]any{"values": []any{bitbucketPR()}, "next": next})
	}, Options{})
	base = client.api.base
	r := githubclient.PullRequestListRequest{State: "open", View: "review-requested", PageSize: 1}
	first, e := client.ListPullRequestsPage(context.Background(), "team", "app", r)
	if e != nil || !first.HasNextPage || first.TotalCount != -1 {
		t.Fatalf("page: %+v %v", first, e)
	}
	r.Cursor = first.NextCursor
	if _, e = client.ListPullRequestsPage(context.Background(), "team", "app", r); e != nil {
		t.Fatal(e)
	}
	r.Query = "other"
	if _, e = client.ListPullRequestsPage(context.Background(), "team", "app", r); e == nil {
		t.Fatal("cursor reused for another query")
	}
	if _, e = client.nextPath("https://outside.example.test/repositories/team/app/pullrequests?page=2", "/repositories/team/app/pullrequests"); e == nil {
		t.Fatal("external pagination accepted")
	}
}
func TestBitbucketReviewRetryAndNativeChangeRequest(t *testing.T) {
	var mu sync.Mutex
	notes := []map[string]any{}
	posts, votes := 0, 0
	lose := true
	client := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path
		if path == "/repositories/team/app/pullrequests/7/comments" {
			if r.Method == "GET" {
				glRespond(w, map[string]any{"values": notes})
				return
			}
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid body")
			}
			if body["inline"] != nil {
				inline := body["inline"].(map[string]any)
				if inline["to"] != float64(2) || inline["from"] != nil {
					t.Errorf("wrong side: %+v", inline)
				}
			}
			posts++
			body["id"] = 100 + posts
			body["user"] = map[string]string{"uuid": "{reviewer}"}
			notes = append(notes, body)
			if lose {
				lose = false
				w.WriteHeader(503)
				return
			}
			glRespond(w, body)
			return
		}
		if path == "/repositories/team/app/pullrequests/7/request-changes" {
			votes++
			w.WriteHeader(200)
			return
		}
		if bbFixture(w, r) {
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL)
	}, Options{})
	r := githubclient.ReviewRequest{CommitID: glHead, Event: "REQUEST_CHANGES", Body: "Please revise", Comments: []githubclient.ReviewCommentRequest{{ClientID: "draft-1", Path: "src/app.ts", Line: 2, Side: "RIGHT", Body: "Check value"}}}
	if _, e := client.SubmitReview(context.Background(), "team", "app", 7, r); e == nil {
		t.Fatal("expected first response failure")
	}
	review, e := client.SubmitReview(context.Background(), "team", "app", 7, r)
	if e != nil {
		t.Fatal(e)
	}
	if posts != 2 || votes != 1 || review.State != "CHANGES_REQUESTED" {
		t.Fatalf("duplicate or missing publication: %d %d %+v", posts, votes, review)
	}
	if _, e = client.SubmitReview(context.Background(), "team", "app", 7, r); e != nil {
		t.Fatal(e)
	}
	if posts != 2 || votes != 1 {
		t.Fatal("retry published duplicates")
	}
}
func TestBitbucketImmutableFilesHeadGuardAndReadonly(t *testing.T) {
	writes := 0
	client := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			t.Error("unexpected write")
		}
		if r.URL.Path == "/repositories/fork/app/src/"+glHead+"/src/app.ts" {
			w.Write([]byte("const value = 2\n"))
			return
		}
		if bbFixture(w, r) {
			return
		}
		http.NotFound(w, r)
	}, Options{ReadOnly: true})
	if _, e := client.ReadFileAtCommit(context.Background(), "fork", "app", "src/app.ts", glHead); e != nil {
		t.Fatal(e)
	}
	if _, e := client.ReadFileAtCommit(context.Background(), "fork", "app", "../outside", glHead); e == nil {
		t.Fatal("path escaped")
	}
	if _, e := client.SubmitReview(context.Background(), "team", "app", 7, githubclient.ReviewRequest{CommitID: glBase, Event: "APPROVE"}); e == nil {
		t.Fatal("stale head accepted")
	}
	if _, e := client.AddIssueComment(context.Background(), "team", "app", 7, "No write"); e == nil {
		t.Fatal("readonly write allowed")
	}
	source, e := client.PullRequestSource(context.Background(), "team", "app", 7)
	if e != nil || source.HeadURL != "https://bitbucket.org/fork/app.git" || source.HeadSHA != glHead || source.HeadRef != "refs/heads/feature" {
		t.Fatalf("source: %+v %v", source, e)
	}
	if writes != 0 {
		t.Fatal("readonly request reached server")
	}
	if _, e = client.MergePullRequest(context.Background(), "team", "app", 7, "merge", glHead); e == nil || !strings.Contains(e.Error(), "atomically") {
		t.Fatal("unguarded merge exposed")
	}
}
func TestProviderDiffQuotedPathsAndSourceHeaderLines(t *testing.T) {
	diff := "diff --git \"a/space path/\\303\\251.ts\" \"b/space path/\\303\\251.ts\"\n--- \"a/space path/\\303\\251.ts\"\n+++ \"b/space path/\\303\\251.ts\"\n@@ -1 +1 @@\n---- source header\n++++ source header\n"
	files, e := splitFilePatches(diff)
	if e != nil {
		t.Fatal(e)
	}
	patch := files["space path/é.ts"]
	a, d := patchCounts(patch)
	if patch == "" || a != 1 || d != 1 {
		t.Fatalf("wrong paths/counts: %+v %d %d", files, a, d)
	}
}
