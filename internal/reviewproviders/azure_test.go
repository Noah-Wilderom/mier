package reviewproviders

import (
	"context"
	"encoding/json"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var azCommon = strings.Repeat("c", 40)

const azActor = "d6245f20-2af8-44f4-9451-8107cb2767db"

func azurePR() map[string]any {
	return map[string]any{"pullRequestId": 7, "repository": map[string]any{"id": "repository-id", "name": "app", "project": map[string]string{"name": "Project"}}, "title": "Review changes", "description": "Description", "status": "active", "createdBy": map[string]string{"id": azActor, "displayName": "Review Person"}, "creationDate": "2026-10-16T12:00:00Z", "sourceRefName": "refs/heads/feature", "targetRefName": "refs/heads/main", "lastMergeSourceCommit": map[string]string{"commitId": glHead}, "lastMergeTargetCommit": map[string]string{"commitId": glBase}, "lastMergeCommit": map[string]string{"commitId": strings.Repeat("d", 40)}, "mergeStatus": "succeeded", "supportsIterations": true, "reviewers": []any{map[string]any{"id": "other-reviewer", "displayName": "Other Person", "vote": 10}}, "labels": []map[string]string{{"id": "label-id", "name": "enhancement"}}}
}
func fakeAzure(t *testing.T, handler http.HandlerFunc, options Options) *AzureClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "" || pass != "synthetic-token" {
			t.Error("incorrect Azure PAT auth")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	options.BaseURL = server.URL
	options.HTTPClient = server.Client()
	options.Organization = "org"
	if options.Diff == nil {
		git, e := gitclient.New(gitclient.Options{})
		if e != nil {
			t.Fatal(e)
		}
		options.Diff = git.DiffText
	}
	client, e := NewAzure("dev.azure.com", "synthetic-token", options)
	if e != nil {
		t.Fatal(e)
	}
	return client
}
func azureFixture(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/org/_apis/connectionData":
		glRespond(w, map[string]any{"authenticatedUser": map[string]string{"id": azActor, "providerDisplayName": "Review Person"}})
	case "/org/Project/_apis/git/repositories/app/pullRequests/7":
		glRespond(w, azurePR())
	case "/org/Project/_apis/git/repositories/app/pullRequests/7/iterations":
		glRespond(w, map[string]any{"value": []any{map[string]any{"id": 3, "sourceRefCommit": map[string]string{"commitId": glHead}, "targetRefCommit": map[string]string{"commitId": glBase}, "commonRefCommit": map[string]string{"commitId": azCommon}}}})
	case "/org/Project/_apis/git/repositories/app/pullRequests/7/iterations/3/changes":
		glRespond(w, map[string]any{"changeEntries": []any{map[string]any{"changeTrackingId": 21, "changeType": "edit, rename", "originalPath": "/src/old.ts", "item": map[string]string{"path": "/src/app.ts", "gitObjectType": "blob"}}}, "nextSkip": 0})
	case "/org/Project/_apis/git/repositories/app/items":
		if r.URL.Query().Get("versionDescriptor.versionType") != "commit" {
			return false
		}
		switch r.URL.Query().Get("versionDescriptor.version") {
		case azCommon:
			if r.URL.Query().Get("path") != "/src/old.ts" {
				return false
			}
			w.Write([]byte("const stable = 1\nconst value = 1\n"))
		case glHead:
			if r.URL.Query().Get("path") != "/src/app.ts" {
				return false
			}
			w.Write([]byte("const stable = 1\nconst value = 2\n"))
		default:
			return false
		}
	case "/org/Project/_apis/git/repositories/app/pullRequests/7/commits":
		glRespond(w, map[string]any{"value": []any{map[string]any{"commitId": glHead, "comment": "Change value", "author": map[string]string{"name": "Review Person", "date": "2026-10-16T12:00:00Z"}}}})
	case "/org/Project/_apis/git/repositories/app/pullRequests/7/statuses":
		glRespond(w, map[string]any{"value": []any{map[string]any{"id": 5, "state": "succeeded", "context": map[string]string{"name": "Tests"}, "targetUrl": "https://dev.azure.com/org/Project/_build/results?buildId=1"}}})
	default:
		return false
	}
	return true
}
func TestAzureImmutableDiffsNativeDetailAndPackedCommentIDs(t *testing.T) {
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/Project/_apis/git/repositories/app/pullRequests/7/threads" {
			glRespond(w, map[string]any{"value": []any{map[string]any{"id": 100, "status": "fixed", "threadContext": map[string]any{"filePath": "/src/app.ts", "rightFileStart": map[string]int{"line": 2}}, "pullRequestThreadContext": map[string]any{"changeTrackingId": 21, "iterationContext": map[string]int{"firstComparingIteration": 0, "secondComparingIteration": 3}}, "comments": []any{map[string]any{"id": 1, "author": map[string]string{"id": "other-reviewer"}, "content": "Check this", "commentType": "text"}, map[string]any{"id": 2, "parentCommentId": 1, "author": map[string]string{"id": azActor}, "content": "Fixed", "commentType": "text"}}}}})
			return
		}
		if azureFixture(w, r) {
			return
		}
		t.Errorf("unexpected %s", r.URL)
		http.NotFound(w, r)
	}, Options{})
	detail, e := client.GetPullRequest(context.Background(), "org/Project", "app", 7)
	if e != nil {
		t.Fatal(e)
	}
	if detail.PullRequest.Provider != "azure" || detail.PullRequest.Head.SHA != glHead || detail.PullRequest.Base.SHA != glBase || detail.Files[0].PreviousPath != "src/old.ts" || detail.Files[0].Additions != 1 || !strings.Contains(detail.Files[0].Patch, "+const value = 2") {
		t.Fatalf("wrong detail: %+v", detail)
	}
	if !detail.Capabilities.Merge || !detail.Capabilities.RequestChanges || detail.Capabilities.Assignees || detail.Capabilities.Actions {
		t.Fatal("incorrect native capabilities")
	}
	thread := detail.Threads[0]
	if !thread.Resolved || thread.Outdated || len(thread.Comments) != 2 {
		t.Fatalf("wrong thread %+v", thread)
	}
	first, _ := azCommentID(100, 1)
	second, _ := azCommentID(100, 2)
	if thread.Comments[0].ID != first || thread.Comments[1].ID != second || thread.Comments[1].InReplyToID != first {
		t.Fatal("thread-local IDs collided")
	}
	other, _ := azCommentID(101, 1)
	if other == first {
		t.Fatal("comment IDs collided across threads")
	}
	max, e := azCommentID(2147483647, (1<<21)-1)
	if e != nil || max >= 1<<53 {
		t.Fatal("comment ID is not JavaScript-safe")
	}
	a, b, e := azUnpackComment(max)
	if e != nil || a != 2147483647 || b != (1<<21)-1 {
		t.Fatal("packed ID did not round-trip")
	}
	if _, e = azCommentID(1, 1<<21); e == nil {
		t.Fatal("unrepresentable comment accepted")
	}
}
func TestAzureReviewRetryIterationContextAndCommentDoesNotResetVote(t *testing.T) {
	var mu sync.Mutex
	threads := []map[string]any{}
	posts, votes := 0, 0
	lose := true
	lastVote := 0
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path
		if path == "/org/Project/_apis/git/repositories/app/pullRequests/7/threads" {
			if r.Method == "GET" {
				glRespond(w, map[string]any{"value": threads})
				return
			}
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			if value, ok := input["pullRequestThreadContext"]; ok {
				native := value.(map[string]any)
				iteration := native["iterationContext"].(map[string]any)
				if native["changeTrackingId"] != float64(21) || iteration["firstComparingIteration"] != float64(0) || iteration["secondComparingIteration"] != float64(3) {
					t.Errorf("incorrect iteration: %+v", native)
				}
				tc := input["threadContext"].(map[string]any)
				if tc["rightFileStart"] == nil || tc["leftFileStart"] != nil {
					t.Error("wrong comment side")
				}
			}
			posts++
			note := input["comments"].([]any)[0].(map[string]any)
			note["id"] = 1
			note["author"] = map[string]string{"id": azActor}
			note["commentType"] = "text"
			input["id"] = 100 + posts
			input["status"] = "active"
			threads = append(threads, input)
			if lose {
				lose = false
				w.WriteHeader(503)
				return
			}
			glRespond(w, input)
			return
		}
		if strings.Contains(path, "/reviewers/") {
			var body struct {
				Vote int `json:"vote"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			votes++
			lastVote = body.Vote
			glRespond(w, map[string]any{"id": azActor, "vote": lastVote})
			return
		}
		if azureFixture(w, r) {
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}, Options{})
	request := githubclient.ReviewRequest{CommitID: glHead, Event: "REQUEST_CHANGES", Body: "Please revise", Comments: []githubclient.ReviewCommentRequest{{ClientID: "draft-1", Body: "Check value", Path: "src/app.ts", Line: 2, Side: "RIGHT"}}}
	if _, e := client.SubmitReview(context.Background(), "org/Project", "app", 7, request); e == nil {
		t.Fatal("expected response loss")
	}
	if _, e := client.SubmitReview(context.Background(), "org/Project", "app", 7, request); e != nil {
		t.Fatal(e)
	}
	if posts != 2 || votes != 1 || lastVote != -10 {
		t.Fatalf("wrong publication: posts %d votes %d value %d", posts, votes, lastVote)
	}
	request.Event = "COMMENT"
	request.Body = "Additional context"
	request.Comments = nil
	if _, e := client.SubmitReview(context.Background(), "org/Project", "app", 7, request); e != nil {
		t.Fatal(e)
	}
	if votes != 1 || lastVote != -10 {
		t.Fatal("comment-only review reset an existing vote")
	}
}
func TestAzureCompletionHasExpectedSourceAndHonorsPolicies(t *testing.T) {
	writes := 0
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			writes++
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			head := input["lastMergeSourceCommit"].(map[string]any)
			options := input["completionOptions"].(map[string]any)
			if head["commitId"] != glHead || input["status"] != "completed" || options["mergeStrategy"] != "squash" || options["bypassPolicy"] != false || options["deleteSourceBranch"] != false {
				t.Errorf("unguarded completion: %+v", input)
			}
			pr := azurePR()
			pr["status"] = "completed"
			glRespond(w, pr)
			return
		}
		if azureFixture(w, r) {
			return
		}
		http.NotFound(w, r)
	}, Options{})
	if _, e := client.MergePullRequest(context.Background(), "org/Project", "app", 7, "squash", glBase); e == nil {
		t.Fatal("stale completion accepted")
	}
	if writes != 0 {
		t.Fatal("stale completion wrote")
	}
	merged, e := client.MergePullRequest(context.Background(), "org/Project", "app", 7, "squash", glHead)
	if e != nil || !merged.Merged || writes != 1 {
		t.Fatalf("merge: %+v %v", merged, e)
	}
}
func TestAzureListCursorSearchAndSourceUsesOriginalHead(t *testing.T) {
	pages := []int{}
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/Project/_apis/git/repositories/app/pullrequests" {
			q := r.URL.Query()
			skip, _ := strconv.Atoi(q.Get("$skip"))
			pages = append(pages, skip)
			if q.Get("searchCriteria.reviewerId") != azActor || q.Get("searchCriteria.status") != "active" {
				t.Error("lost list filters")
			}
			glRespond(w, map[string]any{"value": []any{azurePR()}})
			return
		}
		if azureFixture(w, r) {
			return
		}
		http.NotFound(w, r)
	}, Options{})
	request := githubclient.PullRequestListRequest{PageSize: 1, State: "open", View: "review-requested", Query: "Review"}
	page, e := client.ListPullRequestsPage(context.Background(), "org/Project", "app", request)
	if e != nil || !page.HasNextPage || page.TotalCount != -1 {
		t.Fatalf("page %+v %v", page, e)
	}
	request.Cursor = page.NextCursor
	if _, e = client.ListPullRequestsPage(context.Background(), "org/Project", "app", request); e != nil {
		t.Fatal(e)
	}
	if len(pages) != 2 || pages[1] != 1 {
		t.Fatal("offset cursor failed")
	}
	request.Query = "other"
	if _, e = client.ListPullRequestsPage(context.Background(), "org/Project", "app", request); e == nil {
		t.Fatal("cross-query cursor accepted")
	}
	source, e := client.PullRequestSource(context.Background(), "org/Project", "app", 7)
	if e != nil || source.HeadSHA != glHead || source.HeadRef != "refs/heads/feature" || source.HeadURL != "" {
		t.Fatalf("wrong source %+v %v", source, e)
	}
}
func TestAzureReadonlyAndThreadResolution(t *testing.T) {
	writes := 0
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
			t.Error("readonly write reached provider")
		}
		if azureFixture(w, r) {
			return
		}
		http.NotFound(w, r)
	}, Options{ReadOnly: true})
	if _, e := client.AddIssueComment(context.Background(), "org/Project", "app", 7, "Test"); e == nil {
		t.Fatal("readonly comment accepted")
	}
	if e := client.ResolveThread(context.Background(), encodedThread("azure", "org/Project", "app", 7, "100"), true); e == nil {
		t.Fatal("readonly resolution accepted")
	}
	if writes != 0 {
		t.Fatal("unexpected writes")
	}
	if _, e := client.ReadFileAtCommit(context.Background(), "org/Project", "app", "../outside", glHead); e == nil {
		t.Fatal("path escaped")
	}
}
