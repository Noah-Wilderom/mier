package main

import (
	"context"
	"encoding/json"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"mag-ik-een-review/internal/reviewproviders"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type providerAccountClient struct {
	reviewproviders.API
	user githubclient.User
	err  error
}

func (c providerAccountClient) CurrentUser(context.Context) (githubclient.User, error) {
	return c.user, c.err
}
func providerServiceFixture(t *testing.T) (*ReviewProviderService, *CredentialService) {
	t.Helper()
	credentials := &CredentialService{store: &memorySecrets{values: map[string]string{}}, accounts: map[string]Account{}, configPath: filepath.Join(t.TempDir(), "github.json")}
	s := &ReviewProviderService{store: credentials.store, configPath: filepath.Join(t.TempDir(), "providers.json"), connections: map[string]ReviewConnection{}, clients: map[string]reviewproviders.API{}, publisher: reviewproviders.NewPublisher(nil)}
	s.factory = func(connection ReviewConnection, token string, options reviewproviders.Options) (reviewproviders.API, error) {
		if token == "rejected" {
			return providerAccountClient{err: errors.New("not authorized")}, nil
		}
		return providerAccountClient{user: githubclient.User{Login: "tester", Name: "Test Person", AvatarURL: "https://example.invalid/avatar.png"}}, nil
	}
	credentials.providers = s
	return s, credentials
}
func TestReviewProviderVerifiesAndStoresOnlyKeychainSecrets(t *testing.T) {
	s, credentials := providerServiceFixture(t)
	request := ReviewConnectRequest{Kind: "gitlab", Host: "gitlab.company.test", Token: "rejected", ReadOnly: true}
	if _, err := s.Connect(context.Background(), request); err == nil {
		t.Fatal("unverified token saved")
	}
	store := s.store.(*memorySecrets)
	if len(store.values) != 0 {
		t.Fatal("stored rejected token")
	}
	request.Token = "accepted-provider-secret"
	status, err := s.Connect(context.Background(), request)
	if err != nil || !status.Authenticated || !status.ReadOnly || status.Provider != "gitlab" {
		t.Fatal(status, err)
	}
	data, err := os.ReadFile(s.configPath)
	if err != nil || strings.Contains(string(data), request.Token) {
		t.Fatal("secret in provider metadata", err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), request.Token) {
		t.Fatal("returned token in account status")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(s.configPath)
		if info.Mode().Perm()&0077 != 0 {
			t.Fatal("public account settings", info.Mode())
		}
	}
	credentials.cli = func(context.Context, ...string) ([]byte, error) {
		t.Error("GitHub CLI queried for GitLab credentials")
		return nil, errors.New("unexpected")
	}
	status, err = credentials.AccountStatus(context.Background(), request.Host)
	if err != nil || !status.Authenticated {
		t.Fatal(status, err)
	}
	host, token, _, err := credentials.resolveToken(context.Background(), request.Host)
	if err != nil || host != request.Host || token != request.Token {
		t.Fatal(host, err)
	}
	username, token, err := s.gitCredential(context.Background(), request.Host)
	if err != nil || username != "oauth2" || token != request.Token {
		t.Fatal(username, err)
	}
	if err = s.Disconnect(context.Background(), request.Host); err != nil {
		t.Fatal(err)
	}
	if s.Kind(request.Host) != "gitlab" {
		t.Fatal("lost self-hosted provider identity on disconnect")
	}
	status, err = s.Status(context.Background(), request.Host)
	if err != nil || status.Authenticated {
		t.Fatal(status, err)
	}
	if len(store.values) != 0 {
		t.Fatal("disconnect kept credential")
	}
}
func TestProviderSettingsFailureRollsBackSecretAndAccount(t *testing.T) {
	s, _ := providerServiceFixture(t)
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	s.configPath = filepath.Join(parent, "providers.json")
	if _, err := s.Connect(context.Background(), ReviewConnectRequest{Kind: "gitlab", Host: "gitlab.company.test", Token: "new-secret"}); err == nil {
		t.Fatal("failed persistence accepted")
	}
	if len(s.store.(*memorySecrets).values) != 0 || len(s.connections) != 0 {
		t.Fatal("failed account save kept new credentials")
	}
	s.configPath = filepath.Join(t.TempDir(), "providers.json")
	s.store.(*memorySecrets).fail = true
	if _, err := s.Connect(context.Background(), ReviewConnectRequest{Kind: "gitlab", Host: "gitlab.company.test", Token: "new-secret"}); err == nil || !strings.Contains(err.Error(), "keychain") {
		t.Fatal(err)
	}
}
func TestReviewPublicationReceiptsStayPrivateAndRejectPathEscapes(t *testing.T) {
	s := &reviewProgressFiles{directory: filepath.Join(t.TempDir(), "receipts")}
	key := strings.Repeat("a", 64)
	progress := reviewproviders.PublicationProgress{StartedAt: "2026-10-16T00:00:00Z", Comments: map[string]reviewproviders.PublishedComment{"draft": {ID: 55, Hash: strings.Repeat("b", 64)}}, Votes: map[string]bool{"APPROVE": true}}
	if err := s.Save(key, progress); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load(key)
	if err != nil || loaded.Comments["draft"].ID != 55 || !loaded.Votes["APPROVE"] {
		t.Fatal(loaded, err)
	}
	if err = s.Save("../escape", progress); err == nil {
		t.Fatal("receipt escaped private storage")
	}
	if _, err = s.Load(strings.Repeat("z", 64)); err == nil {
		t.Fatal("invalid receipt key accepted")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(s.directory, key+".json"))
		if info.Mode().Perm()&0077 != 0 {
			t.Fatal("public review receipt")
		}
	}
}

func TestProviderUnreadableSettingsArePreservedBeforeCredentialWrites(t *testing.T) {
	s, credentials := providerServiceFixture(t)
	broken := []byte("keep unreadable provider metadata")
	if err := os.WriteFile(s.configPath, broken, 0600); err != nil {
		t.Fatal(err)
	}
	s.configErr = errors.New("Saved provider settings are unreadable")
	if _, err := s.Connect(context.Background(), ReviewConnectRequest{Kind: "gitlab", Host: "gitlab.company.test", Token: "synthetic-token"}); err == nil {
		t.Fatal("unreadable settings overwritten")
	}
	data, err := os.ReadFile(s.configPath)
	if err != nil || string(data) != string(broken) || len(credentials.store.(*memorySecrets).values) != 0 {
		t.Fatal("settings or credentials changed", err)
	}
	oversized := filepath.Join(t.TempDir(), "oversized.json")
	if err = os.WriteFile(oversized, []byte(strings.Repeat("x", 2*1024*1024+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readReviewSettingsFile(oversized); err == nil {
		t.Fatal("oversized settings accepted")
	}
}
