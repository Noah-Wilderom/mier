package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryPaginationSearchAndStaleCursor(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	for _, message := range []string{"fix: First [literal]", "feat: Second", "fix: Third"} {
		writeFile(t, root, "review.txt", message)
		testGit(t, root, "commit", "-am", message)
	}
	query := HistoryQuery{Limit: 2}
	first, err := c.HistoryPage(ctx, root, query)
	if err != nil || len(first.Commits) != 2 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	query.Cursor = first.NextCursor
	second, err := c.HistoryPage(ctx, root, query)
	if err != nil || len(second.Commits) != 2 || second.Commits[0].SHA == first.Commits[0].SHA || second.NextCursor != "" {
		t.Fatal(second, err)
	}
	for _, q := range []HistoryQuery{{Query: "[literal]", Search: "message"}, {Query: "reviewer@example.invalid", Search: "author"}, {Query: first.Commits[0].SHA[:8], Search: "sha"}} {
		page, err := c.HistoryPage(ctx, root, q)
		if err != nil || len(page.Commits) == 0 {
			t.Fatal(q, page, err)
		}
	}
	testGit(t, root, "branch", "new-ref")
	if _, err = c.HistoryPage(ctx, root, query); err == nil {
		t.Fatal("cursor remained valid after refs changed")
	}
	if _, err = c.HistoryPage(ctx, root, HistoryQuery{Revision: "--all"}); err == nil {
		t.Fatal("option accepted as revision")
	}
}
func TestFileHistoryFollowsRenameAndBlamePreservesCheckout(t *testing.T) {
	c, root := testRepository(t, Options{})
	initial := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "mv", "review.txt", "renamed.txt")
	testGit(t, root, "commit", "-m", "Rename file")
	head := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "renamed.txt", "do not overwrite me\n")
	testGit(t, root, "add", "renamed.txt")
	page, err := c.HistoryPage(ctx, root, HistoryQuery{Revision: head, Path: "renamed.txt"})
	if err != nil || len(page.Commits) != 2 {
		t.Fatal(page, err)
	}
	if page.Commits[0].FilePath != "renamed.txt" || page.Commits[1].FilePath != "review.txt" {
		t.Fatal("rename paths lost", page.Commits)
	}
	doc, err := c.RevisionFile(ctx, root, initial, page.Commits[1].FilePath)
	if err != nil || doc.Content != "first\nsecond\n" {
		t.Fatal(doc, err)
	}
	lines, err := c.Blame(ctx, root, head, "renamed.txt")
	if err != nil || len(lines) != 2 || lines[0].Text != "first" || lines[0].SHA != initial || lines[0].Author != "Review Tester" {
		t.Fatal(lines, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "renamed.txt"))
	if string(data) != "do not overwrite me\n" || testGit(t, root, "show", ":renamed.txt") != "do not overwrite me" {
		t.Fatal("history inspection changed local work")
	}
}
