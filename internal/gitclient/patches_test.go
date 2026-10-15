package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchExportPreviewApplyAndReversePreserveIndex(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "review.txt", "new first\nsecond\n")
	patch, err := c.ExportPatch(ctx, root, ExportPatchRequest{Mode: "working"})
	if err != nil || !strings.Contains(patch, "+new first") {
		t.Fatal(patch, err)
	}
	testGit(t, root, "restore", "review.txt")
	request := ApplyPatchRequest{Patch: patch, Mode: "working", ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewPatch(ctx, root, request)
	if err != nil || len(preview.Paths) != 1 || preview.Paths[0] != "review.txt" {
		t.Fatal(preview, err)
	}
	request.Token = preview.Token
	writeFile(t, root, "review.txt", "external\nsecond\n")
	if _, err = c.ApplyPatch(ctx, root, request); err == nil {
		t.Fatal("stale patch applied")
	}
	testGit(t, root, "restore", "review.txt")
	if _, err = c.ApplyPatch(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(data) != "new first\nsecond\n" {
		t.Fatal(string(data))
	}
	if diff := testGit(t, root, "diff", "--cached"); diff != "" {
		t.Fatal("working patch staged itself", diff)
	}
	request.Reverse = true
	preview, err = c.PreviewPatch(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Token = preview.Token
	if _, err = c.ApplyPatch(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if changes := snapshot(t, c, root).Changes; len(changes) != 0 {
		t.Fatal(changes)
	}
}
func TestMailboxPatchRestoresAuthorAndCreatesRealCommit(t *testing.T) {
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "source")
	writeFile(t, root, "new.txt", "feature\n")
	testGit(t, root, "add", "new.txt")
	testGit(t, root, "commit", "-m", "Feature patch", "--author=Patch Author <patch@example.invalid>")
	source := testGit(t, root, "rev-parse", "HEAD")
	patch, err := c.ExportPatch(ctx, root, ExportPatchRequest{Mode: "commit", SHA: source})
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "switch", "main")
	request := ApplyPatchRequest{Patch: patch, Mode: "commits", ExpectedHead: base, ExpectedBranch: "main"}
	preview, err := c.PreviewPatch(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Token = preview.Token
	result, err := c.ApplyPatch(ctx, root, request)
	if err != nil || result.Operation != nil {
		t.Fatal(result, err)
	}
	if author := testGit(t, root, "show", "-s", "--format=%ae", "HEAD"); author != "patch@example.invalid" {
		t.Fatal(author)
	}
	if message := testGit(t, root, "show", "-s", "--format=%s", "HEAD"); message != "Feature patch" {
		t.Fatal(message)
	}
}
func TestPatchRejectsTraversalAndOversizedBinaryExpansion(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	for _, patch := range []string{"diff --git a/../escape b/../escape\n--- /dev/null\n+++ b/../escape\n@@ -0,0 +1 @@\n+bad\n", "diff --git a/x b/x\nnew file mode 100644\nGIT binary patch\nliteral 999999999999999999999999999999999999\n"} {
		if _, err := c.PreviewPatch(ctx, root, ApplyPatchRequest{Patch: patch, Mode: "working", ExpectedHead: head, ExpectedBranch: "main"}); err == nil {
			t.Fatal("unsafe patch accepted")
		}
	}
}
func TestMailboxConflictCanBeSkippedOrAborted(t *testing.T) {
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "source")
	writeFile(t, root, "review.txt", "source\nsecond\n")
	testGit(t, root, "commit", "-am", "Mailbox change")
	sha := testGit(t, root, "rev-parse", "HEAD")
	patch, err := c.ExportPatch(ctx, root, ExportPatchRequest{Mode: "commit", SHA: sha})
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "switch", "main")
	writeFile(t, root, "review.txt", "main\nsecond\n")
	testGit(t, root, "commit", "-am", "Conflicting main")
	head := testGit(t, root, "rev-parse", "HEAD")
	if head == base {
		t.Fatal("missing commit")
	}
	for _, action := range []string{"skip", "abort"} {
		request := ApplyPatchRequest{Patch: patch, Mode: "commits", ExpectedHead: head, ExpectedBranch: "main"}
		preview, err := c.PreviewPatch(ctx, root, request)
		if err != nil {
			t.Fatal(err)
		}
		request.Token = preview.Token
		result, err := c.ApplyPatch(ctx, root, request)
		if err != nil || result.Operation == nil || result.Operation.Kind != "am" || !result.Operation.CanSkip || result.Operation.Message != "Mailbox change" {
			t.Fatal(result, err)
		}
		result, err = c.ControlOperation(ctx, root, result.Operation.Token, action)
		if err != nil || result.Operation != nil || result.HeadSHA != head {
			t.Fatal(result, err)
		}
	}
}
