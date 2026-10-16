package reviewproviders

import (
	"context"
	"encoding/json"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var glBase = strings.Repeat("a", 40)
var glHead = strings.Repeat("b", 40)

func gitlabMR() map[string]any {
	return map[string]any{"id": 77, "iid": 7, "title": "Draft: Review changes", "description": "A careful change", "state": "opened", "draft": true, "web_url": "https://gitlab.example.test/team/sub/app/-/merge_requests/7", "author": map[string]any{"id": 42, "username": "reviewer", "name": "Review Person"}, "reviewers": []any{}, "assignees": []any{}, "labels": []string{"enhancement"}, "source_branch": "feature", "target_branch": "main", "source_project_id": 1, "target_project_id": 1, "sha": glHead, "diff_refs": map[string]string{"base_sha": glBase, "start_sha": glBase, "head_sha": glHead}, "changes_count": "1", "user_notes_count": 1, "created_at": "2026-10-15T12:00:00Z", "updated_at": "2026-10-15T13:00:00Z", "merge_status": "can_be_merged", "detailed_merge_status": "mergeable"}
}
func glRespond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
func fakeGitlab(t *testing.T, handler http.HandlerFunc, options Options) *GitLabClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "synthetic-token" {
			t.Error("missing token")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	options.BaseURL = server.URL
	client, err := NewGitLab("gitlab.example.test", "synthetic-token", options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func TestGitLabNestedNamespacesDetailAndReadOnlyCapabilities(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(strconv.FormatBool(readOnly), func(t *testing.T) {
			client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				switch {
				case path == "/user":
					glRespond(w, map[string]any{"id": 42, "username": "reviewer", "name": "Review Person"})
				case path == "/projects/team/sub/app":
					if !strings.Contains(r.URL.EscapedPath(), "team%2Fsub%2Fapp") {
						t.Error("namespace was not encoded as one project ID")
					}
					glRespond(w, map[string]any{"id": 1, "path_with_namespace": "team/sub/app", "merge_method": "merge", "squash_option": "default_off"})
				case strings.HasSuffix(path, "/merge_requests/7"):
					glRespond(w, gitlabMR())
				case strings.HasSuffix(path, "/diffs"):
					glRespond(w, []any{map[string]any{"old_path": "file.go", "new_path": "file.go", "diff": "@@ -1,2 +1,2 @@\n package main\n-old\n+new\n"}})
				case strings.HasSuffix(path, "/commits"):
					glRespond(w, []any{map[string]any{"id": glHead, "title": "Change", "message": "Change\n\nBody", "author_name": "Review Person", "authored_date": "2026-10-15T13:00:00Z"}})
				case strings.HasSuffix(path, "/discussions"):
					glRespond(w, []any{map[string]any{"id": "discussion-1", "individual_note": false, "notes": []any{map[string]any{"id": 55, "body": "Line comment", "author": map[string]any{"id": 42, "username": "reviewer"}, "resolvable": true, "resolved": false, "position": map[string]any{"new_path": "file.go", "old_path": "file.go", "new_line": 2, "head_sha": glHead}}}}})
				case strings.HasSuffix(path, "/statuses"):
					glRespond(w, []any{map[string]any{"id": 60, "name": "Tests", "status": "success", "target_url": "https://gitlab.example.test/job/60"}})
				case strings.HasSuffix(path, "/approvals"):
					glRespond(w, map[string]any{"approved": true, "approvals_required": 1, "approved_by": []any{map[string]any{"user": map[string]any{"id": 42, "username": "reviewer"}, "approved_at": "2026-10-15T13:00:00Z"}}})
				default:
					t.Errorf("unexpected GitLab path %s", r.URL)
					http.NotFound(w, r)
				}
			}, Options{ReadOnly: readOnly})
			result, err := client.GetPullRequest(context.Background(), "team/sub", "app", 7)
			if err != nil {
				t.Fatal(err)
			}
			if result.PullRequest.Provider != "gitlab" || result.PullRequest.Title != "Review changes" || result.PullRequest.Status != "draft" || result.PullRequest.Head.Repository != "team/sub/app" || len(result.Files) != 1 || result.Files[0].Additions != 1 || len(result.Threads) != 1 || len(result.Statuses) != 1 || len(result.Reviews) != 1 || result.MergeStateStatus != "CLEAN" {
				t.Fatalf("bad native mapping: %+v", result)
			}
			if result.Capabilities.Comments == readOnly || result.ViewerCanUpdate == readOnly || result.Capabilities.RequestChanges {
				t.Fatal(result.Capabilities, result.ViewerCanUpdate)
			}
			owner, repo, number, id, err := decodeThread(result.Threads[0].ID, "gitlab")
			if err != nil || owner != "team/sub" || repo != "app" || number != 7 || id != "discussion-1" {
				t.Fatal(owner, repo, number, id, err)
			}
		})
	}
}
func TestGitLabPageScopesAndNativeFilters(t *testing.T) {
	requests := 0
	client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			glRespond(w, map[string]any{"id": 42, "username": "reviewer"})
			return
		}
		requests++
		query := r.URL.Query()
		if query.Get("reviewer_id") != "42" || query.Get("search") != "fix parser" || query.Get("state") != "opened" {
			t.Error("native filters missing", query)
		}
		w.Header().Set("X-Total", "2")
		if query.Get("page") == "1" {
			w.Header().Set("X-Next-Page", "2")
		}
		glRespond(w, []any{gitlabMR()})
	}, Options{})
	request := githubclient.PullRequestListRequest{State: "open", View: "review-requested", Query: "fix parser", PageSize: 1}
	page, err := client.ListPullRequestsPage(context.Background(), "team/sub", "app", request)
	if err != nil || !page.HasNextPage || page.TotalCount != 2 || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	request.Cursor = page.NextCursor
	page, err = client.ListPullRequestsPage(context.Background(), "team/sub", "app", request)
	if err != nil || page.HasNextPage {
		t.Fatal(page, err)
	}
	request.Query = "another search"
	if _, err = client.ListPullRequestsPage(context.Background(), "team/sub", "app", request); err == nil || requests != 2 {
		t.Fatal("cursor query binding lost", requests, err)
	}
}
func TestGitLabInlineContextSendsBothLineNumbersAndHeadGuard(t *testing.T) {
	mutations := 0
	client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests/7"):
			glRespond(w, gitlabMR())
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			glRespond(w, []any{map[string]any{"old_path": "before.go", "new_path": "after.go", "renamed_file": true, "diff": "@@ -1,2 +1,3 @@\n context\n+added\n last\n"}})
		case strings.HasSuffix(r.URL.Path, "/discussions") && r.Method == "POST":
			mutations++
			var payload struct {
				Body     string     `json:"body"`
				Position glPosition `json:"position"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.Position.OldPath != "before.go" || payload.Position.NewPath != "after.go" || payload.Position.BaseSHA != glBase || payload.Position.HeadSHA != glHead || payload.Position.OldLine == nil || *payload.Position.OldLine != 1 || payload.Position.NewLine == nil || *payload.Position.NewLine != 1 {
				t.Error("native context or rename lost", payload)
			}
			glRespond(w, map[string]any{"id": "discussion-1", "notes": []any{map[string]any{"id": 55, "body": payload.Body, "author": map[string]any{"id": 42, "username": "reviewer"}}}})
		default:
			t.Error("unexpected", r.URL)
			http.NotFound(w, r)
		}
	}, Options{})
	request := githubclient.ReviewCommentRequest{Path: "after.go", Line: 1, Side: "RIGHT", Body: "Context comment", CommitID: glHead}
	if _, err := client.AddReviewComment(context.Background(), "team/sub", "app", 7, request); err != nil {
		t.Fatal(err)
	}
	request.CommitID = glBase
	if _, err := client.AddReviewComment(context.Background(), "team/sub", "app", 7, request); err == nil || mutations != 1 {
		t.Fatal("stale comment published", mutations, err)
	}
}
func TestGitLabReadsImmutableForkSourcesAndRejectsReadonlyWrites(t *testing.T) {
	client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("read-only request mutated provider")
		}
		if strings.HasSuffix(r.URL.Path, "/raw") {
			if r.URL.Query().Get("ref") != glHead || !strings.Contains(r.URL.EscapedPath(), "src%2Ffile.php") {
				t.Error("mutable or malformed source path", r.URL)
			}
			w.Write([]byte("<?php\nclass Source {}\n"))
			return
		}
		t.Error("unexpected", r.URL)
		http.NotFound(w, r)
	}, Options{ReadOnly: true})
	source, err := client.ReadFileAtCommit(context.Background(), "fork/nested", "app", "src/file.php", glHead)
	if err != nil || !strings.Contains(source, "class Source") {
		t.Fatal(source, err)
	}
	if _, err = client.CreatePullRequest(context.Background(), "team/sub", "app", githubclient.CreateRequest{Title: "Change", Head: "feature", Base: "main"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatal(err)
	}
}
func TestGitLabReviewPublicationIsNativeAndResumable(t *testing.T) {
	var mu sync.Mutex
	notes := []map[string]any{}
	approvalCalls := 0
	client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/user":
			glRespond(w, map[string]any{"id": 42, "username": "reviewer"})
		case strings.HasSuffix(r.URL.Path, "/merge_requests/7"):
			mr := gitlabMR()
			mr["draft"] = false
			glRespond(w, mr)
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			glRespond(w, []any{map[string]any{"old_path": "file.go", "new_path": "file.go", "diff": "@@ -1 +1 @@\n-old\n+new\n"}})
		case strings.HasSuffix(r.URL.Path, "/discussions") && r.Method == "GET":
			items := []any{}
			for _, note := range notes {
				items = append(items, map[string]any{"id": "d" + strconv.Itoa(note["id"].(int)), "notes": []any{note}})
			}
			glRespond(w, items)
		case strings.HasSuffix(r.URL.Path, "/discussions") && r.Method == "POST":
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			note := map[string]any{"id": len(notes) + 1, "body": payload["body"], "author": map[string]any{"id": 42, "username": "reviewer"}, "position": payload["position"]}
			notes = append(notes, note)
			glRespond(w, map[string]any{"id": "d1", "notes": []any{note}})
		case strings.HasSuffix(r.URL.Path, "/approve"):
			approvalCalls++
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["sha"] != glHead {
				t.Error("approval not pinned to head")
			}
			glRespond(w, map[string]bool{"approved": true})
		default:
			t.Error("unexpected", r.URL)
			http.NotFound(w, r)
		}
	}, Options{})
	request := githubclient.ReviewRequest{CommitID: glHead, Event: "APPROVE", Comments: []githubclient.ReviewCommentRequest{{ClientID: "draft-one", Path: "file.go", Line: 1, Side: "RIGHT", Body: "A review comment"}}}
	for i := 0; i < 2; i++ {
		review, err := client.SubmitReview(context.Background(), "team/sub", "app", 7, request)
		if err != nil || review.State != "APPROVED" {
			t.Fatal(review, err)
		}
	}
	if len(notes) != 1 || approvalCalls != 1 {
		t.Fatal("duplicate native publication", len(notes), approvalCalls)
	}
}
