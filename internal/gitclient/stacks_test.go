package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func stackAction(t *testing.T, c *Client, root string, request StackBranchRequest) {
	t.Helper()
	state := snapshot(t, c, root)
	stacks, err := c.BranchStacks(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedHead = state.HeadSHA
	request.ExpectedBranch = state.CurrentBranch
	request.Token = stacks.Token
	if err = c.MutateStackBranch(context.Background(), root, request); err != nil {
		t.Fatal(err)
	}
}
func TestBranchStackRestackUpdatesForkPointAndRejectsCycles(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "parent")
	writeFile(t, root, "parent.txt", "first\n")
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "Parent first")
	base := testGit(t, root, "rev-parse", "HEAD")
	stackAction(t, c, root, StackBranchRequest{Action: "create", Name: "child"})
	writeFile(t, root, "child.txt", "child\n")
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "Child first")
	child := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "parent")
	writeFile(t, root, "parent.txt", "second\n")
	testGit(t, root, "commit", "-am", "Parent second")
	parent := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "child")
	stacks, _ := c.BranchStacks(ctx, root)
	found := false
	for _, item := range stacks.Branches {
		if item.Name == "child" {
			found = item.NeedsRestack && item.BaseSHA == base
		}
	}
	if !found {
		t.Fatal(stacks)
	}
	request := RebasePreviewRequest{Onto: parent, Upstream: base, ExpectedHead: child, ExpectedBranch: "child"}
	preview, err := c.PreviewRebase(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Commits) != 1 {
		t.Fatal(preview)
	}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, StackToken: stacks.Token, Steps: []RebaseStep{{SHA: child, Action: "pick"}}})
	if err != nil || result.Operation != nil || result.HeadSHA == child {
		t.Fatal(result, err)
	}
	if saved := testGit(t, root, "config", "--get", "branch.child.mier-stack-base"); saved != parent {
		t.Fatal("fork point not updated", saved)
	}
	testGit(t, root, "merge-base", "--is-ancestor", parent, "child")
	testGit(t, root, "switch", "parent")
	stacks, _ = c.BranchStacks(ctx, root)
	if err = c.MutateStackBranch(ctx, root, StackBranchRequest{Action: "link", Parent: "child", BaseSHA: base, ExpectedHead: parent, ExpectedBranch: "parent", Token: stacks.Token}); err == nil {
		t.Fatal("stack cycle accepted")
	}
}
func TestStackRestackAbortKeepsOriginalForkPoint(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "parent")
	base := testGit(t, root, "rev-parse", "HEAD")
	stackAction(t, c, root, StackBranchRequest{Action: "create", Name: "child"})
	writeFile(t, root, "review.txt", "child\nsecond\n")
	testGit(t, root, "commit", "-am", "Child")
	child := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "parent")
	writeFile(t, root, "review.txt", "parent\nsecond\n")
	testGit(t, root, "commit", "-am", "Parent")
	parent := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "child")
	stacks, _ := c.BranchStacks(ctx, root)
	request := RebasePreviewRequest{Onto: parent, Upstream: base, ExpectedHead: child, ExpectedBranch: "child"}
	preview, _ := c.PreviewRebase(ctx, root, request)
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, StackToken: stacks.Token, Steps: []RebaseStep{{SHA: child, Action: "pick"}}})
	if err != nil || result.Operation == nil {
		t.Fatal(result, err)
	}
	result, err = c.ControlOperation(ctx, root, result.Operation.Token, "abort")
	if err != nil || result.HeadSHA != child {
		t.Fatal(result, err)
	}
	if saved := testGit(t, root, "config", "--get", "branch.child.mier-stack-base"); saved != base {
		t.Fatal("abort advanced stack base", saved)
	}
	// A second attempt resolves the actual conflict and records the parent on Continue.
	stacks, _ = c.BranchStacks(ctx, root)
	preview, _ = c.PreviewRebase(ctx, root, request)
	result, err = c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, StackToken: stacks.Token, Steps: []RebaseStep{{SHA: child, Action: "pick"}}})
	if err != nil || result.Operation == nil {
		t.Fatal(result, err)
	}
	file, err := c.Conflict(ctx, root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ResolveConflict(ctx, root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "incoming"}); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, c, root)
	result, err = c.ControlOperation(ctx, root, state.Operation.Token, "continue")
	if err != nil || result.Operation != nil {
		t.Fatal(result, err)
	}
	if saved := testGit(t, root, "config", "--get", "branch.child.mier-stack-base"); saved != parent {
		t.Fatal("continue did not update base", saved)
	}
}
func TestStackPublishUsesRemoteLeaseAndRejectsChangedPreview(t *testing.T) {
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.Mkdir(remote, 0755); err != nil {
		t.Fatal(err)
	}
	testGit(t, remote, "init", "--bare")
	testGit(t, root, "remote", "add", "origin", remote)
	request := StackPushRequest{Branch: "main", Remote: "origin", ExpectedHead: base, ExpectedBranch: "main"}
	preview, err := c.PreviewStackPush(ctx, root, request)
	if err != nil || preview.RemoteSHA != "" {
		t.Fatal(preview, err)
	}
	request.Token = preview.Token
	if err = c.PushStackBranch(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "new\nsecond\n")
	testGit(t, root, "commit", "-am", "Second")
	newer := testGit(t, root, "rev-parse", "HEAD")
	request.ExpectedHead = newer
	preview, err = c.PreviewStackPush(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Token = preview.Token
	if err = c.PushStackBranch(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "reset", "--hard", base)
	request.ExpectedHead = base
	preview, err = c.PreviewStackPush(ctx, root, request)
	if err != nil || !preview.Rewrites {
		t.Fatal(preview, err)
	}
	request.Token = preview.Token
	testGit(t, remote, "update-ref", "refs/heads/main", base)
	if err = c.PushStackBranch(ctx, root, request); err == nil {
		t.Fatal("changed remote accepted")
	}
	testGit(t, remote, "update-ref", "refs/heads/main", newer)
	preview, err = c.PreviewStackPush(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Token = preview.Token
	if err = c.PushStackBranch(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if testGit(t, remote, "rev-parse", "refs/heads/main") != base {
		t.Fatal("approved rewrite not published")
	}
}
func TestEmptyStackChildCanMoveToUpdatedParent(t *testing.T) {
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "parent")
	stackAction(t, c, root, StackBranchRequest{Action: "create", Name: "empty-child"})
	testGit(t, root, "switch", "parent")
	writeFile(t, root, "parent.txt", "new\n")
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "-m", "Parent")
	parent := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "empty-child")
	stacks, _ := c.BranchStacks(ctx, root)
	request := RebasePreviewRequest{Onto: parent, Upstream: base, ExpectedHead: base, ExpectedBranch: "empty-child"}
	preview, err := c.PreviewRebase(ctx, root, request)
	if err != nil || len(preview.Commits) != 0 {
		t.Fatal(preview, err)
	}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, StackToken: stacks.Token, Steps: []RebaseStep{}})
	if err != nil || result.Operation != nil || result.HeadSHA != parent {
		t.Fatal(result, err)
	}
}
