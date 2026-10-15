package githubclient

import (
	"context"
	"net/http"
	"testing"
)

func stackPR(number int, head, base, repo, status string) PullRequestSummary {
	return PullRequestSummary{Number: number, Title: head, Head: Branch{Ref: head, SHA: head + "-sha", Repository: repo}, Base: Branch{Ref: base, SHA: base + "-sha", Repository: "team/app"}, Status: status}
}
func TestStackRelationshipsExcludeForkNameCollisionsAndChooseCurrentParent(t *testing.T) {
	parent := stackPR(1, "api", "main", "team/app", "open")
	current := stackPR(2, "ui", "api", "team/app", "open")
	child := stackPR(3, "polish", "ui", "team/app", "draft")
	fork := stackPR(4, "api", "main", "fork/app", "open")
	old := stackPR(5, "api", "other", "team/app", "closed")
	old.Head.SHA = "old-sha"
	result := buildPullRequestStack(current, []PullRequestSummary{parent, child, fork, old}, "team/app")
	if len(result.Nodes) != 3 || len(result.Warnings) != 0 || result.Nodes[0].Number != 1 || result.Nodes[1].Parents[0] != 1 || result.Nodes[2].Parents[0] != 2 || result.Nodes[2].Depth != 2 {
		t.Fatal(result)
	}
}
func TestStackRetargetChecksReviewedRevisionBeforeMutation(t *testing.T) {
	mutations := 0
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		pr := fixturePR()
		if r.Method == http.MethodPatch {
			mutations++
			pr["base"] = map[string]any{"ref": "parent", "sha": "base-sha"}
		}
		respond(w, pr)
	})
	if _, err := client.RetargetPullRequest(context.Background(), "owner", "repo", 12, RetargetRequest{Base: "parent", ExpectedBase: "main", ExpectedHead: "stale"}); err == nil {
		t.Fatal("stale retarget accepted")
	}
	if mutations != 0 {
		t.Fatal("stale request mutated")
	}
	pr, err := client.RetargetPullRequest(context.Background(), "owner", "repo", 12, RetargetRequest{Base: "parent", ExpectedBase: "main", ExpectedHead: "head-sha"})
	if err != nil || pr.Base.Ref != "parent" || mutations != 1 {
		t.Fatal(pr, mutations, err)
	}
}
