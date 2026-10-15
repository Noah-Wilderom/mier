package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMergePreflightPredictsConflictsWithoutChangingFilesOrIndex(t *testing.T) {
	c, root, source, head := conflictingBranches(t)
	before := testGit(t, root, "ls-files", "--stage")
	working, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	preview, err := c.PreviewMerge(context.Background(), root, MergeBranchRequest{Branch: "feature", ExpectedSource: source, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil || preview.Clean || preview.FastForward || len(preview.Conflicts) != 1 || preview.Conflicts[0] != "review.txt" {
		t.Fatal(preview, err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	if string(after) != string(working) || testGit(t, root, "ls-files", "--stage") != before || testGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("preflight changed repository work")
	}
	if op := snapshot(t, c, root).Operation; op != nil {
		t.Fatal("preflight started a merge", op)
	}
}
func TestMergePreflightFastForwardAndStaleSource(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "new.txt", "new\n")
	testGit(t, root, "add", "new.txt")
	testGit(t, root, "commit", "-m", "Feature")
	source := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "main")
	request := MergeBranchRequest{Branch: "feature", ExpectedSource: source, ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewMerge(context.Background(), root, request)
	if err != nil || !preview.Clean || !preview.FastForward || len(preview.Conflicts) != 0 {
		t.Fatal(preview, err)
	}
	request.ExpectedSource = head
	if _, err = c.PreviewMerge(context.Background(), root, request); err == nil {
		t.Fatal("stale source previewed")
	}
}
