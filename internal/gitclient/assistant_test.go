package gitclient

import (
	"context"
	"strings"
	"testing"
)

func TestAssistantCommitCaptureUsesStagedBytesAndKeepsImmutableReads(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "source.ts", "const value = 1\n")
	testGit(t, root, "add", "source.ts")
	writeFile(t, root, "source.ts", "const value = 2\n")
	captured, err := c.AssistantCapture(ctx, root, AssistantCaptureRequest{Kind: "commit", ExpectedHead: head})
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.Files) != 1 || !strings.Contains(captured.Files[0].Patch, "+const value = 1") || strings.Contains(captured.Files[0].Patch, "value = 2") {
		t.Fatal("unstaged changes entered the commit context", captured.Files)
	}
	if err = c.ValidateAssistantCapture(ctx, captured); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "add", "source.ts")
	source, err := c.AssistantSource(ctx, captured, "source.ts", "new")
	if err != nil || source.Content != "const value = 1\n" {
		t.Fatal("source read followed a later index", source, err)
	}
	if err = c.ValidateAssistantCapture(ctx, captured); err == nil {
		t.Fatal("stale generated commit draft accepted")
	}
	if testGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("AI capture committed changes")
	}
	if _, err = c.AssistantSource(ctx, captured, "../outside", "new"); err == nil {
		t.Fatal("source escaped repository")
	}
}

func TestAssistantCommitCaptureSupportsInitialCommitAndRejectsEmptyStage(t *testing.T) {
	c, root := testRepository(t, Options{})
	ctx := context.Background()
	if _, err := c.AssistantCapture(ctx, root, AssistantCaptureRequest{Kind: "commit"}); err == nil {
		t.Fatal("empty stage accepted")
	}
	writeFile(t, root, "first.txt", "first staged source\n")
	testGit(t, root, "add", "first.txt")
	captured, err := c.AssistantCapture(ctx, root, AssistantCaptureRequest{Kind: "commit"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := c.AssistantSource(ctx, captured, "first.txt", "new")
	if err != nil || source.Content != "first staged source\n" || captured.HeadSHA != "" {
		t.Fatal(source, err)
	}
	if _, err = c.AssistantSource(ctx, captured, "first.txt", "old"); err == nil {
		t.Fatal("invented old source for an initial commit")
	}
}

func TestAssistantBranchCaptureIgnoresWorkingFilesAndRejectsMovedSource(t *testing.T) {
	c, root := testRepository(t, Options{})
	base := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "source.ts", "const value = 1\n")
	testGit(t, root, "add", "source.ts")
	testGit(t, root, "commit", "-m", "feat: add value")
	head := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "source.ts", "private uncommitted edit\n")
	captured, err := c.AssistantCapture(ctx, root, AssistantCaptureRequest{Kind: "pull_request", Base: "main", Head: "feature", ExpectedHead: head})
	if err != nil {
		t.Fatal(err)
	}
	if captured.BaseSHA != base || captured.HeadSHA != head || captured.MergeBaseSHA != base || strings.Contains(captured.Files[0].Patch, "private uncommitted") {
		t.Fatal("comparison is not based on committed branches", captured)
	}
	if err = c.ValidateAssistantCapture(ctx, captured); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "add", "source.ts")
	testGit(t, root, "commit", "-m", "feat: update value")
	if err = c.ValidateAssistantCapture(ctx, captured); err == nil {
		t.Fatal("moved source accepted")
	}
	source, err := c.AssistantSource(ctx, captured, "source.ts", "new")
	if err != nil || source.Content != "const value = 1\n" {
		t.Fatal("immutable branch source changed", source, err)
	}
}

func TestAssistantConflictProposalKeepsUnresolvedIndexAndRejectsDiskChanges(t *testing.T) {
	c, root, source, head := conflictingBranches(t)
	ctx := context.Background()
	if _, err := c.MergeBranch(ctx, root, MergeBranchRequest{Branch: "feature", ExpectedHead: head, ExpectedBranch: "main", ExpectedSource: source}); err != nil {
		t.Fatal(err)
	}
	captured, err := c.AssistantCapture(ctx, root, AssistantCaptureRequest{Kind: "conflict", Path: "review.txt", ExpectedHead: head})
	if err != nil || captured.Conflict == nil {
		t.Fatal(captured, err)
	}
	if err = c.ValidateAssistantCapture(ctx, captured); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "manually edited result\n")
	if err = c.ValidateAssistantCapture(ctx, captured); err == nil {
		t.Fatal("changed conflict accepted")
	}
	if testGit(t, root, "ls-files", "--unmerged", "review.txt") == "" {
		t.Fatal("capture resolved or staged a conflict")
	}
}
