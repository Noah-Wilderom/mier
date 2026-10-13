package githubclient

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCommitDiffReadsPaginatedFilesAndPreservesMissingPatches(t *testing.T) {
	sha := strings.Repeat("a", 40)
	calls := 0
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/owner/repo/commits/"+sha || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("Unexpected commit endpoint: %s", r.URL)
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/ignored>; rel="next"`)
			respond(w, map[string]any{"sha": sha, "files": []map[string]any{{"filename": "first.ts", "status": "modified", "patch": "@@ -1 +1 @@\n-old\n+new"}}})
		} else {
			respond(w, map[string]any{"sha": sha, "files": []map[string]any{{"filename": "image.png", "status": "added", "additions": 1}}})
		}
	})
	result, err := client.GetCommitDiff(context.Background(), "owner", "repo", sha)
	if err != nil || calls != 2 || len(result.Files) != 2 || result.Files[0].Path != "first.ts" || result.Files[1].Patch != "" {
		t.Fatalf("Paginated commit diff: %#v, %v, calls=%d", result, err, calls)
	}
}

func TestCommitDiffRejectsMutableRefsAndWrongReturnedCommit(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{"sha": strings.Repeat("b", 40), "files": []any{}})
	})
	for _, ref := range []string{"main", "../secrets", "", strings.Repeat("z", 40), strings.Repeat("a", 40)} {
		if _, err := client.GetCommitDiff(context.Background(), "owner", "repo", ref); err == nil {
			t.Errorf("Accepted mutable/invalid or mismatched commit: %q", ref)
		}
	}
}
