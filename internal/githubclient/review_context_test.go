package githubclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func TestReviewContextCapturesDiffMergeBaseAndRejectsMovingBase(t *testing.T) {
	head, base, ancestor := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/compare/"):
			respond(w, map[string]any{"merge_base_commit": map[string]string{"sha": ancestor}})
		case strings.HasSuffix(r.URL.Path, "/files"):
			respond(w, []map[string]any{{"filename": "source.ts", "status": "modified", "patch": "@@ -1 +1 @@\n-old\n+new"}})
		default:
			value := fixturePR()
			value["head"] = map[string]string{"sha": head}
			value["base"] = map[string]string{"sha": base}
			respond(w, value)
		}
	})
	value, err := client.ReviewChanges(context.Background(), "owner", "repo", 12, head, base)
	if err != nil || value.MergeBaseSHA != ancestor || len(value.Files) != 1 {
		t.Fatal(value, err)
	}
	if _, err := client.ReviewChanges(context.Background(), "owner", "repo", 12, head, ancestor); err == nil {
		t.Fatal("stale base accepted")
	}
}
func TestReadReviewFilePinsCommitAndRejectsUnsafePathsAndBinaryContent(t *testing.T) {
	sha := strings.Repeat("a", 40)
	binary := false
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != sha {
			t.Error("unversioned source read")
		}
		value := "source\n"
		if binary {
			value = "\x00binary"
		}
		respond(w, map[string]any{"type": "file", "size": len(value), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(value)), "download_url": "https://untrusted.invalid/private"})
	})
	if source, err := client.ReadFileAtCommit(context.Background(), "owner", "repo", "src/source.ts", sha); err != nil || source != "source\n" {
		t.Fatal(source, err)
	}
	for _, path := range []string{"../secret", "/secret", ".git/config", "a/../secret", "a\\secret", "a\x00b"} {
		if _, err := client.ReadFileAtCommit(context.Background(), "owner", "repo", path, sha); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	binary = true
	if _, err := client.ReadFileAtCommit(context.Background(), "owner", "repo", "binary.dat", sha); err == nil {
		t.Fatal("binary source exposed as text")
	}
}
