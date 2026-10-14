package gitclient

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitTransportsBypassHTTPSAuthentication(t *testing.T) {
	credentialCalls := 0
	client, root := testRepository(t, Options{CredentialProvider: func(context.Context, string) (string, error) {
		credentialCalls++
		return "unused-application-token", nil
	}})
	sha := initialCommit(t, client, root)
	for _, remote := range []string{
		"git@github.com:team/review.git",
		"github.com:team/review.git",
		"git@work-alias:team/review.git",
		"git@[2001:db8::1]:team/review.git",
		"ssh://git@github.com:2222/team/review.git",
		"git://github.com/team/review.git",
		"file:///tmp/100%25coverage.git",
		"./repositories/100%coverage.git",
		`C:\repositories\review.git`,
	} {
		t.Run(remote, func(t *testing.T) {
			if err := validateRemoteURL(remote); err != nil {
				t.Fatal(err)
			}
			// Exercise the shared runner without contacting an external host.
			output, err := client.authenticatedRun(context.Background(), root, client.options.LocalTimeout, remote, "rev-parse", "HEAD")
			if err != nil || strings.TrimSpace(string(output)) != sha {
				t.Fatalf("Git transport was rejected before running Git: %s %v", output, err)
			}
		})
	}
	if credentialCalls != 0 {
		t.Fatal("A non-HTTPS Git transport requested an application token")
	}
}

func localRepositoryURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestSCPRemoteCloneFetchPullPushAndMissingReviewCommit(t *testing.T) {
	client, source := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, source)
	bare := filepath.Join(t.TempDir(), "transport fixture.git")
	testGit(t, source, "clone", "--bare", "--", source, bare)
	remote := "git@github.com:team/transport-fixture.git"
	// Git resolves the SSH spelling to a disposable local repository. This
	// exercises real clone/fetch/pull/push without SSH keys or a public server.
	testGit(t, source, "config", "--global", "url."+localRepositoryURL(bare)+".insteadOf", remote)
	destination := filepath.Join(t.TempDir(), "review clone")
	state, err := client.Clone(ctx, remote, destination)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentBranch != "main" {
		t.Fatalf("Clone selected the wrong branch: %#v", state)
	}
	writeFile(t, source, "review.txt", "remote fast-forward\n")
	testGit(t, source, "commit", "--all", "-m", "Update remote")
	fastForward := testGit(t, source, "rev-parse", "HEAD")
	testGit(t, source, "push", "--", bare, "main")
	if err := client.Fetch(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, destination).Behind != 1 {
		t.Fatal("SSH fetch did not update tracking status")
	}
	if err := client.Pull(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if testGit(t, destination, "rev-parse", "HEAD") != fastForward {
		t.Fatal("SSH pull did not fast-forward")
	}
	testGit(t, destination, "config", "user.name", "Transport Tester")
	testGit(t, destination, "config", "user.email", "tester@example.invalid")
	testGit(t, destination, "config", "commit.gpgsign", "false")
	writeFile(t, destination, "local.txt", "published through SSH spelling\n")
	testGit(t, destination, "add", "local.txt")
	testGit(t, destination, "commit", "-m", "Publish local change")
	if err := client.Push(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if testGit(t, source, "--git-dir="+bare, "rev-parse", "main") != testGit(t, destination, "rev-parse", "HEAD") {
		t.Fatal("SSH push did not publish the local commit")
	}
	// A PR commit is deliberately absent from the review clone. Hover must be
	// able to fetch it without checking out the PR or changing tracking refs.
	testGit(t, source, "switch", "-c", "review-only")
	writeFile(t, source, "missing.php", "<?php class OnlyInPullRequest {}\n")
	testGit(t, source, "add", "missing.php")
	testGit(t, source, "commit", "-m", "Missing PR source")
	prSHA := testGit(t, source, "rev-parse", "HEAD")
	testGit(t, source, "push", "--", bare, "HEAD:refs/pull/7/head")
	writeFile(t, destination, "local.txt", "uncommitted local edit\n")
	before := testGit(t, destination, "status", "--porcelain=v2")
	refs := testGit(t, destination, "show-ref")
	fetchHead, err := os.ReadFile(filepath.Join(destination, ".git", "FETCH_HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := client.ArchiveCommit(ctx, destination, CommitArchiveRequest{Revision: prSHA, Host: "github.com", Owner: "team", Repo: "transport-fixture", PullRequest: 7})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Revision != prSHA || archiveFiles(t, archive.Data)["missing.php"] != "<?php class OnlyInPullRequest {}\n" {
		t.Fatal("Missing PR source was not fetched accurately")
	}
	afterFetchHead, err := os.ReadFile(filepath.Join(destination, ".git", "FETCH_HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if testGit(t, destination, "status", "--porcelain=v2") != before || testGit(t, destination, "show-ref") != refs || string(afterFetchHead) != string(fetchHead) {
		t.Fatal("PR source capture changed local work, refs, or FETCH_HEAD")
	}
}
