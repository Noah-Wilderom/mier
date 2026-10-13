package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAndCheckoutPreservesUnbornAndDirtyWork(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	writeFile(t, root, "review.txt", "unfinished work\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateAndCheckoutBranch(ctx, root, "feature/first"); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, client, root)
	if state.CurrentBranch != "feature/first" || state.HasHead || !findChange(t, state, "review.txt").Staged {
		t.Fatalf("unborn state changed: %+v", state)
	}
	if _, err := client.Commit(ctx, root, "First commit"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "uncommitted revision\n")
	if err := client.CreateAndCheckoutBranch(ctx, root, "feature/second"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, root).CurrentBranch != "feature/second" {
		t.Fatal("new branch was not selected")
	}
	got, err := os.ReadFile(filepath.Join(root, "review.txt"))
	if err != nil || string(got) != "uncommitted revision\n" {
		t.Fatalf("dirty work lost: %q %v", got, err)
	}
	if err := client.CreateAndCheckoutBranch(ctx, root, "feature/first"); err == nil {
		t.Fatal("an existing branch was replaced")
	}
	if err := client.CreateAndCheckoutBranch(ctx, root, "--force"); err == nil {
		t.Fatal("option-like branch was accepted")
	}
}

func TestDeleteBranchPreservesUnmergedAndCheckedOutWork(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	if err := client.CreateAndCheckoutBranch(ctx, root, "feature/work"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "feature.txt", "a new change\n")
	if err := client.Stage(ctx, root, []string{"feature.txt"}); err != nil {
		t.Fatal(err)
	}
	sha, err := client.Commit(ctx, root, "Unmerged work")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteBranch(ctx, root, "feature/work"); err == nil {
		t.Fatal("checked-out branch was deleted")
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteBranch(ctx, root, "feature/work"); err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("unmerged work was not protected: %v", err)
	}
	if got := testGit(t, root, "rev-parse", "feature/work"); got != sha {
		t.Fatalf("branch moved: %s", got)
	}
	testGit(t, root, "merge", "--ff-only", "feature/work")
	if err := client.DeleteBranch(ctx, root, "feature/work"); err != nil {
		t.Fatal(err)
	}
	for _, branch := range snapshot(t, client, root).Branches {
		if branch.Name == "feature/work" {
			t.Fatal("merged branch was not deleted")
		}
	}
}

func TestRenameBranchKeepsCommitAndTracking(t *testing.T) {
	client, root := testRepository(t, Options{})
	sha := initialCommit(t, client, root)
	testGit(t, root, "config", "branch.main.remote", "origin")
	testGit(t, root, "config", "branch.main.merge", "refs/heads/trunk")
	if err := client.RenameBranch(context.Background(), root, "main", "feature/renamed"); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, client, root)
	if state.CurrentBranch != "feature/renamed" || state.HeadSHA != sha {
		t.Fatalf("rename changed history: %+v", state)
	}
	if got := testGit(t, root, "config", "branch.feature/renamed.merge"); got != "refs/heads/trunk" {
		t.Fatal("rename lost tracking")
	}
	if err := client.RenameBranch(context.Background(), root, "feature/renamed", "--force"); err == nil {
		t.Fatal("option-like rename was accepted")
	}
}
