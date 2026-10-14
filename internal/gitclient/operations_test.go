package gitclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func conflictingBranches(t *testing.T) (*Client, string, string, string) {
	t.Helper()
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "review.txt", "incoming\nsecond\n")
	testGit(t, root, "commit", "-am", "Incoming change")
	source := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "main")
	writeFile(t, root, "review.txt", "current\nsecond\n")
	testGit(t, root, "commit", "-am", "Current change")
	return c, root, source, testGit(t, root, "rev-parse", "HEAD")
}

func TestCherryPickGuardsAndSuccess(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "feature.txt", "feature\n")
	testGit(t, root, "add", "feature.txt")
	testGit(t, root, "commit", "-m", "Feature", "--author=Original <original@example.invalid>")
	source := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "main")
	request := CherryPickRequest{SHA: source, ExpectedHead: head, ExpectedBranch: "main"}
	writeFile(t, root, "untracked.txt", "keep this")
	if _, err := c.CherryPick(context.Background(), root, request); err == nil {
		t.Fatal("dirty tree accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "untracked.txt")); string(data) != "keep this" {
		t.Fatal("local work was modified")
	}
	os.Remove(filepath.Join(root, "untracked.txt"))
	for _, bad := range []CherryPickRequest{{SHA: "--help", ExpectedHead: head, ExpectedBranch: "main"}, {SHA: source, ExpectedHead: source, ExpectedBranch: "main"}, {SHA: source, ExpectedHead: head, ExpectedBranch: "feature"}, {SHA: source, ExpectedHead: head, ExpectedBranch: "main", Mainline: 1}} {
		if _, err := c.CherryPick(context.Background(), root, bad); err == nil {
			t.Fatalf("unsafe request accepted: %#v", bad)
		}
	}
	result, err := c.CherryPick(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != nil || result.HeadSHA == head || result.HeadSHA == source {
		t.Fatalf("invalid cherry pick: %#v", result)
	}
	if author := testGit(t, root, "show", "-s", "--format=%ae", "HEAD"); author != "original@example.invalid" {
		t.Fatal(author)
	}
	if body := testGit(t, root, "show", "-s", "--format=%B", "HEAD"); !strings.Contains(body, source) {
		t.Fatal("missing original commit attribution", body)
	}
	if len(snapshot(t, c, root).Changes) != 0 {
		t.Fatal("successful cherry pick left changes")
	}
	empty, err := c.CherryPick(context.Background(), root, CherryPickRequest{SHA: source, ExpectedHead: result.HeadSHA, ExpectedBranch: "main"})
	if err != nil || empty.Operation == nil || !empty.Operation.Empty {
		t.Fatal("empty pick not surfaced", empty, err)
	}
	if _, err := c.ControlOperation(context.Background(), root, empty.Operation.Token, "continue"); err == nil {
		t.Fatal("empty pick continued")
	}
	skipped, err := c.ControlOperation(context.Background(), root, empty.Operation.Token, "skip")
	if err != nil || skipped.Operation != nil || skipped.HeadSHA != result.HeadSHA {
		t.Fatal("empty pick not skipped safely", skipped, err)
	}
}

func TestConflictResolutionAndContinue(t *testing.T) {
	c, root, source, head := conflictingBranches(t)
	ctx := context.Background()
	result, err := c.CherryPick(ctx, root, CherryPickRequest{SHA: source, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation == nil || result.Operation.Conflicts != 1 {
		t.Fatalf("conflict not returned: %#v", result)
	}
	op := snapshot(t, c, root).Operation
	if op == nil || op.Kind != "cherry-pick" {
		t.Fatal(op)
	}
	file, err := c.Conflict(ctx, root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if file.Base.Content != "first\nsecond\n" || file.Current.Content != "current\nsecond\n" || file.Incoming.Content != "incoming\nsecond\n" || !file.Editable {
		t.Fatalf("wrong conflict versions: %#v", file)
	}
	if err := c.Stage(ctx, root, []string{"review.txt"}); err == nil {
		t.Fatal("unresolved markers staged")
	}
	if err := c.ResolveConflict(ctx, root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "result", Content: file.Working}); err == nil {
		t.Fatal("unresolved markers saved")
	}
	if _, err := c.ControlOperation(ctx, root, op.Token, "continue"); err == nil {
		t.Fatal("continued before resolving")
	}
	writeFile(t, root, "review.txt", "external editor change\n")
	if err := c.ResolveConflict(ctx, root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "result", Content: "overwrite\n"}); err == nil {
		t.Fatal("stale result overwrote external editor")
	}
	if content, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(content) != "external editor change\n" {
		t.Fatal("external changes lost")
	}
	file, err = c.Conflict(ctx, root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveConflict(ctx, root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "result", Content: "both preserved\nsecond\n"}); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, c, root)
	if state.Operation == nil || state.Operation.Conflicts != 0 || !state.Changes[0].Staged {
		t.Fatalf("not resolved/staged: %#v", state)
	}
	if _, err := c.ControlOperation(ctx, root, "stale-token", "abort"); err == nil {
		t.Fatal("stale abort accepted")
	}
	if _, err := c.Commit(ctx, root, "accidental commit"); err == nil {
		t.Fatal("ordinary commit bypassed sequencer UI")
	}
	result, err = c.ControlOperation(ctx, root, op.Token, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != nil || result.HeadSHA == head {
		t.Fatal("operation did not finish")
	}
	if content, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(content) != "both preserved\nsecond\n" {
		t.Fatal("resolution changed")
	}
}

func TestAbortMergeAndDetectLinkedWorktreeOperation(t *testing.T) {
	c, root, source, head := conflictingBranches(t)
	ctx := context.Background()
	result, err := c.MergeBranch(ctx, root, MergeBranchRequest{Branch: "feature", ExpectedSource: source, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil || result.Operation == nil {
		t.Fatal(result, err)
	}
	if _, err := c.ControlOperation(ctx, root, result.Operation.Token, "abort"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(data) != "current\nsecond\n" {
		t.Fatal("abort did not restore branch")
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	testGit(t, root, "worktree", "add", "-b", "linked", worktree, "main")
	result, err = c.CherryPick(ctx, worktree, CherryPickRequest{SHA: source, ExpectedHead: head, ExpectedBranch: "linked"})
	if err != nil || result.Operation == nil {
		t.Fatal("worktree state not detected", err)
	}
	file, err := c.Conflict(ctx, worktree, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveConflict(ctx, worktree, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "incoming"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ControlOperation(ctx, worktree, result.Operation.Token, "continue"); err != nil {
		t.Fatal(err)
	}
	if testGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("primary worktree branch changed")
	}
}

func TestDeleteModifyAndBinaryConflict(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted incoming", true: "binary incoming"}[binary], func(t *testing.T) {
			c, root := testRepository(t, Options{})
			ctx := context.Background()
			initialCommit(t, c, root)
			testGit(t, root, "switch", "-c", "feature")
			if binary {
				writeFile(t, root, "review.txt", "\x00incoming")
				testGit(t, root, "commit", "-am", "binary incoming")
			} else {
				testGit(t, root, "rm", "review.txt")
				testGit(t, root, "commit", "-m", "Delete file")
			}
			source := testGit(t, root, "rev-parse", "HEAD")
			testGit(t, root, "switch", "main")
			if binary {
				writeFile(t, root, "review.txt", "\x00current")
			} else {
				writeFile(t, root, "review.txt", "current\n")
			}
			testGit(t, root, "commit", "-am", "Current")
			result, err := c.MergeBranch(ctx, root, MergeBranchRequest{Branch: "feature", ExpectedSource: source, ExpectedHead: testGit(t, root, "rev-parse", "HEAD"), ExpectedBranch: "main"})
			if err != nil || result.Operation == nil {
				t.Fatal(result, err)
			}
			file, err := c.Conflict(ctx, root, "review.txt")
			if err != nil {
				t.Fatal(err)
			}
			if binary && file.Editable {
				t.Fatal("binary presented as editable text")
			}
			if err := c.ResolveConflict(ctx, root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "incoming"}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ControlOperation(ctx, root, result.Operation.Token, "continue"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "review.txt"))
			if binary {
				if err != nil || string(data) != "\x00incoming" {
					t.Fatal("binary bytes changed", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("deletion not applied", err)
			}
		})
	}
}

func TestExternalRebaseConflictUsesIndexStageLabels(t *testing.T) {
	c, root, _, _ := conflictingBranches(t)
	command := exec.Command("git", "-C", root, "rebase", "feature")
	if err := command.Run(); err == nil {
		t.Fatal("expected a rebase conflict")
	}
	state := snapshot(t, c, root)
	if state.Operation == nil || state.Operation.Kind != "rebase" {
		t.Fatal(state.Operation)
	}
	file, err := c.Conflict(context.Background(), root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if file.Current.Content != "incoming\nsecond\n" || file.Incoming.Content != "current\nsecond\n" || !strings.Contains(file.IncomingLabel, "Replayed commit") {
		t.Fatalf("rebase versions mislabeled: %#v", file)
	}
	if _, err := c.ControlOperation(context.Background(), root, state.Operation.Token, "abort"); err != nil {
		t.Fatal(err)
	}
}

func TestPartialStagingPreservesWorkingTreeAndRejectsStaleDiff(t *testing.T) {
	c, root := testRepository(t, Options{})
	ctx := context.Background()
	old := make([]string, 45)
	for i := range old {
		old[i] = "unchanged line"
	}
	writeFile(t, root, "review.txt", strings.Join(old, "\n")+"\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "Initial")
	changed := append([]string{}, old...)
	changed[2] = "first edit"
	changed[35] = "second edit"
	working := strings.Join(changed, "\n") + "\n"
	writeFile(t, root, "review.txt", working)
	diff, err := c.Diff(ctx, root, "review.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StageHunk(ctx, root, StageHunkRequest{Path: "review.txt", Hunk: 0, ExpectedPatch: diff.RawPatch + "tampered"}); err == nil {
		t.Fatal("untrusted patch accepted")
	}
	if err := c.StageHunk(ctx, root, StageHunkRequest{Path: "review.txt", Hunk: 0, ExpectedPatch: diff.RawPatch}); err != nil {
		t.Fatal(err)
	}
	index := testGit(t, root, "show", ":review.txt")
	if !strings.Contains(index, "first edit") || strings.Contains(index, "second edit") {
		t.Fatal("wrong section staged", index)
	}
	staged, err := c.Diff(ctx, root, "review.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StageHunk(ctx, root, StageHunkRequest{Path: "review.txt", Staged: true, Hunk: 0, ExpectedPatch: staged.RawPatch}); err != nil {
		t.Fatal(err)
	}
	if index := testGit(t, root, "show", ":review.txt"); strings.Contains(index, "first edit") {
		t.Fatal("section not unstaged")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "review.txt")); string(data) != working {
		t.Fatal("working tree altered by partial staging")
	}
}
