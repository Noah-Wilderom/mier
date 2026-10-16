package reviewproviders

import (
	"context"
	"encoding/json"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"strings"
	"testing"
)

func TestBitbucketNativeCreationRetargetingAndReviewerPreservation(t *testing.T) {
	ctx := context.Background()
	updates := []map[string]any{}
	client := fakeBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repositories/team/app/pullrequests" {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			if input["title"] != "Stacked work" || input["draft"] != true || input["close_source_branch"] != false || input["source"].(map[string]any)["branch"].(map[string]any)["name"] != "child" || input["destination"].(map[string]any)["branch"].(map[string]any)["name"] != "feature" {
				t.Errorf("wrong creation: %+v", input)
			}
			glRespond(w, bitbucketPR())
			return
		}
		if r.Method == "PUT" {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			updates = append(updates, input)
			glRespond(w, bitbucketPR())
			return
		}
		if r.Method == "GET" && r.URL.Path == "/repositories/team/app/pullrequests/7" {
			pr := bitbucketPR()
			pr["reviewers"] = []map[string]string{{"uuid": "{existing}"}}
			glRespond(w, pr)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}, Options{})
	if _, e := client.CreatePullRequest(ctx, "team", "app", githubclient.CreateRequest{Title: "Stacked work", Base: "feature", Head: "child", Draft: true}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.RetargetPullRequest(ctx, "team", "app", 7, githubclient.RetargetRequest{Base: "parent", ExpectedBase: "main", ExpectedHead: glBase}); e == nil {
		t.Fatal("stale retarget accepted")
	}
	if len(updates) != 0 {
		t.Fatal("stale retarget wrote")
	}
	if _, e := client.RetargetPullRequest(ctx, "team", "app", 7, githubclient.RetargetRequest{Base: "parent", ExpectedBase: "main", ExpectedHead: glHead}); e != nil {
		t.Fatal(e)
	}
	if updates[0]["destination"].(map[string]any)["branch"].(map[string]any)["name"] != "parent" {
		t.Fatal("wrong target")
	}
	if _, e := client.RequestReviewers(ctx, "team", "app", 7, []string{"{new}"}); e != nil {
		t.Fatal(e)
	}
	members := map[string]bool{}
	for _, wire := range updates[1]["reviewers"].([]any) {
		members[wire.(map[string]any)["uuid"].(string)] = true
	}
	if len(members) != 2 || !members["{existing}"] || !members["{new}"] {
		t.Fatal("reviewer replacement lost existing members")
	}
}

func TestAzureNativeMetadataDoesNotResetExistingReviewerVotes(t *testing.T) {
	ctx := context.Background()
	writes := 0
	labels := []azLabel{{ID: "keep", Name: "keep"}, {ID: "remove", Name: "remove"}}
	created := []string{}
	deleted := []string{}
	client := fakeAzure(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/org/Project/_apis/git/repositories/app/pullRequests/7/labels" {
			if r.Method == "GET" {
				glRespond(w, map[string]any{"value": labels})
				return
			}
			var input azLabel
			json.NewDecoder(r.Body).Decode(&input)
			created = append(created, input.Name)
			glRespond(w, input)
			return
		}
		if strings.Contains(r.URL.Path, "/labels/") && r.Method == "DELETE" {
			deleted = append(deleted, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			w.WriteHeader(204)
			return
		}
		if strings.Contains(r.URL.Path, "/reviewers/") && r.Method == "PUT" {
			writes++
			if strings.HasSuffix(r.URL.Path, "/other-reviewer") {
				t.Error("reset the existing approval vote")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["id"] != "new-reviewer" || body["vote"] != float64(0) {
				t.Errorf("wrong invitation %+v", body)
			}
			glRespond(w, body)
			return
		}
		if azureFixture(w, r) {
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}, Options{})
	if _, e := client.RequestReviewers(ctx, "org/Project", "app", 7, []string{"other-reviewer", "new-reviewer"}); e != nil {
		t.Fatal(e)
	}
	if writes != 1 {
		t.Fatal("wrong invitation count")
	}
	if _, e := client.SetLabels(ctx, "org/Project", "app", 7, []string{"keep", "new"}); e != nil {
		t.Fatal(e)
	}
	if strings.Join(created, ",") != "new" || strings.Join(deleted, ",") != "remove" {
		t.Fatal("wrong native label changes", created, deleted)
	}
}

func TestGitLabMergeAndMetadataUseNativeIDsAndSourcePrecondition(t *testing.T) {
	ctx := context.Background()
	updates := []map[string]any{}
	merged := false
	client := fakeGitlab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users" && r.URL.Query().Get("username") == "new-reviewer" {
			glRespond(w, []map[string]any{{"id": 73, "username": "new-reviewer"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/merge_requests/7") && r.Method == "GET" {
			glRespond(w, gitlabMR())
			return
		}
		if strings.HasSuffix(r.URL.Path, "/merge_requests/7") && r.Method == "PUT" {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			updates = append(updates, input)
			glRespond(w, gitlabMR())
			return
		}
		if strings.HasSuffix(r.URL.Path, "/merge_requests/7/merge") {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			if input["sha"] != glHead || input["squash"] != true || input["should_remove_source_branch"] != false {
				t.Errorf("wrong guarded merge %+v", input)
			}
			merged = true
			mr := gitlabMR()
			mr["state"] = "merged"
			mr["merge_commit_sha"] = strings.Repeat("d", 40)
			glRespond(w, mr)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}, Options{})
	if _, e := client.RequestReviewers(ctx, "team/sub", "app", 7, []string{"new-reviewer"}); e != nil {
		t.Fatal(e)
	}
	if updates[0]["reviewer_ids"].([]any)[0] != float64(73) {
		t.Fatal("reviewer username was not resolved to a GitLab ID")
	}
	if _, e := client.MergePullRequest(ctx, "team/sub", "app", 7, "squash", glBase); e == nil || merged {
		t.Fatal("stale merge accepted")
	}
	if result, e := client.MergePullRequest(ctx, "team/sub", "app", 7, "squash", glHead); e != nil || !result.Merged || !merged {
		t.Fatal(result, e)
	}
}
