package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTagsPreserveAnnotationAndRejectStaleAndExistingNames(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	if err := c.MutateTag(ctx, root, TagRequest{Action: "create", Name: "v1", SHA: head, Message: "Release one"}); err != nil {
		t.Fatal(err)
	}
	tags, err := c.Tags(ctx, root)
	if err != nil || len(tags) != 1 || !tags[0].Annotated || tags[0].Commit != head {
		t.Fatal(tags, err)
	}
	if err = c.MutateTag(ctx, root, TagRequest{Action: "rename", Name: "v1", NewName: "v2", ExpectedObject: tags[0].Object}); err != nil {
		t.Fatal(err)
	}
	tags, err = c.Tags(ctx, root)
	if err != nil || tags[0].Name != "v2" || tags[0].Message != "Release one" {
		t.Fatal(tags, err)
	}
	if err = c.MutateTag(ctx, root, TagRequest{Action: "create", Name: "v3", SHA: head}); err != nil {
		t.Fatal(err)
	}
	if err = c.MutateTag(ctx, root, TagRequest{Action: "rename", Name: "v2", NewName: "v3", ExpectedObject: tags[0].Object}); err == nil {
		t.Fatal("overwrote another tag")
	}
	if err = c.MutateTag(ctx, root, TagRequest{Action: "delete", Name: "v2", ExpectedObject: head}); err == nil {
		t.Fatal("stale tag deletion accepted")
	}
	if err = c.MutateTag(ctx, root, TagRequest{Action: "delete", Name: "v2", ExpectedObject: tags[0].Object}); err != nil {
		t.Fatal(err)
	}
}
func TestRemoteEditingAndSigningAreRepositoryScoped(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	ctx := context.Background()
	if err := c.MutateRemote(ctx, root, RemoteRequest{Action: "add", Name: "origin", URL: "git@example.invalid:team/repo.git"}); err != nil {
		t.Fatal(err)
	}
	remote := snapshot(t, c, root).Remotes[0]
	request := RemoteRequest{Action: "url", Name: "origin", URL: "https://example.invalid/team/new.git", ExpectedFetchURL: remote.FetchURL, ExpectedPushURL: remote.PushURL}
	if err := c.MutateRemote(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if err := c.MutateRemote(ctx, root, request); err == nil {
		t.Fatal("stale remote edit accepted")
	}
	remote = snapshot(t, c, root).Remotes[0]
	if err := c.MutateRemote(ctx, root, RemoteRequest{Action: "rename", Name: "origin", NewName: "upstream", ExpectedFetchURL: remote.FetchURL, ExpectedPushURL: remote.PushURL}); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, c, root).Remotes[0].Name; got != "upstream" {
		t.Fatal(got)
	}
	config := SigningConfig{Enabled: true, Format: "ssh", Key: "/keys/signing.pub"}
	if err := c.SaveSigningConfig(ctx, root, config); err != nil {
		t.Fatal(err)
	}
	got, err := c.SigningConfig(ctx, root)
	if err != nil || got != config {
		t.Fatal(got, err)
	}
	sig, err := c.CommitSignature(ctx, root, testGit(t, root, "rev-parse", "HEAD"))
	if err != nil || sig.Status != "N" {
		t.Fatal(sig, err)
	}
}
func TestResetCheckpointAndRecoveryNeverLoseUncommittedWork(t *testing.T) {
	c, root := testRepository(t, Options{})
	initial := initialCommit(t, c, root)
	ctx := context.Background()
	writeFile(t, root, "review.txt", "committed\n")
	testGit(t, root, "commit", "-am", "Second")
	head := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "untracked.txt", "preserve")
	request := ResetRequest{SHA: initial, Mode: "hard", ExpectedHead: head, ExpectedBranch: "main"}
	if _, err := c.Reset(ctx, root, request); err == nil {
		t.Fatal("hard reset accepted dirty tree")
	}
	os.Remove(filepath.Join(root, "untracked.txt"))
	if _, err := c.Reset(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	checkpoints, err := c.Checkpoints(ctx, root)
	if err != nil || len(checkpoints) != 1 || checkpoints[0].SHA != head {
		t.Fatal(checkpoints, err)
	}
	if err = c.RecoverBranch(ctx, root, RecoverBranchRequest{SHA: head, Name: "recovered"}); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, root, "rev-parse", "refs/heads/recovered"); got != head {
		t.Fatal(got)
	}
	log, err := c.Reflog(ctx, root)
	if err != nil || len(log) < 2 || !strings.Contains(log[0].Message, "reset") {
		t.Fatal(log, err)
	}
	if history := snapshot(t, c, root).History; len(history) != 2 {
		t.Fatal("history contains checkpoint duplicates", history)
	}
}
func TestCherryPickManyKeepsOrderAndRevertAddsCommit(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "switch", "-c", "feature")
	shas := []string{}
	for _, name := range []string{"one.txt", "two.txt"} {
		writeFile(t, root, name, name)
		testGit(t, root, "add", name)
		testGit(t, root, "commit", "-m", name)
		shas = append(shas, testGit(t, root, "rev-parse", "HEAD"))
	}
	testGit(t, root, "switch", "main")
	result, err := c.CherryPickMany(ctx, root, CherryPickManyRequest{SHAs: shas, ExpectedHead: head, ExpectedBranch: "main"})
	if err != nil || result.Operation != nil {
		t.Fatal(result, err)
	}
	if subject := testGit(t, root, "show", "-s", "--format=%s", "HEAD"); subject != "two.txt" {
		t.Fatal("order not retained", subject)
	}
	reverted, err := c.Revert(ctx, root, CherryPickRequest{SHA: result.HeadSHA, ExpectedHead: result.HeadSHA, ExpectedBranch: "main"})
	if err != nil || reverted.Operation != nil || reverted.HeadSHA == result.HeadSHA {
		t.Fatal(reverted, err)
	}
	if _, err = os.Stat(filepath.Join(root, "two.txt")); !os.IsNotExist(err) {
		t.Fatal("revert did not undo chosen commit")
	}
}

func TestHardResetProtectsIgnoredFilesAndDirectories(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			c, root := testRepository(t, Options{})
			initialCommit(t, c, root)
			writeFile(t, root, ".gitignore", "hidden\n")
			testGit(t, root, "add", ".gitignore")
			testGit(t, root, "commit", "-m", "Ignore hidden")
			base := testGit(t, root, "rev-parse", "HEAD")
			writeFile(t, root, "hidden", "tracked target\n")
			testGit(t, root, "add", "--force", "hidden")
			testGit(t, root, "commit", "-m", "Track target")
			target := testGit(t, root, "rev-parse", "HEAD")
			testGit(t, root, "reset", "--hard", base)
			if directory {
				if err := os.Mkdir(filepath.Join(root, "hidden"), 0755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, "hidden/secret.txt", "keep")
			} else {
				writeFile(t, root, "hidden", "keep")
			}
			if _, err := c.Reset(context.Background(), root, ResetRequest{SHA: target, Mode: "hard", ExpectedHead: base, ExpectedBranch: "main"}); err == nil {
				t.Fatal("hard reset overwrote ignored work")
			}
			file := "hidden"
			if directory {
				file += "/secret.txt"
			}
			data, _ := os.ReadFile(filepath.Join(root, file))
			if string(data) != "keep" {
				t.Fatal("ignored work lost")
			}
		})
	}
}
