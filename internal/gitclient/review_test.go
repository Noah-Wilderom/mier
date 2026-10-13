package gitclient

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPullRequestCheckoutPreservesExistingBranchesAndDirtyWork(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	bare := filepath.Join(t.TempDir(), "pull-request-origin.git")
	testGit(t, filepath.Dir(bare), "init", "--bare", "--initial-branch=main", bare)
	testGit(t, root, "remote", "add", "origin", bare)
	if err := client.Push(ctx, root, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateBranch(ctx, root, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "pull request content\n")
	if err := client.Stage(ctx, root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	head, err := client.Commit(ctx, root, "Pull request change")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(ctx, root, "origin"); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "--git-dir="+bare, "update-ref", "refs/pull/142/head", head)
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "dirty work must survive\n")
	if err := client.CheckoutPullRequest(ctx, root, 142); err == nil {
		t.Fatal("PR checkout overwrote dirty work")
	}
	if snapshot(t, client, root).CurrentBranch != "main" {
		t.Fatal("Rejected PR checkout changed current branch")
	}
	command := exec.Command(client.gitPath, "-C", root, "show-ref", "--verify", "--quiet", "refs/heads/pr-142")
	if err := command.Run(); err == nil {
		t.Fatal("Failed PR checkout left a new branch behind")
	}
	content, _ := os.ReadFile(filepath.Join(root, "review.txt"))
	if string(content) != "dirty work must survive\n" {
		t.Fatal("Dirty work was altered")
	}
	testGit(t, root, "restore", "--", "review.txt")
	if err := client.CheckoutPullRequest(ctx, root, 142); err != nil {
		t.Fatal(err)
	}
	if snapshot(t, client, root).CurrentBranch != "pr-142" || testGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("PR head was not checked out")
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckoutPullRequest(ctx, root, 142); err != nil {
		t.Fatal("Identical existing PR branch was not reused:", err)
	}
	writeFile(t, root, "local-review.txt", "keep my review work\n")
	if err := client.Stage(ctx, root, []string{"local-review.txt"}); err != nil {
		t.Fatal(err)
	}
	local, err := client.Commit(ctx, root, "Local review work")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckoutPullRequest(ctx, root, 142); err == nil {
		t.Fatal("PR checkout clobbered an existing branch")
	}
	if testGit(t, root, "rev-parse", "refs/heads/pr-142") != local {
		t.Fatal("Existing branch work changed")
	}
	for _, number := range []int{0, -1} {
		if err := client.CheckoutPullRequest(ctx, root, number); err == nil {
			t.Fatal("Invalid PR number accepted")
		}
	}
}

func TestCompareUsesMergeBaseAndIgnoresWorkingChanges(t *testing.T) {
	client, root := testRepository(t, Options{})
	ctx := context.Background()
	initialCommit(t, client, root)
	if err := client.CreateBranch(ctx, root, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "review.txt"), filepath.Join(root, "renamed review.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "feature.txt", "real feature change\n")
	if err := client.Stage(ctx, root, []string{"review.txt", "renamed review.txt", "feature.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Rename and feature"); err != nil {
		t.Fatal(err)
	}
	if err := client.Checkout(ctx, root, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "base-only.txt", "new base branch content\n")
	if err := client.Stage(ctx, root, []string{"base-only.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Commit(ctx, root, "Base branch update"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "uncommitted working content\n")
	files, err := client.Compare(ctx, root, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("Comparison did not use the merge base: %#v", files)
	}
	for _, file := range files {
		if file.Path == "base-only.txt" || strings.Contains(file.RawPatch, "uncommitted") {
			t.Fatal("Comparison included base-only or working changes")
		}
		if file.Path == "renamed review.txt" && (file.PreviousPath != "review.txt" || file.Status != "renamed" || file.Additions != 0) {
			t.Fatalf("Comparison lost rename metadata: %#v", file)
		}
		if file.Path == "feature.txt" && (file.Additions != 1 || file.Lines[0].Text != "real feature change") {
			t.Fatalf("Comparison reused incorrect content: %#v", file)
		}
	}
	same, err := client.Compare(ctx, root, "main", "main")
	if err != nil || len(same) != 0 {
		t.Fatalf("Equal branches returned changes: %v %#v", err, same)
	}
	for _, ref := range []string{"-main", "main..feature", "HEAD~1", "feature@{1}"} {
		if _, err := client.Compare(ctx, root, ref, "feature"); err == nil {
			t.Fatalf("Unsafe comparison revision accepted: %q", ref)
		}
	}
}

func TestHTTPSAuthenticationIsEphemeralScopedAndDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		http.NotFound(w, r)
	}))
	defer second.Close()
	var redirectMode atomic.Bool
	headers := make(chan string, 8)
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		if redirectMode.Load() {
			http.Redirect(w, r, second.URL+"/repo.git/info/refs", http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
	}))
	defer first.Close()
	providerHosts := []string{}
	token := "synthetic-secret-for-authentication"
	client, root := testRepository(t, Options{CredentialProvider: func(ctx context.Context, host string) (string, error) {
		providerHosts = append(providerHosts, host)
		return token, nil
	}})
	testGit(t, root, "remote", "add", "origin", first.URL+"/repo.git")
	testGit(t, root, "config", "http."+first.URL+"/.sslVerify", "false")
	testGit(t, root, "config", "http."+first.URL+"/repo.git.followRedirects", "true")
	t.Setenv("GIT_TRACE_CURL", "1")
	t.Setenv("GIT_TRACE_REDACT", "0")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.quotepath")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	before, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	err = client.Fetch(context.Background(), root, "origin")
	if err == nil {
		t.Fatal("Synthetic non-repository unexpectedly fetched")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), encoded) {
		t.Fatal("Authentication appeared in a returned error")
	}
	select {
	case header := <-headers:
		if header != "Basic "+encoded {
			t.Fatal("Git did not receive the origin-scoped credential")
		}
	default:
		t.Fatal("Synthetic HTTPS repository was not contacted")
	}
	if len(providerHosts) != 1 || providerHosts[0] != strings.TrimPrefix(first.URL, "https://") {
		t.Fatalf("Credential request was not scoped to host: %v", providerHosts)
	}
	after, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || strings.Contains(string(after), token) {
		t.Fatal("Authentication changed repository configuration")
	}
	redirectMode.Store(true)
	err = client.Fetch(context.Background(), root, "origin")
	if err == nil || redirected.Load() != 0 {
		t.Fatal("Authenticated Git request followed a redirect")
	}
}

func TestCredentialBearingCloneURLIsSanitizedAndUsesEphemeralHeader(t *testing.T) {
	headers := make(chan string, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, root := testRepository(t, Options{})
	certificate := filepath.Join(filepath.Dir(root), "test-ca.pem")
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", certificate)
	parsed, err := url.Parse(server.URL + "/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	token := "synthetic-clone-secret"
	parsed.User = url.User(token)
	_, err = client.Clone(context.Background(), parsed.String(), filepath.Join(t.TempDir(), "clone"))
	if err == nil {
		t.Fatal("Synthetic repository unexpectedly cloned")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))) {
		t.Fatal("Clone credentials appeared in an error")
	}
	select {
	case header := <-headers:
		if header != "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)) {
			t.Fatal("Credential-bearing clone did not use ephemeral HTTP auth")
		}
	default:
		t.Fatal("Synthetic clone server was not contacted")
	}
}

func TestNoAccountPreservesExistingGitAuthenticationAndRedactsPublicMetadata(t *testing.T) {
	client, root := testRepository(t, Options{CredentialProvider: func(context.Context, string) (string, error) {
		return "", errors.New("No application account")
	}})
	testGit(t, root, "remote", "add", "origin", "https://synthetic-user:synthetic-password@github.com/team/repo.git")
	state := snapshot(t, client, root)
	if strings.Contains(fmt.Sprintf("%+v", state), "synthetic-password") || strings.Contains(fmt.Sprintf("%+v", state), "synthetic-user") {
		t.Fatal("Public snapshot contains raw credential state")
	}
	for _, raw := range []string{"https://token@host.example/a.git", "https://user:password@host.example/a.git?token=query-secret#fragment"} {
		sanitized := redactCredentials(raw)
		if strings.Contains(sanitized, "token@") || strings.Contains(sanitized, "password") || strings.Contains(sanitized, "query-secret") || strings.Contains(sanitized, "fragment") {
			t.Fatal("Synthetic credential URL was not sanitized")
		}
	}
}
