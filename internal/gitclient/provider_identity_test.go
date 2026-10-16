package gitclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderRemoteIdentitiesCoverNamespacesAndAzureTransports(t *testing.T) {
	for _, test := range []struct{ url, host, owner, repo, provider string }{{"git@gitlab.com:team/sub/app.git", "gitlab.com", "team/sub", "app", "gitlab"}, {"ssh://git@gitlab.com:2222/team/sub/app.git", "gitlab.com", "team/sub", "app", "gitlab"}, {"git@ssh.dev.azure.com:v3/company/My Project/app", "dev.azure.com", "company/My Project", "app", "azure"}, {"ssh://git@ssh.dev.azure.com:22/v3/company/project/app", "dev.azure.com", "company/project", "app", "azure"}, {"https://company@dev.azure.com/company/project/_git/app", "dev.azure.com", "company/project", "app", "azure"}, {"https://company.visualstudio.com/project/_git/app", "dev.azure.com", "company/project", "app", "azure"}, {"ssh://git@ssh.github.com:443/team/app.git", "github.com", "team", "app", "github"}} {
		host, owner, repo, provider := remoteMetadata(test.url)
		if host != test.host || owner != test.owner || repo != test.repo || provider != test.provider {
			t.Fatal(test.url, host, owner, repo, provider)
		}
	}
}
func TestSSHIdentitiesResolveAliasesWithoutChangingFetchURLs(t *testing.T) {
	calls := 0
	c, root := testRepository(t, Options{SSHHostResolver: func(ctx context.Context, host string) (string, error) {
		calls++
		if host != "work-alias" {
			t.Error(host)
		}
		return "gitlab.company.test", nil
	}, ProviderResolver: func(host string) string {
		if host == "gitlab.company.test" {
			return "gitlab"
		}
		return ""
	}})
	initialCommit(t, c, root)
	original := "git@work-alias:team/sub/app.git"
	testGit(t, root, "remote", "add", "origin", original)
	for i := 0; i < 2; i++ {
		state := snapshot(t, c, root)
		remote := state.Remotes[0]
		if remote.Host != "gitlab.company.test" || remote.Owner != "team/sub" || remote.Provider != "gitlab" || remote.FetchURL != original {
			t.Fatal(remote)
		}
	}
	if calls != 1 {
		t.Fatal("SSH configuration not cached", calls)
	}
}
func TestUsernameOnlyRemoteUsesSavedProviderCredentials(t *testing.T) {
	headers := make(chan string, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer server.Close()
	c, root := testRepository(t, Options{CredentialResolver: func(ctx context.Context, host string) (string, string, error) {
		return "x-bitbucket-api-token-auth", "synthetic-provider-token", nil
	}})
	source := strings.Replace(server.URL, "https://", "https://username-hint@", 1)
	testGit(t, root, "remote", "add", "origin", source+"/repo.git")
	testGit(t, root, "config", "http."+server.URL+"/.sslVerify", "false")
	err := c.Fetch(context.Background(), root, "origin")
	if err == nil {
		t.Fatal("synthetic remote unexpectedly fetched")
	}
	select {
	case header := <-headers:
		if header != "Basic "+base64.StdEncoding.EncodeToString([]byte("x-bitbucket-api-token-auth:synthetic-provider-token")) {
			t.Fatal("URL username interpreted as a token", header)
		}
	default:
		t.Fatal("remote not contacted")
	}
	if strings.Contains(err.Error(), "synthetic-provider-token") {
		t.Fatal("credential in error")
	}
}
func TestRemoteURLRewritesUseGitIdentityAndPreserveConfiguredURL(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	testGit(t, root, "config", "url.https://gitlab.com/team/sub/.insteadOf", "work:")
	testGit(t, root, "config", "--add", "url.https://gitlab.com/team/sub/.insteadOf", "publish:")
	testGit(t, root, "remote", "add", "origin", "work:app.git")
	testGit(t, root, "remote", "set-url", "--push", "origin", "publish:app.git")
	state := snapshot(t, c, root)
	remote := state.Remotes[0]
	if remote.FetchURL != "work:app.git" || remote.PushURL != "publish:app.git" || remote.Host != "gitlab.com" || remote.Owner != "team/sub" || remote.Repository != "app" || remote.Provider != "gitlab" {
		t.Fatal(remote)
	}
	selected, err := c.selectRemote(context.Background(), root, "origin")
	if err != nil || selected.fetchSource != "https://gitlab.com/team/sub/app.git" || selected.pushSource != "https://gitlab.com/team/sub/app.git" {
		t.Fatal(selected, err)
	}
}
