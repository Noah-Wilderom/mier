package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreesCreateOpenLockAndRemoveKeepBranchesAndMainFiles(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "linked worktree")
	writeFile(t, root, "review.txt", "main work stays here\n")
	if err := c.MutateWorktree(ctx, root, WorktreeRequest{Action: "add", Path: target, Mode: "new", Branch: "feature/worktree", StartSHA: head, ExpectedHead: head, ExpectedBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	items, err := c.Worktrees(ctx, root)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	linked := items[1]
	if linked.Branch != "feature/worktree" || linked.Primary || linked.Current || linked.SHA != head {
		t.Fatal(linked)
	}
	opened, err := c.Snapshot(ctx, target)
	if err != nil || opened.CurrentBranch != "feature/worktree" {
		t.Fatal(opened, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(data) != "main work stays here\n" {
		t.Fatal("main working file changed")
	}
	request := WorktreeRequest{Action: "lock", Path: target, Token: linked.Token, Reason: "Keep while reviewing"}
	if err = c.MutateWorktree(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	items, _ = c.Worktrees(ctx, root)
	linked = items[1]
	if !linked.Locked || linked.LockReason != request.Reason {
		t.Fatal(linked)
	}
	if err = c.MutateWorktree(ctx, root, WorktreeRequest{Action: "remove", Path: target, Token: linked.Token}); err == nil {
		t.Fatal("removed locked worktree")
	}
	if err = c.MutateWorktree(ctx, root, WorktreeRequest{Action: "unlock", Path: target, Token: request.Token}); err == nil {
		t.Fatal("accepted stale token")
	}
	if err = c.MutateWorktree(ctx, root, WorktreeRequest{Action: "unlock", Path: target, Token: linked.Token}); err != nil {
		t.Fatal(err)
	}
	items, _ = c.Worktrees(ctx, root)
	linked = items[1]
	if err = c.MutateWorktree(ctx, root, WorktreeRequest{Action: "remove", Path: target, Token: linked.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("worktree directory remains")
	}
	if sha := testGit(t, root, "rev-parse", "refs/heads/feature/worktree"); sha != head {
		t.Fatal("removed worktree lost its branch", sha)
	}
}
func TestWorktreeRemovalRejectsDirtyCurrentPrimaryAndUnregisteredPaths(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "linked")
	if err := c.MutateWorktree(ctx, root, WorktreeRequest{Action: "add", Path: target, Mode: "detached", StartSHA: head, ExpectedHead: head, ExpectedBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	items, _ := c.Worktrees(ctx, root)
	primary, linked := items[0], items[1]
	if err := c.MutateWorktree(ctx, root, WorktreeRequest{Action: "remove", Path: root, Token: primary.Token}); err == nil {
		t.Fatal("primary removed")
	}
	if err := c.MutateWorktree(ctx, target, WorktreeRequest{Action: "remove", Path: target, Token: linked.Token}); err == nil {
		t.Fatal("current worktree removed")
	}
	writeFile(t, target, "new.txt", "keep local work")
	if err := c.MutateWorktree(ctx, root, WorktreeRequest{Action: "remove", Path: target, Token: linked.Token}); err == nil {
		t.Fatal("dirty worktree removed")
	}
	if err := c.MutateWorktree(ctx, root, WorktreeRequest{Action: "remove", Path: t.TempDir(), Token: linked.Token}); err == nil {
		t.Fatal("unregistered path accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(target, "new.txt")); string(data) != "keep local work" {
		t.Fatal("work lost")
	}
}
func TestWorktreeCreationPreservesExistingPathsAndRejectsNestedDirectories(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	request := WorktreeRequest{Action: "add", Mode: "detached", StartSHA: head, ExpectedHead: head, ExpectedBranch: "main"}
	for _, target := range []string{root, filepath.Join(root, "nested"), filepath.Join(root, ".git", "nested")} {
		request.Path = target
		if err := c.MutateWorktree(ctx, root, request); err == nil {
			t.Fatal("unsafe target accepted", target)
		}
	}
}
