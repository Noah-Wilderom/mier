package gitclient

import (
	"context"
	"testing"
)

func TestForecastDetectsRebaseConflictsEvenWhenFinalMergeIsClean(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "review.txt", "temporary feature\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "temporary")
	first := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "review.txt", "incoming\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "align final content")
	head := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "main")
	writeFile(t, root, "review.txt", "incoming\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "target changes")
	target := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "feature")
	writeFile(t, root, "untracked.txt", "keep my work\n")
	before := testGit(t, root, "status", "--porcelain=v2")
	request := ConflictForecastRequest{Target: "main", ExpectedTarget: target, ExpectedHead: head, ExpectedBranch: "feature", Rebase: true}
	result, err := client.ForecastConflict(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Clean || len(result.Overlap) != 1 || result.RebaseComplete || len(result.Rebase) != 1 || result.Rebase[0].Commit != first || result.Rebase[0].Clean {
		t.Fatalf("wrong forecast: %#v", result)
	}
	if testGit(t, root, "status", "--porcelain=v2") != before || testGit(t, root, "rev-parse", "HEAD") != head || testGit(t, root, "branch", "--show-current") != "feature" {
		t.Fatal("forecast changed checkout or index")
	}
	request.ExpectedTarget = head
	if _, err = client.ForecastConflict(context.Background(), root, request); err == nil {
		t.Fatal("stale target accepted")
	}
	files, err := client.CompareExact(context.Background(), root, first, target)
	if err != nil || len(files) == 0 {
		t.Fatalf("exact divergent review comparison: %v %v", files, err)
	}
}
