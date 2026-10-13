package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
	"mag-ik-een-review/internal/githubclient"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
}

func (m *memorySecrets) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token := m.values[service+":"+user]; token != "" {
		return token, nil
	}
	return "", keyring.ErrNotFound
}
func (m *memorySecrets) Set(service, user, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("keyring unavailable")
	}
	m.values[service+":"+user] = token
	return nil
}
func (m *memorySecrets) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, service+":"+user)
	return nil
}
func fakeCredentials(t *testing.T) *CredentialService {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v3/user" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer accepted-secret" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "invalid accepted-secret"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "noah", "name": "Noah", "avatar_url": "https://avatar.example/noah"})
	}))
	t.Cleanup(server.Close)
	return &CredentialService{store: &memorySecrets{values: map[string]string{}}, configPath: filepath.Join(t.TempDir(), "accounts.json"), accounts: map[string]Account{}, cli: func(context.Context, ...string) ([]byte, error) { return nil, errors.New("not authenticated") }, client: func(host, token string) (*githubclient.Client, error) {
		return githubclient.NewWithHTTPClient(server.URL, token, server.Client())
	}}
}

func TestSaveTokenVerifiedAndOnlyStoredInKeychain(t *testing.T) {
	service := fakeCredentials(t)
	if _, err := service.SaveToken(context.Background(), "ghe.example.test", "rejected-secret"); err == nil {
		t.Fatal("accepted invalid token")
	}
	store := service.store.(*memorySecrets)
	if len(store.values) != 0 {
		t.Fatal("stored an unverified token")
	}
	account, err := service.SaveToken(context.Background(), "https://GHE.Example.test/api/v3/", "accepted-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !account.Authenticated || account.Host != "ghe.example.test" || account.Login != "noah" || account.Source != "saved" {
		t.Fatal(account)
	}
	data, err := os.ReadFile(service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatal("plaintext token in metadata")
	}
	if !strings.Contains(string(data), "noah") {
		t.Fatal("missing nonsecret account metadata")
	}
	service.cli = func(context.Context, ...string) ([]byte, error) {
		t.Error("CLI should not override explicit saved token")
		return nil, errors.New("unexpected")
	}
	status, err := service.AccountStatus(context.Background(), "ghe.example.test")
	if err != nil || !status.Authenticated || status.Source != "saved" {
		t.Fatal(status, err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "accepted-secret") {
		t.Fatal("returned secret to frontend")
	}
}

func TestCLIHostBindingFallbackAndDelete(t *testing.T) {
	service := fakeCredentials(t)
	_, err := service.SaveToken(context.Background(), "ghe.example.test", "accepted-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteToken(context.Background(), "ghe.example.test"); err != nil {
		t.Fatal(err)
	}
	service.cli = func(ctx context.Context, args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"auth", "token", "--hostname", "ghe.example.test"}) {
			t.Fatalf("unsafe CLI args: %v", args)
		}
		return []byte("accepted-secret\n"), nil
	}
	status, err := service.AccountStatus(context.Background(), "https://ghe.example.test/")
	if err != nil || !status.Authenticated || status.Source != "gh" {
		t.Fatal(status, err)
	}
	if len(service.store.(*memorySecrets).values) != 0 {
		t.Fatal("CLI token copied into saved credential storage")
	}
	if _, _, _, err := service.resolveToken(context.Background(), "-malicious"); err == nil {
		t.Fatal("unsafe hostname reached CLI")
	}
}

func TestKeychainFailureDoesNotFallBackToPlaintext(t *testing.T) {
	service := fakeCredentials(t)
	service.store.(*memorySecrets).fail = true
	if _, err := service.SaveToken(context.Background(), "github.com", "accepted-secret"); err == nil {
		t.Fatal("keychain failure ignored")
	}
	if _, err := os.Stat(service.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote config despite failed keychain save", err)
	}
	service.cli = func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("stderr secret that must not escape")
	}
	status, err := service.AccountStatus(context.Background(), "github.com")
	if err != nil || status.Authenticated || strings.Contains(status.Error, "stderr secret") {
		t.Fatal(status, err)
	}
}

func TestAccountsDiscoverCLIHostsWithoutReturningTokens(t *testing.T) {
	service := fakeCredentials(t)
	service.accounts["github.com"] = Account{Host: "github.com", Login: "saved-user", Source: "saved"}
	service.cli = func(context.Context, ...string) ([]byte, error) {
		return []byte(`{"hosts":{"github.com":[{"login":"cli-user","active":true,"oauth_token":"private-token"}],"ghe.example.test":[{"login":"old-user"},{"login":"active-user","active":true}]}}`), nil
	}
	accounts, err := service.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].Login != "active-user" || accounts[1].Login != "saved-user" {
		t.Fatal(accounts)
	}
	data, _ := json.Marshal(accounts)
	if strings.Contains(string(data), "token") {
		t.Fatal("leaked CLI account token")
	}
}
