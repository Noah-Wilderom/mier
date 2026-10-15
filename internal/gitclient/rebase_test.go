package gitclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if handled, err := HandleGitEditor(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func rebaseRepository(t *testing.T) (*Client, string, string, string) {
	t.Helper()
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		writeFile(t, root, name, name+"\n")
		testGit(t, root, "add", name)
		testGit(t, root, "commit", "-m", name)
	}
	return c, root, base, testGit(t, root, "rev-parse", "HEAD")
}
func TestInteractiveRebaseRewordSquashDropAndValidation(t *testing.T) {
	c, root, base, head := rebaseRepository(t)
	ctx := context.Background()
	previewRequest := RebasePreviewRequest{Onto: base, Upstream: base, ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewRebase(ctx, root, previewRequest)
	if err != nil || len(preview.Commits) != 3 {
		t.Fatal(preview, err)
	}
	steps := []RebaseStep{{SHA: preview.Commits[0].SHA, Action: "reword", Message: "feat: combined work"}, {SHA: preview.Commits[1].SHA, Action: "squash"}, {SHA: preview.Commits[2].SHA, Action: "drop"}}
	bad := append([]RebaseStep{}, steps...)
	bad[1].SHA = bad[0].SHA
	if _, err = c.Rebase(ctx, root, RebaseRequest{Preview: previewRequest, Token: preview.Token, Steps: bad}); err == nil {
		t.Fatal("duplicate plan accepted")
	}
	if _, err = c.Rebase(ctx, root, RebaseRequest{Preview: previewRequest, Token: preview.Token + "stale", Steps: steps}); err == nil {
		t.Fatal("stale preview accepted")
	}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: previewRequest, Token: preview.Token, Steps: steps})
	if err != nil || result.Operation != nil {
		t.Fatal(result, err)
	}
	if subject := testGit(t, root, "show", "-s", "--format=%B", "HEAD"); subject != "feat: combined work\n\ntwo.txt" {
		t.Fatal("wrong reword/squash message", subject)
	}
	if count := testGit(t, root, "rev-list", "--count", base+"..HEAD"); count != "1" {
		t.Fatal(count)
	}
	if _, err = os.Stat(root + "/three.txt"); !os.IsNotExist(err) {
		t.Fatal("dropped commit still present")
	}
	if checkpoints, _ := c.Checkpoints(ctx, root); len(checkpoints) != 1 || checkpoints[0].SHA != head {
		t.Fatal(checkpoints)
	}
}
func TestRebaseEditAmendAndContinueRetainsLaterReword(t *testing.T) {
	c, root, base, head := rebaseRepository(t)
	ctx := context.Background()
	request := RebasePreviewRequest{Onto: base, Upstream: base, ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewRebase(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	steps := []RebaseStep{{SHA: preview.Commits[0].SHA, Action: "edit"}, {SHA: preview.Commits[1].SHA, Action: "reword", Message: "fix: later reword"}, {SHA: preview.Commits[2].SHA, Action: "fixup"}}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, Steps: steps})
	if err != nil || result.Operation == nil || !result.Operation.PausedForEdit {
		t.Fatal(result, err)
	}
	newHead, err := c.Amend(ctx, root, AmendRequest{Message: "feat: edited first", ExpectedHead: result.HeadSHA, OperationToken: result.Operation.Token})
	if err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, c, root)
	if state.HeadSHA != newHead || state.Operation == nil {
		t.Fatal(state)
	}
	result, err = c.ControlOperation(ctx, root, state.Operation.Token, "continue")
	if err != nil || result.Operation != nil {
		t.Fatal(result, err)
	}
	if message := testGit(t, root, "show", "-s", "--format=%s", "HEAD"); message != "fix: later reword" {
		t.Fatal("continued rebase lost planned message", message)
	}
	if count := testGit(t, root, "rev-list", "--count", base+"..HEAD"); count != "2" {
		t.Fatal(count)
	}
}
func TestRebaseConflictAbortRestoresBranch(t *testing.T) {
	c, root, source, head := conflictingBranches(t)
	ctx := context.Background()
	request := RebasePreviewRequest{Onto: source, ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewRebase(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	steps := []RebaseStep{}
	for _, commit := range preview.Commits {
		steps = append(steps, RebaseStep{SHA: commit.SHA, Action: "pick"})
	}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, Steps: steps})
	if err != nil || result.Operation == nil || result.Operation.Conflicts != 1 {
		t.Fatal(result, err)
	}
	result, err = c.ControlOperation(ctx, root, result.Operation.Token, "abort")
	if err != nil || result.Operation != nil || result.HeadSHA != head {
		t.Fatal(result, err)
	}
	if state := snapshot(t, c, root); state.CurrentBranch != "main" || len(state.Changes) != 0 {
		t.Fatal(state)
	}
}
func TestRebaseRejectsShellActionsAndLeadingFixup(t *testing.T) {
	preview := RebasePreview{Commits: []Commit{{SHA: strings.Repeat("a", 40), Subject: "one"}}}
	for _, action := range []string{"exec", "pick\nexec touch /tmp/unsafe", "fixup", "squash"} {
		if _, err := buildRebasePlan(preview, []RebaseStep{{SHA: preview.Commits[0].SHA, Action: action}}); err == nil {
			t.Fatal("invalid action accepted", action)
		}
	}
}

func TestRebaseConflictContinueUsesPreparedMessageAndLaterReword(t *testing.T) {
	c, root, source, _ := conflictingBranches(t)
	ctx := context.Background()
	writeFile(t, root, "later.txt", "later\n")
	testGit(t, root, "add", "later.txt")
	testGit(t, root, "commit", "-m", "Later commit")
	head := testGit(t, root, "rev-parse", "HEAD")
	request := RebasePreviewRequest{Onto: source, ExpectedHead: head, ExpectedBranch: "main"}
	preview, err := c.PreviewRebase(ctx, root, request)
	if err != nil || len(preview.Commits) != 2 {
		t.Fatal(preview, err)
	}
	steps := []RebaseStep{{SHA: preview.Commits[0].SHA, Action: "pick"}, {SHA: preview.Commits[1].SHA, Action: "reword", Message: "fix: later planned message"}}
	result, err := c.Rebase(ctx, root, RebaseRequest{Preview: request, Token: preview.Token, Steps: steps})
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
	if message := testGit(t, root, "show", "-s", "--format=%s", "HEAD"); message != "fix: later planned message" {
		t.Fatal(message)
	}
}
