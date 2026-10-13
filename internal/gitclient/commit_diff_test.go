package gitclient

import (
	"context"
	"strings"
	"testing"
)

func TestCommitDiffHandlesRootAndPreservesDirtyIndexAndCheckout(t *testing.T) {
	client, root := testRepository(t, Options{})
	first := initialCommit(t, client, root)
	files, err := client.CommitDiff(context.Background(), root, first)
	if err != nil || len(files) != 1 || files[0].Path != "review.txt" || files[0].Status != "added" {
		t.Fatalf("Root commit diff: %#v, %v", files, err)
	}
	writeFile(t, root, "review.txt", "second commit\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "Second")
	second := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "review.txt", "staged local content\n")
	testGit(t, root, "add", "review.txt")
	writeFile(t, root, "review.txt", "unstaged local content\n")
	before := testGit(t, root, "status", "--porcelain=v2")
	files, err = client.CommitDiff(context.Background(), root, second)
	if err != nil || len(files) != 1 || !strings.Contains(files[0].RawPatch, "+second commit") || strings.Contains(files[0].RawPatch, "local content") {
		t.Fatalf("Commit diff used dirty content: %#v, %v", files, err)
	}
	if testGit(t, root, "status", "--porcelain=v2") != before || testGit(t, root, "rev-parse", "HEAD") != second {
		t.Fatal("Inspecting a commit changed the checkout or index")
	}
	if _, err := client.CommitDiff(context.Background(), root, "--output=other-file"); err == nil {
		t.Fatal("Accepted an option as a commit revision")
	}
}

func TestMergeCommitDiffUsesFirstParent(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	testGit(t, root, "switch", "-c", "topic")
	writeFile(t, root, "topic.txt", "topic work\n")
	testGit(t, root, "add", "topic.txt")
	testGit(t, root, "commit", "-m", "Topic")
	testGit(t, root, "switch", "main")
	writeFile(t, root, "main.txt", "main work\n")
	testGit(t, root, "add", "main.txt")
	testGit(t, root, "commit", "-m", "Main")
	testGit(t, root, "merge", "--no-ff", "-m", "Merge topic", "topic")
	files, err := client.CommitDiff(context.Background(), root, "HEAD")
	if err != nil || len(files) != 1 || files[0].Path != "topic.txt" {
		t.Fatalf("Merge diff did not compare with the first parent: %#v, %v", files, err)
	}
}
