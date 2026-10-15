package gitclient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBisectFindsFirstBadAndRestoresBranch(t *testing.T) {
	c, root := testRepository(t, Options{})
	good := initialCommit(t, c, root)
	ctx := context.Background()
	firstBad := ""
	for i := 1; i <= 8; i++ {
		value := "good"
		if i >= 5 {
			value = "bad"
		}
		writeFile(t, root, "probe.txt", fmt.Sprintf("%s %d\n", value, i))
		testGit(t, root, "add", "probe.txt")
		testGit(t, root, "commit", "-m", fmt.Sprintf("Step %d", i))
		if i == 5 {
			firstBad = testGit(t, root, "rev-parse", "HEAD")
		}
	}
	head := testGit(t, root, "rev-parse", "HEAD")
	result, err := c.Bisect(ctx, root, BisectRequest{Action: "start", Good: good, Bad: head, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if result.Operation == nil || result.Operation.Kind != "bisect" {
			t.Fatal(result)
		}
		if result.Operation.BisectDone {
			break
		}
		data, _ := os.ReadFile(filepath.Join(root, "probe.txt"))
		action := "good"
		if strings.HasPrefix(string(data), "bad") {
			action = "bad"
		}
		stale := result.Operation.Token
		result, err = c.Bisect(ctx, root, BisectRequest{Action: action, Token: stale})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Bisect(ctx, root, BisectRequest{Action: action, Token: stale}); err == nil {
			t.Fatal("stale mark accepted")
		}
	}
	if result.Operation == nil || result.Operation.BisectResult != firstBad || result.HeadSHA != firstBad {
		t.Fatalf("Unexpected bisect result: %+v, operation=%+v", result, result.Operation)
	}
	result, err = c.Bisect(ctx, root, BisectRequest{Action: "reset", Token: result.Operation.Token})
	if err != nil || result.Operation != nil || result.HeadSHA != head {
		t.Fatal(result, err)
	}
	if state := snapshot(t, c, root); state.CurrentBranch != "main" || state.Detached {
		t.Fatal(state)
	}
}
func TestBisectLogSupportsOldAndNewGitAndCustomTerms(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, term := range []string{"bad", "'bad'", "'new'", "'slow'"} {
		if got := bisectResult("# bad: [other]\n# first " + term + " commit: [" + sha + "] Failure\n"); got != sha {
			t.Fatal(term, got)
		}
	}
	if got := bisectResult("# possible first 'bad' commit: [" + sha + "] Maybe"); got != "" {
		t.Fatal("ambiguous result marked as exact", got)
	}
}
func TestBisectPreservesDirtyTestEdits(t *testing.T) {
	c, root := testRepository(t, Options{})
	good := initialCommit(t, c, root)
	for i := 0; i < 4; i++ {
		writeFile(t, root, "probe.txt", fmt.Sprint(i))
		testGit(t, root, "add", ".")
		testGit(t, root, "commit", "-m", fmt.Sprint(i))
	}
	head := testGit(t, root, "rev-parse", "HEAD")
	result, err := c.Bisect(context.Background(), root, BisectRequest{Action: "start", Good: good, Bad: head, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "private.txt", "keep")
	for _, action := range []string{"good", "reset"} {
		if _, err = c.Bisect(context.Background(), root, BisectRequest{Action: action, Token: result.Operation.Token}); err == nil {
			t.Fatal("dirty bisect moved", action)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(root, "private.txt")); string(data) != "keep" {
		t.Fatal("lost edits")
	}
}
