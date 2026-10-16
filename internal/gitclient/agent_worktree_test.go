package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func agentWorkspaceFixture(t *testing.T) (*Client, string, AgentWorktree) {
	t.Helper()
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	target := filepath.Join(t.TempDir(), "agent worktree")
	branch := "mier/agent/fixture"
	if err := c.MutateWorktree(context.Background(), root, WorktreeRequest{Action: "add", Path: target, Mode: "new", Branch: branch, StartSHA: head, ExpectedHead: head, ExpectedBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	guard, err := c.PinAgentWorktree(context.Background(), root, target, branch, head)
	if err != nil {
		t.Fatal(err)
	}
	return c, root, guard
}
func TestAgentWorktreeEditsKeepOriginalCheckoutAndRequireCurrentHashes(t *testing.T) {
	c, root, guard := agentWorkspaceFixture(t)
	ctx := context.Background()
	before := testGit(t, root, "status", "--porcelain=v2")
	file, err := c.AgentRead(ctx, guard, "review.txt")
	if err != nil || !file.Exists {
		t.Fatal(file, err)
	}
	changed, err := c.AgentWrite(ctx, guard, "review.txt", file.Hash, "agent change\n")
	if err != nil || changed.Hash == file.Hash || changed.Content != "agent change\n" {
		t.Fatal(changed, err)
	}
	if _, err = c.AgentWrite(ctx, guard, "review.txt", file.Hash, "stale overwrite"); err == nil {
		t.Fatal("stale agent overwrite accepted")
	}
	if _, err = c.AgentWrite(ctx, guard, "review.txt", "", "overwrite without read"); err == nil {
		t.Fatal("existing file accepted as absent")
	}
	missing, err := c.AgentRead(ctx, guard, "src/new.ts")
	if err != nil || missing.Exists || missing.Hash != "" {
		t.Fatal(missing, err)
	}
	if _, err = c.AgentWrite(ctx, guard, "src/new.ts", "", "export const value = 1\n"); err != nil {
		t.Fatal(err)
	}
	if testGit(t, root, "status", "--porcelain=v2") != before || testGit(t, root, "branch", "--show-current") != "main" {
		t.Fatal("agent edits changed the source checkout")
	}
	if testGit(t, guard.Path, "diff", "--cached", "--name-only") != "" {
		t.Fatal("agent edits were staged automatically")
	}
}
func TestAgentWorktreeRejectsMetadataEscapesIgnoredFilesAndBranchChanges(t *testing.T) {
	c, _, guard := agentWorkspaceFixture(t)
	ctx := context.Background()
	for _, path := range []string{"../outside", "/absolute", ".git/config", "src/../review.txt", ".GIT/config", "name:stream", "src/.git /config", "src\\escape"} {
		if _, err := c.AgentWrite(ctx, guard, path, "", "bad"); err == nil {
			t.Fatalf("agent wrote unsupported path %q", path)
		}
	}
	writeFile(t, guard.Path, ".gitignore", "private.env\n")
	writeFile(t, guard.Path, "private.env", "private local contents\n")
	if _, err := c.AgentRead(ctx, guard, "private.env"); err == nil {
		t.Fatal("ignored private file read")
	}
	if _, err := c.AgentWrite(ctx, guard, "private.env", "", "bad"); err == nil {
		t.Fatal("ignored file overwritten")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(guard.Path, "linked.txt")); err == nil {
		if _, err = c.AgentRead(ctx, guard, "linked.txt"); err == nil {
			t.Fatal("escaped through symlink")
		}
		if _, err = c.AgentWrite(ctx, guard, "linked.txt", "", "bad"); err == nil {
			t.Fatal("wrote through symlink")
		}
	}
	testGit(t, guard.Path, "switch", "--detach", guard.HeadSHA)
	if _, err := c.AgentWrite(ctx, guard, "new.txt", "", "bad"); err == nil {
		t.Fatal("agent wrote after its branch changed")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "keep" {
		t.Fatal("outside file changed", err)
	}
}
func TestAgentWorktreeCatalogPagesAndInvalidatesAfterChanges(t *testing.T) {
	c, _, guard := agentWorkspaceFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(guard.Path, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 210; i++ {
		writeFile(t, guard.Path, "src/file-"+strconv.Itoa(i)+".ts", "source\n")
	}
	first, err := c.AgentFiles(ctx, guard, AgentFileQuery{Prefix: "src"})
	if err != nil || len(first.Paths) != 200 || first.NextCursor == "" || first.Total != 210 {
		t.Fatal(first, err)
	}
	second, err := c.AgentFiles(ctx, guard, AgentFileQuery{Prefix: "src", Cursor: first.NextCursor})
	if err != nil || len(second.Paths) != 10 || second.NextCursor != "" {
		t.Fatal(second, err)
	}
	if _, err = c.AgentFiles(ctx, guard, AgentFileQuery{Prefix: "other", Cursor: first.NextCursor}); err == nil {
		t.Fatal("cross-prefix cursor accepted")
	}
	writeFile(t, guard.Path, "src/another.ts", "new\n")
	if _, err = c.AgentFiles(ctx, guard, AgentFileQuery{Prefix: "src", Cursor: first.NextCursor}); err == nil {
		t.Fatal("changed file catalog accepted an old cursor")
	}
}
