package gitclient

import (
	"context"
	"strings"
	"testing"
)

func TestProviderContentDiffKeepsContextWithoutChangingRepository(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	// Provider patches must not inherit a workstation's line-ending/display
	// preferences: those would corrupt normalized hunks on Windows or custom Git.
	testGit(t, root, "config", "--global", "core.autocrlf", "true")
	testGit(t, root, "config", "--global", "diff.suppressBlankEmpty", "true")
	before := "unchanged\r\nold\r\nlast"
	after := "unchanged\r\nnew\r\nlast"
	patch, err := c.DiffText(context.Background(), before, after)
	if err != nil || !strings.Contains(patch, " unchanged\r\n-old\r\n+new\r\n last\n\\ No newline at end of file") {
		t.Fatal(patch, err)
	}
	if state := snapshot(t, c, root); state.HeadSHA != head || len(state.Changes) != 0 {
		t.Fatal("provider diff changed checkout", state)
	}
	patch, err = c.DiffText(context.Background(), "", "added\n")
	if err != nil || !strings.HasPrefix(patch, "@@ -0,0 +1 @@") {
		t.Fatal(patch, err)
	}
	if _, err = c.DiffText(context.Background(), "binary\x00", ""); err == nil {
		t.Fatal("binary diff accepted")
	}
}
