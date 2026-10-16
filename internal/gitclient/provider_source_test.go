package gitclient

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderReviewSourceFetchesForkWithoutChangingCheckoutOrTrackingRefs(t *testing.T) {
	var source PullRequestFetchSource
	c, root := testRepository(t, Options{ReviewSourceResolver: func(ctx context.Context, host, owner, repo string, number int) (PullRequestFetchSource, error) {
		if host != "bitbucket.org" || owner != "team" || repo != "app" || number != 7 {
			t.Error("wrong review identity", host, owner, repo, number)
		}
		return source, nil
	}})
	base := initialCommit(t, c, root)
	testGit(t, root, "remote", "add", "origin", "git@bitbucket.org:team/app.git")
	fork := filepath.Join(t.TempDir(), "fork")
	testGit(t, filepath.Dir(fork), "clone", "--no-hardlinks", root, fork)
	testGit(t, fork, "config", "user.name", "Fork Author")
	testGit(t, fork, "config", "user.email", "fork@example.invalid")
	testGit(t, fork, "switch", "-c", "feature")
	writeFile(t, fork, "fork.go", "package fork\n")
	testGit(t, fork, "add", ".")
	testGit(t, fork, "commit", "-m", "Fork work")
	head := testGit(t, fork, "rev-parse", "HEAD")
	source = PullRequestFetchSource{HeadURL: fork, HeadRef: "refs/heads/feature", HeadSHA: head, BaseURL: fork, BaseRef: "refs/heads/main"}
	refs := testGit(t, root, "show-ref")
	// FETCH_HEAD is a pseudoref; newer Git deliberately rejects update-ref.
	writeFile(t, root, ".git/FETCH_HEAD", base+"\t\tbranch 'main' of fixture\n")
	archive, err := c.ArchiveCommit(context.Background(), root, CommitArchiveRequest{Revision: head, Host: "bitbucket.org", Owner: "team", Repo: "app", PullRequest: 7})
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(bytes.NewReader(archive.Data))
	found := false
	for {
		entry, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "fork.go" {
			data, _ := io.ReadAll(reader)
			found = string(data) == "package fork\n"
		}
	}
	if !found {
		t.Fatal("immutable fork source missing")
	}
	if state := snapshot(t, c, root); state.HeadSHA != base || state.CurrentBranch != "main" || len(state.Changes) != 0 {
		t.Fatal("review source changed checkout", state)
	}
	if after := testGit(t, root, "show-ref"); after != refs {
		t.Fatal("review fetch changed tracking refs", after)
	}
	if after := testGit(t, root, "rev-parse", "FETCH_HEAD"); after != base {
		t.Fatal("review fetch changed FETCH_HEAD", after)
	}
	if err = c.CheckoutPullRequest(context.Background(), root, 7, "origin", strings.Repeat("c", 40)); err == nil {
		t.Fatal("stale reviewed head checked out")
	}
	if testGit(t, root, "branch", "--show-current") != "main" {
		t.Fatal("stale checkout switched branch")
	}
	if err = os.MkdirAll(filepath.Join(root, ".git", "info"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".git/info/exclude", "fork.go\n")
	writeFile(t, root, "fork.go", "private ignored contents\n")
	if err = c.CheckoutPullRequest(context.Background(), root, 7, "origin", head); err == nil {
		t.Fatal("review checkout overwrote an ignored file")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "fork.go")); string(data) != "private ignored contents\n" {
		t.Fatal("ignored contents were changed")
	}
	if err = os.Remove(filepath.Join(root, "fork.go")); err != nil {
		t.Fatal(err)
	}
	if err = c.CheckoutPullRequest(context.Background(), root, 7, "origin", head); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "fork.go")); string(data) != "package fork\n" {
		t.Fatal("fork checkout missing")
	}
}

func TestProviderReviewSourceCanFetchCommitAfterSourceBranchDeletion(t *testing.T) {
	var source PullRequestFetchSource
	c, root := testRepository(t, Options{ReviewSourceResolver: func(context.Context, string, string, string, int) (PullRequestFetchSource, error) { return source, nil }})
	initialCommit(t, c, root)
	testGit(t, root, "remote", "add", "origin", "git@bitbucket.org:team/app.git")
	fork := filepath.Join(t.TempDir(), "fork")
	testGit(t, filepath.Dir(fork), "clone", "--no-hardlinks", root, fork)
	testGit(t, fork, "config", "user.name", "Fork Author")
	testGit(t, fork, "config", "user.email", "fork@example.invalid")
	testGit(t, fork, "switch", "-c", "feature")
	writeFile(t, fork, "review.txt", "source after branch deletion\n")
	testGit(t, fork, "add", ".")
	testGit(t, fork, "commit", "-m", "Reviewed work")
	head := testGit(t, fork, "rev-parse", "HEAD")
	testGit(t, fork, "switch", "main")
	testGit(t, fork, "branch", "-D", "feature")
	source = PullRequestFetchSource{HeadURL: fork, HeadRef: "refs/heads/feature", HeadSHA: head, BaseURL: fork, BaseRef: "refs/heads/main"}
	archive, err := c.ArchiveCommit(context.Background(), root, CommitArchiveRequest{Revision: head, Host: "bitbucket.org", Owner: "team", Repo: "app", PullRequest: 7})
	if err != nil {
		t.Fatal(err)
	}
	if archiveFiles(t, archive.Data)["review.txt"] != "source after branch deletion\n" {
		t.Fatal("lost deleted branch source")
	}
	if testGit(t, root, "branch", "--show-current") != "main" {
		t.Fatal("source capture switched checkout")
	}
	if err = c.CheckoutPullRequest(context.Background(), root, 7, "origin", head); err != nil {
		t.Fatal("immutable checkout after branch deletion", err)
	}
	if testGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("deleted branch checkout selected a different commit")
	}
}
func TestGitLabReviewRefsUseMergeRequestNamespace(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	remote := selectedRemote{Remote: Remote{Provider: "gitlab"}, fetchSource: root}
	source, err := c.pullRequestSource(context.Background(), root, remote, 15)
	if err != nil || source.HeadRef != "refs/merge-requests/15/head" || source.HeadURL != root {
		t.Fatal(source, err)
	}
	testGit(t, root, "update-ref", "refs/merge-requests/15/head", head)
	if source.HeadRef == "refs/pull/15/head" {
		t.Fatal("GitHub ref used for GitLab")
	}
}
