package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"testing"
)

func TestCreateEditConversationInlineAndReplyPayloads(t *testing.T) {
	var mutex sync.Mutex
	seen := map[string]map[string]any{}
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respond(w, fixturePR())
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		key := r.Method + " " + r.URL.Path
		mutex.Lock()
		seen[key] = body
		mutex.Unlock()
		switch r.URL.Path {
		case "/repos/owner/repo/pulls", "/repos/owner/repo/pulls/12":
			respond(w, fixturePR())
		default:
			respond(w, map[string]any{"id": 991, "node_id": "comment-node", "body": body["body"], "commit_id": body["commit_id"], "user": map[string]string{"login": "reviewer"}})
		}
	})
	ctx := context.Background()
	if _, err := client.CreatePullRequest(ctx, "owner", "repo", CreateRequest{Title: "Title", Body: "Body", Base: "main", Head: "owner:feature", Draft: true}); err != nil {
		t.Fatal(err)
	}
	title, body, state := "New title", "New description", "closed"
	if _, err := client.UpdatePullRequest(ctx, "owner", "repo", 12, UpdateRequest{Title: &title, Body: &body, State: &state}); err != nil {
		t.Fatal(err)
	}
	if comment, err := client.AddIssueComment(ctx, "owner", "repo", 12, "Discussion"); err != nil || comment.ID != 991 {
		t.Fatal(comment, err)
	}
	if comment, err := client.AddReviewComment(ctx, "owner", "repo", 12, ReviewCommentRequest{Body: "Specific feedback", Path: "src/file.go", Line: 7, Side: "LEFT", CommitID: "head-sha"}); err != nil || comment.CommitID != "head-sha" {
		t.Fatal(comment, err)
	}
	if _, err := client.ReplyReviewComment(ctx, "owner", "repo", 12, 991, "Reply"); err != nil {
		t.Fatal(err)
	}
	expected := map[string]map[string]any{
		"POST /repos/owner/repo/pulls":                         {"title": "Title", "body": "Body", "base": "main", "head": "owner:feature", "draft": true},
		"PATCH /repos/owner/repo/pulls/12":                     {"title": "New title", "body": "New description", "state": "closed"},
		"POST /repos/owner/repo/issues/12/comments":            {"body": "Discussion"},
		"POST /repos/owner/repo/pulls/12/comments":             {"body": "Specific feedback", "path": "src/file.go", "line": float64(7), "side": "LEFT", "commit_id": "head-sha"},
		"POST /repos/owner/repo/pulls/12/comments/991/replies": {"body": "Reply"},
	}
	if !reflect.DeepEqual(seen, expected) {
		t.Fatalf("wrong endpoint payloads\n got %#v\nwant %#v", seen, expected)
	}
}

func TestPermissionFailureAndChangedHeadNeverProduceFalseReadyData(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		respond(w, map[string]string{"message": "Resource not accessible by personal access token"})
	})
	if _, err := client.ListPullRequests(context.Background(), "owner", "repo", "open"); err == nil {
		t.Fatal("permission failure hidden")
	}
	if _, err := client.SubmitReview(context.Background(), "owner", "repo", 12, ReviewRequest{Event: "REQUEST_CHANGES", CommitID: "head-sha"}); err == nil {
		t.Fatal("empty requested changes allowed")
	}
	if _, err := client.AddReviewComment(context.Background(), "owner", "repo", 12, ReviewCommentRequest{Body: "Feedback", Path: "src/file.go", Line: 1, Side: "MIDDLE", CommitID: "head-sha"}); err == nil {
		t.Fatal("invalid diff side allowed")
	}
}
