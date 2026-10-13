package gitclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is not installed")
	}
	command := exec.Command(git, args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func testRepository(t *testing.T, options Options) (*Client, string) {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(directory, "empty-global-config"))
	root := filepath.Join(directory, "repository with spaces")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "init", "--initial-branch=main")
	testGit(t, root, "config", "user.name", "Review Tester")
	testGit(t, root, "config", "user.email", "reviewer@example.invalid")
	testGit(t, root, "config", "commit.gpgsign", "false")
	testGit(t, root, "config", "core.autocrlf", "false")
	testGit(t, root, "config", "core.hooksPath", filepath.Join(directory, "hooks"))
	client, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return client, root
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, client *Client, root string) RepositorySnapshot {
	t.Helper()
	state, err := client.Snapshot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func findChange(t *testing.T, state RepositorySnapshot, path string) FileStatus {
	t.Helper()
	for _, change := range state.Changes {
		if change.Path == path {
			return change
		}
	}
	t.Fatalf("No change for %q: %#v", path, state.Changes)
	return FileStatus{}
}

func initialCommit(t *testing.T, client *Client, root string) string {
	t.Helper()
	writeFile(t, root, "review.txt", "first\nsecond\n")
	if err := client.Stage(context.Background(), root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	sha, err := client.Commit(context.Background(), root, "Initial commit\n\nA real local repository.")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestUnbornStagingPartialChangesAndRenames(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	state := snapshot(t, client, root)
	if state.HasHead || state.HeadSHA != "" || state.CurrentBranch != "main" || len(state.History) != 0 {
		t.Fatalf("Invalid unborn state: %#v", state)
	}
	names := []string{"review.txt", " leading space.txt", "-notes.txt"}
	if runtime.GOOS != "windows" {
		names = append(names, "notes\nreview.txt")
	}
	for _, name := range names {
		writeFile(t, root, name, "first\nsecond\n")
	}
	diff, err := client.Diff(ctx, root, names[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Additions != 2 || diff.Deletions != 0 || len(diff.Lines) != 2 || *diff.Lines[0].NewLine != 1 {
		t.Fatalf("Invalid untracked diff: %#v", diff)
	}
	if err := client.Stage(ctx, root, names); err != nil {
		t.Fatal(err)
	}
	state = snapshot(t, client, root)
	for _, name := range names {
		change := findChange(t, state, name)
		if !change.Staged || change.Unstaged || change.Untracked {
			t.Fatalf("Invalid staged state: %#v", change)
		}
	}
	if err := client.Unstage(ctx, root, names); err != nil {
		t.Fatal(err)
	}
	if !findChange(t, snapshot(t, client, root), names[0]).Untracked {
		t.Fatal("Unborn unstage did not preserve the working file")
	}
	if err := client.Stage(ctx, root, names); err != nil {
		t.Fatal(err)
	}
	sha, err := client.Commit(ctx, root, "Initial review\n\nPreserve spaces and newlines.")
	if err != nil {
		t.Fatal(err)
	}
	state = snapshot(t, client, root)
	if len(sha) != 40 || len(state.History) != 1 || state.History[0].SHA != sha || state.History[0].Body != "Preserve spaces and newlines." {
		t.Fatalf("Invalid history: %#v", state.History)
	}

	writeFile(t, root, "review.txt", "first\nupdated\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "first\nupdated\nworking\n")
	change := findChange(t, snapshot(t, client, root), "review.txt")
	if !change.Staged || !change.Unstaged {
		t.Fatalf("Partially staged state lost: %#v", change)
	}
	staged, err := client.Diff(ctx, root, "review.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	working, err := client.Diff(ctx, root, "review.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Additions != 1 || staged.Deletions != 1 || working.Additions != 1 || working.Deletions != 0 {
		t.Fatalf("Incorrect staged/working diffs: %#v / %#v", staged, working)
	}
	if err := client.Unstage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if findChange(t, snapshot(t, client, root), "review.txt").Staged {
		t.Fatal("Unstage retained staged changes")
	}
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Update review"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "review.txt"), filepath.Join(root, "renamed review.txt")); err != nil {
		t.Fatal(err)
	}
	if err := client.Stage(ctx, root, []string{"review.txt", "renamed review.txt"}); err != nil {
		t.Fatal(err)
	}
	rename := findChange(t, snapshot(t, client, root), "renamed review.txt")
	if rename.Status != "renamed" || rename.OriginalPath != "review.txt" {
		t.Fatalf("Rename metadata lost: %#v", rename)
	}
	diff, err = client.Diff(ctx, root, "renamed review.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Status != "renamed" || diff.PreviousPath != "review.txt" || diff.Additions != 0 || diff.Deletions != 0 {
		t.Fatalf("Incorrect rename diff: %#v", diff)
	}
	if err := client.Unstage(ctx, root, []string{"renamed review.txt"}); err != nil {
		t.Fatal(err)
	}
	for _, change := range snapshot(t, client, root).Changes {
		if change.Staged {
			t.Fatalf("Unstage did not restore both rename paths: %#v", change)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "renamed review.txt")); err != nil {
		t.Fatal("Unstage removed the working file")
	}
}

func TestBranchOperationsPreserveWorkAndRejectUnsafeRefs(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	for _, invalid := range []string{"-danger", "feature..bad", "feature@{1}", "HEAD", ""} {
		if err := client.CreateBranch(ctx, root, invalid); err == nil {
			t.Fatalf("Accepted invalid ref %q", invalid)
		}
		if err := client.Checkout(ctx, root, invalid); err == nil {
			t.Fatalf("Accepted invalid checkout %q", invalid)
		}
	}
	if err := client.CreateBranch(ctx, root, "feature/review"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, root).CurrentBranch != "main" {
		t.Fatal("Creating a branch unexpectedly checked it out")
	}
	if err := client.Checkout(ctx, root, "feature/review"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "feature content\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Feature content"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "unsaved local work\n")
	if err := client.Checkout(ctx, root, "feature/review"); err == nil {
		t.Fatal("Checkout overwrote dirty work")
	}
	content, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	if string(content) != "unsaved local work\n" || snapshot(t, client, root).CurrentBranch != "main" {
		t.Fatal("Rejected checkout changed the working tree")
	}
	testGit(t, root, "restore", "--", "review.txt")
	writeFile(t, root, "unrelated.txt", "keep this\n")
	if err := client.Checkout(ctx, root, "feature/review"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "unrelated.txt")); err != nil {
		t.Fatal("Safe checkout removed unrelated work")
	}
}

func TestSnapshotHeadMatchesCheckoutWhenOtherBranchHasNewerHistory(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	mainHead := initialCommit(t, client, root)
	if err := client.CreateBranch(ctx, root, "feature/newer"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "feature/newer"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "newer branch\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	featureHead, err := client.Commit(ctx, root, "Newer branch commit")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, client, root)
	if state.HeadSHA != mainHead || state.HeadSHA == featureHead {
		t.Fatalf("Snapshot HEAD does not identify the displayed checkout: %+v", state)
	}
}

func TestFileTraversalSymlinksAndLiteralPathspecs(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	for _, path := range []string{"../outside.txt", filepath.Join(t.TempDir(), "outside.txt"), ".git/config", ".", ""} {
		if err := client.Stage(ctx, root, []string{path}); err == nil {
			t.Fatalf("Accepted unsafe stage path %q", path)
		}
		if _, err := client.Diff(ctx, root, path, false); err == nil {
			t.Fatalf("Accepted unsafe diff path %q", path)
		}
	}
	if err := client.Stage(ctx, root, nil); err == nil {
		t.Fatal("Accepted an empty selection")
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "outside content must not be read")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := client.Stage(ctx, root, []string{"linked/secret.txt"}); err == nil {
		t.Fatal("Followed a parent symlink outside the repository")
	}
	if _, err := client.Diff(ctx, root, "linked/secret.txt", false); err == nil {
		t.Fatal("Read outside the repository")
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	diff, err := client.Diff(ctx, root, "link.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diff.RawPatch, "outside content") || diff.Additions != 1 {
		t.Fatalf("Read symlink target content: %#v", diff)
	}

	writeFile(t, root, ":(glob)*", "literal pathspec\n")
	writeFile(t, root, "another.txt", "do not stage\n")
	if err := client.Stage(ctx, root, []string{":(glob)*"}); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, client, root)
	if !findChange(t, state, ":(glob)*").Staged || findChange(t, state, "another.txt").Staged {
		t.Fatal("Git interpreted a file name as a pathspec expression")
	}
}

func TestBinaryNoNewlineAndCappedUntrackedDiffs(t *testing.T) {
	client, root := testRepository(t, Options{MaxFileBytes: 32})
	ctx := context.Background()
	writeFile(t, root, "binary.dat", string([]byte{0, 1, 2, 255}))
	binary, err := client.Diff(ctx, root, "binary.dat", false)
	if err != nil {
		t.Fatal(err)
	}
	if !binary.Binary || binary.Additions != 0 || len(binary.Lines) != 0 {
		t.Fatalf("Invalid binary diff: %#v", binary)
	}
	writeFile(t, root, "large.txt", strings.Repeat("large\n", 30))
	large, err := client.Diff(ctx, root, "large.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !large.Truncated || len(large.Lines) != 0 {
		t.Fatalf("Oversized file was not capped: %#v", large)
	}
	writeFile(t, root, "no-newline.txt", "last line")
	text, err := client.Diff(ctx, root, "no-newline.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if text.Additions != 1 || !strings.Contains(text.RawPatch, "No newline at end of file") || text.Lines[0].Text != "last line" {
		t.Fatalf("Invalid final line: %#v", text)
	}
}

func TestLocalRemotesFetchFastForwardPushAndTrackingCheckout(t *testing.T) {
	client, source := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, source)
	bare := filepath.Join(t.TempDir(), "local remote.git")
	testGit(t, filepath.Dir(bare), "init", "--bare", "--initial-branch=main", bare)
	testGit(t, source, "remote", "add", "origin", bare)
	if err := client.Push(ctx, source, "origin"); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "clone")
	cloned, err := client.Clone(ctx, bare, destination)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.CurrentBranch != "main" || cloned.Upstream != "origin/main" || cloned.Remotes[0].Provider != "local" {
		t.Fatalf("Invalid clone state: %#v", cloned)
	}
	testGit(t, destination, "config", "user.name", "Clone Tester")
	testGit(t, destination, "config", "user.email", "clone@example.invalid")
	testGit(t, destination, "config", "commit.gpgsign", "false")
	writeFile(t, source, "review.txt", "remote update\n")
	if err := client.Stage(ctx, source, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	remoteSHA, err := client.Commit(ctx, source, "Remote update")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, source, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := client.Fetch(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, destination).Behind != 1 {
		t.Fatal("Fetch did not update behind count")
	}
	if err := client.Pull(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, destination).History[0].SHA != remoteSHA {
		t.Fatal("Pull did not fast-forward")
	}

	writeFile(t, destination, "local.txt", "local divergence\n")
	if err := client.Stage(ctx, destination, []string{"local.txt"}); err != nil {
		t.Fatal(err)
	}
	localSHA, err := client.Commit(ctx, destination, "Local divergence")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "remote.txt", "remote divergence\n")
	if err := client.Stage(ctx, source, []string{"remote.txt"}); err != nil {
		t.Fatal(err)
	}
	remoteSHA, err = client.Commit(ctx, source, "Remote divergence")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, source, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := client.Fetch(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, client, destination)
	if state.Ahead != 1 || state.Behind != 1 {
		t.Fatalf("Divergence not detected: %#v", state)
	}
	if err := client.Pull(ctx, destination, "origin"); err == nil {
		t.Fatal("Pull created a merge for diverged branches")
	}
	if testGit(t, destination, "rev-parse", "HEAD") != localSHA {
		t.Fatal("Rejected pull changed local HEAD")
	}
	if err := client.Push(ctx, destination, "origin"); err == nil {
		t.Fatal("Push overwrote remote divergence")
	}
	if testGit(t, source, "--git-dir="+bare, "rev-parse", "refs/heads/main") != remoteSHA {
		t.Fatal("Rejected push changed remote HEAD")
	}

	if err := client.CreateBranch(ctx, source, "topic"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, source, "topic"); err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, source, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := client.Fetch(ctx, destination, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, destination, "origin/topic"); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, destination).CurrentBranch != "topic" {
		t.Fatal("Remote checkout did not create a tracking branch")
	}
}

func TestRemoteMetadataAndErrorsRedactCredentials(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	testGit(t, root, "remote", "add", "github", "https://fake-pat-secret@github.com/team/review.git")
	testGit(t, root, "remote", "add", "enterprise", "https://user:fake-password@github.enterprise.local/platform/review.git")
	state := snapshot(t, client, root)
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "fake-pat-secret") || strings.Contains(string(encoded), "fake-password") {
		t.Fatal("Snapshot exposed remote credentials")
	}
	for _, remote := range state.Remotes {
		if remote.Name == "github" && (remote.Provider != "github" || remote.Owner != "team" || remote.Repository != "review") {
			t.Fatalf("Invalid GitHub metadata: %#v", remote)
		}
		if remote.Name == "enterprise" && (remote.Host != "github.enterprise.local" || remote.Owner != "platform") {
			t.Fatalf("Invalid enterprise metadata: %#v", remote)
		}
	}
	message := redactCredentials("fatal: unable to access 'https://user:fake-password@host.example/team/repo.git': failed")
	if strings.Contains(message, "fake-password") || strings.Contains(message, "user:") {
		t.Fatal("Error sanitizer exposed credentials")
	}
	_, err = client.Clone(context.Background(), "https://fake-pat-secret@127.0.0.1:1/repo.git", filepath.Join(t.TempDir(), "failed-clone"))
	if err == nil || strings.Contains(err.Error(), "fake-pat-secret") {
		t.Fatal("Clone error exposed credentials or unexpectedly succeeded")
	}
	for _, badURL := range []string{"--config", "ext::touch exploit", "unknown://host/repo"} {
		if _, err := client.Clone(context.Background(), badURL, filepath.Join(t.TempDir(), "rejected")); err == nil {
			t.Fatalf("Accepted unsafe clone URL %q", badURL)
		}
	}
	nonempty := t.TempDir()
	writeFile(t, nonempty, "keep.txt", "preserve")
	if _, err := client.Clone(context.Background(), root, nonempty); err == nil {
		t.Fatal("Clone accepted a nonempty destination")
	}
}

func TestHistoryIsBoundedAndPreservesParents(t *testing.T) {
	client, root := testRepository(t, Options{})
	var stream strings.Builder
	for i := 0; i < 205; i++ {
		subject := fmt.Sprintf("History commit %03d", i)
		fmt.Fprintf(&stream, "commit refs/heads/main\ncommitter History Tester <history@example.invalid> %d +0000\ndata %d\n%s\n\n", 1700000000+i, len(subject), subject)
	}
	stream.WriteString("done\n")
	command := exec.Command(client.gitPath, "fast-import", "--quiet")
	command.Dir, command.Stdin = root, strings.NewReader(stream.String())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Create history: %v %s", err, output)
	}
	state := snapshot(t, client, root)
	if len(state.History) != 200 || state.History[0].Subject != "History commit 204" || len(state.History[0].Parents) != 1 {
		t.Fatalf("History bounds or parents incorrect: %#v", state.History[:min(2, len(state.History))])
	}
}

func TestCancellationAndRepositoryLockWaitAreBounded(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	// Operations lock Git's canonical root, which can differ from t.TempDir on
	// macOS (/var) and Windows (short paths). Hold the actual operation lock.
	canonical, err := client.resolveRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := client.acquire(context.Background(), canonical)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	started := time.Now()
	err = client.Stage(ctx, root, []string{"review.txt"})
	cancel()
	release()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("Lock wait did not honor cancellation: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := client.Snapshot(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("Canceled snapshot continued: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	hooks := filepath.Join(filepath.Dir(root), "hooks")
	if err := os.Mkdir(hooks, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nsleep 10\n"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "pending commit\n")
	if err := client.Stage(context.Background(), root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	started = time.Now()
	_, err = client.Commit(ctx, root, "Cancel this commit")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("Commit process tree did not cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "index.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Cancellation left a stale index lock: %v", err)
	}
	if err := client.Unstage(context.Background(), root, []string{"review.txt"}); err != nil {
		t.Fatal("Canceled operation blocked later writes:", err)
	}
}

func TestMergeConflictStatus(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	if err := client.CreateBranch(ctx, root, "conflicting"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "conflicting"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "feature version\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Feature change"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "main version\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Main change"); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(client.gitPath, "-C", root, "merge", "conflicting")
	if err := command.Run(); err == nil {
		t.Fatal("Expected a real merge conflict")
	}
	change := findChange(t, snapshot(t, client, root), "review.txt")
	if !change.Conflict || change.Status != "conflicted" || !change.Staged || !change.Unstaged {
		t.Fatalf("Conflict status lost: %#v", change)
	}
	diff, err := client.Diff(ctx, root, "review.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Lines) == 0 || !strings.Contains(diff.RawPatch, "<<<<<<< HEAD") {
		t.Fatal("Conflict diff did not expose the working conflict markers")
	}
}

func TestConcurrentWritesAreSerializedPerRepository(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	var writes sync.WaitGroup
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("concurrent-%02d.txt", i)
		writeFile(t, root, name, "independent staged content\n")
		writes.Add(1)
		go func(path string) {
			defer writes.Done()
			if err := client.Stage(context.Background(), root, []string{path}); err != nil {
				t.Errorf("Concurrent stage failed: %v", err)
			}
		}(name)
	}
	writes.Wait()
	state := snapshot(t, client, root)
	if len(state.Changes) != 12 {
		t.Fatalf("Concurrent writes lost changes: %d", len(state.Changes))
	}
	for _, change := range state.Changes {
		if !change.Staged {
			t.Fatalf("Concurrent write was not staged: %#v", change)
		}
	}
}
