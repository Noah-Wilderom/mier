package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedLinesPreserveOtherEditsAndIndex(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
			c, root := testRepository(t, Options{})
			initialCommit(t, c, root)
			writeFile(t, root, "review.txt", strings.Join([]string{"first", "second", "third", "fourth"}, newline))
			testGit(t, root, "commit", "-am", "Base without final newline")
			working := strings.Join([]string{"FIRST", "second", "THIRD", "fourth"}, newline)
			writeFile(t, root, "review.txt", working)
			diff, err := c.Diff(context.Background(), root, "review.txt", false)
			if err != nil {
				t.Fatal(err)
			}
			rows := []int{}
			for i, line := range diff.Lines {
				if strings.TrimSuffix(line.Text, "\r") == "first" || strings.TrimSuffix(line.Text, "\r") == "FIRST" {
					rows = append(rows, i)
				}
			}
			request := StageHunkRequest{Path: "review.txt", Hunk: 0, ExpectedPatch: diff.RawPatch, Lines: rows}
			if err = c.StageHunk(context.Background(), root, request); err != nil {
				t.Fatal(err)
			}
			index := testGit(t, root, "show", ":review.txt")
			if index != strings.Join([]string{"FIRST", "second", "third", "fourth"}, newline) {
				t.Fatalf("wrong index %q", index)
			}
			data, _ := os.ReadFile(filepath.Join(root, "review.txt"))
			if string(data) != working {
				t.Fatal("working file changed")
			}
			if err = c.StageHunk(context.Background(), root, request); err == nil {
				t.Fatal("stale diff accepted")
			}
			staged, _ := c.Diff(context.Background(), root, "review.txt", true)
			rows = nil
			for i, line := range staged.Lines {
				if line.Kind != "context" {
					rows = append(rows, i)
				}
			}
			if err = c.StageHunk(context.Background(), root, StageHunkRequest{Path: "review.txt", Staged: true, Hunk: 0, ExpectedPatch: staged.RawPatch, Lines: rows}); err != nil {
				t.Fatal(err)
			}
			if out := testGit(t, root, "diff", "--cached"); out != "" {
				t.Fatal("unstage left index changes", out)
			}
		})
	}
}
func TestStashPreviewPopAndConflictKeepsEntry(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "review.txt", "stashed\nsecond\n")
	testGit(t, root, "add", "review.txt")
	writeFile(t, root, "untracked.txt", "untracked\n")
	if err := c.SaveStash(ctx, root, SaveStashRequest{Message: "Work in progress", Mode: "all", IncludeUntracked: true, ExpectedHead: head}); err != nil {
		t.Fatal(err)
	}
	items, err := c.Stashes(ctx, root)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	patch, err := c.StashDiff(ctx, root, items[0].SHA)
	if err != nil || !strings.Contains(patch, "+untracked") {
		t.Fatal(patch, err)
	}
	if err = c.MutateStash(ctx, root, StashRequest{SHA: items[0].SHA, Action: "pop", RestoreIndex: true, ExpectedHead: head}); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.Stashes(ctx, root); len(list) != 0 {
		t.Fatal("pop retained stash")
	}
	if testGit(t, root, "show", ":review.txt") != "stashed\nsecond" {
		t.Fatal("staged state not restored")
	}
	if err = c.SaveStash(ctx, root, SaveStashRequest{Mode: "all", IncludeUntracked: true, ExpectedHead: head}); err != nil {
		t.Fatal(err)
	}
	items, _ = c.Stashes(ctx, root)
	writeFile(t, root, "review.txt", "different\nsecond\n")
	testGit(t, root, "commit", "-am", "Different change")
	head = testGit(t, root, "rev-parse", "HEAD")
	if err = c.MutateStash(ctx, root, StashRequest{SHA: items[0].SHA, Action: "pop", ExpectedHead: head}); err == nil {
		t.Fatal("conflicting stash succeeded")
	}
	if list, _ := c.Stashes(ctx, root); len(list) != 1 || list[0].SHA != items[0].SHA {
		t.Fatal("failed pop lost stash")
	}
}
func TestDiscardStaleBackupAndRestore(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "review.txt", "staged\nsecond\n")
	testGit(t, root, "add", "review.txt")
	working := "working\nsecond\n"
	writeFile(t, root, "review.txt", working)
	preview, err := c.PreviewDiscard(ctx, root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "external\n")
	if err = c.Discard(ctx, root, DiscardRequest{Path: "review.txt", Token: preview.Token}); err == nil {
		t.Fatal("stale discard accepted")
	}
	writeFile(t, root, "review.txt", working)
	if err = c.Discard(ctx, root, DiscardRequest{Path: "review.txt", Token: preview.Token}); err != nil {
		t.Fatal(err)
	}
	if testGit(t, root, "show", ":review.txt") != "staged\nsecond" {
		t.Fatal("discard changed staged content")
	}
	data, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	if string(data) != "staged\nsecond\n" {
		t.Fatalf("wrong restored content %q", data)
	}
	backups, err := c.DiscardBackups(ctx, root)
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	writeFile(t, root, "review.txt", "new work\n")
	if err = c.RestoreDiscard(ctx, root, backups[0].ID); err == nil {
		t.Fatal("recovery overwrote new work")
	}
	writeFile(t, root, "review.txt", "staged\nsecond\n")
	if err = c.RestoreDiscard(ctx, root, backups[0].ID); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "review.txt"))
	if string(data) != working {
		t.Fatal("recovery lost content")
	}
}
func TestDiscardSelectedLinesUntrackedBinaryAndIgnoreLiteralPath(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "review.txt", "FIRST\nSECOND\n")
	preview, _ := c.PreviewDiscard(ctx, root, "review.txt")
	hunk := 0
	rows := []int{}
	for i, line := range preview.Diff.Lines {
		if line.Text == "first" || line.Text == "FIRST" {
			rows = append(rows, i)
		}
	}
	if err := c.Discard(ctx, root, DiscardRequest{Path: "review.txt", Token: preview.Token, Hunk: &hunk, Lines: rows}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	if string(data) != "first\nSECOND\n" {
		t.Fatalf("wrong partial discard %q", data)
	}
	writeFile(t, root, "binary.dat", "\x00preserve me")
	preview, err := c.PreviewDiscard(ctx, root, "binary.dat")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Discard(ctx, root, DiscardRequest{Path: "binary.dat", Token: preview.Token}); err != nil {
		t.Fatal(err)
	}
	backups, _ := c.DiscardBackups(ctx, root)
	for _, backup := range backups {
		if backup.Path == "binary.dat" {
			if err = c.RestoreDiscard(ctx, root, backup.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, _ = os.ReadFile(filepath.Join(root, "binary.dat"))
	if string(data) != "\x00preserve me" {
		t.Fatal("binary recovery failed")
	}
	writeFile(t, root, "[literal].txt", "keep")
	writeFile(t, root, "other.txt", "keep")
	if err = c.IgnoreFile(ctx, root, "[literal].txt"); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, c, root)
	for _, file := range state.Changes {
		if file.Path == "[literal].txt" {
			t.Fatal("literal file not ignored")
		}
	}
	if out := testGit(t, root, "ls-files", "--others", "--exclude-standard"); !strings.Contains(out, "other.txt") {
		t.Fatal("ignore broadened to other files")
	}
	if err = c.IgnoreFile(ctx, root, "review.txt"); err == nil {
		t.Fatal("tracked ignore silently accepted")
	}
}
func TestAmendRejectsStaleHeadAndKeepsAuthor(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	if _, err := c.Amend(ctx, root, AmendRequest{Message: "New subject", ExpectedHead: strings.Repeat("a", 40)}); err == nil {
		t.Fatal("stale head amended")
	}
	sha, err := c.Amend(ctx, root, AmendRequest{Message: "New subject", ExpectedHead: head})
	if err != nil || sha == head {
		t.Fatal(sha, err)
	}
	if subject := testGit(t, root, "show", "-s", "--format=%s", "HEAD"); subject != "New subject" {
		t.Fatal(subject)
	}
}
