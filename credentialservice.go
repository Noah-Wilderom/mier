package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	"mag-ik-een-review/internal/githubclient"
)

const credentialKeyringService = "mag-ik-een-review.github"

type Account struct {
	Host   string `json:"host"`
	Login  string `json:"login"`
	Source string `json:"source"`
}
type AccountStatus struct {
	Host          string `json:"host"`
	Authenticated bool   `json:"authenticated"`
	Login         string `json:"login"`
	Name          string `json:"name"`
	AvatarURL     string `json:"avatarUrl"`
	Source        string `json:"source"`
	CLIInstalled  bool   `json:"cliInstalled"`
	Error         string `json:"error"`
}
type secretStore interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}
type systemSecretStore struct{}

func (systemSecretStore) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemSecretStore) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}
func (systemSecretStore) Delete(service, user string) error { return keyring.Delete(service, user) }

type CredentialService struct {
	mu         sync.Mutex
	store      secretStore
	configPath string
	accounts   map[string]Account
	cli        func(context.Context, ...string) ([]byte, error)
	client     func(string, string) (*githubclient.Client, error)
}

func NewCredentialService() *CredentialService {
	directory, err := os.UserConfigDir()
	if err != nil {
		directory = ""
	}
	s := &CredentialService{store: systemSecretStore{}, accounts: map[string]Account{}, cli: runGitHubCLI, client: githubclient.New}
	if directory != "" {
		s.configPath = filepath.Join(directory, "mag-ik-een-review", "accounts.json")
		if data, err := os.ReadFile(s.configPath); err == nil {
			var accounts []Account
			if json.Unmarshal(data, &accounts) == nil {
				for _, account := range accounts {
					if host, err := githubclient.NormalizeHost(account.Host); err == nil {
						s.accounts[host] = Account{Host: host, Login: account.Login, Source: account.Source}
					}
				}
			}
		}
	}
	return s
}

type cappedOutput struct {
	bytes.Buffer
	limit int
}

func (w *cappedOutput) Write(data []byte) (int, error) {
	if w.Len()+len(data) > w.limit {
		return 0, errors.New("GitHub CLI output exceeded its limit")
	}
	return w.Buffer.Write(data)
}
func runGitHubCLI(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "gh", args...)
	command.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	output := &cappedOutput{limit: 64 * 1024}
	command.Stdout = output
	// stderr may contain authentication diagnostics; it must never cross the frontend boundary.
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("GitHub CLI is not authenticated for this host")
	}
	return output.Bytes(), nil
}

func (s *CredentialService) resolveToken(ctx context.Context, input string) (string, string, string, error) {
	host, err := githubclient.NormalizeHost(input)
	if err != nil {
		return "", "", "", err
	}
	if token, err := s.store.Get(credentialKeyringService, host); err == nil && strings.TrimSpace(token) != "" {
		return host, strings.TrimSpace(token), "saved", nil
	}
	data, err := s.cli(ctx, "auth", "token", "--hostname", host)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return host, strings.TrimSpace(string(data)), "gh", nil
	}
	return host, "", "", fmt.Errorf("Sign in with gh auth login --hostname %s, or save a token in Accounts", host)
}

func (s *CredentialService) AccountStatus(ctx context.Context, input string) (AccountStatus, error) {
	host, err := githubclient.NormalizeHost(input)
	if err != nil {
		return AccountStatus{}, err
	}
	_, cliErr := exec.LookPath("gh")
	status := AccountStatus{Host: host, CLIInstalled: cliErr == nil}
	_, token, source, err := s.resolveToken(ctx, host)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.Source = source
	client, err := s.client(host, token)
	if err != nil {
		return status, err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.Authenticated = true
	status.Login = user.Login
	status.Name = user.Name
	status.AvatarURL = user.AvatarURL
	s.mu.Lock()
	s.accounts[host] = Account{Host: host, Login: user.Login, Source: source}
	_ = s.persistLocked()
	s.mu.Unlock()
	return status, nil
}
func (s *CredentialService) SaveToken(ctx context.Context, input, token string) (AccountStatus, error) {
	host, err := githubclient.NormalizeHost(input)
	if err != nil {
		return AccountStatus{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return AccountStatus{}, errors.New("Enter a valid GitHub token")
	}
	client, err := s.client(host, token)
	if err != nil {
		return AccountStatus{}, err
	}
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	if err := s.store.Set(credentialKeyringService, host, token); err != nil {
		return AccountStatus{}, errors.New("The operating system credential store is unavailable. Unlock your keychain or use gh auth login")
	}
	s.mu.Lock()
	s.accounts[host] = Account{Host: host, Login: user.Login, Source: "saved"}
	err = s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return AccountStatus{}, fmt.Errorf("Token was saved securely, but account metadata could not be saved: %w", err)
	}
	_, cliErr := exec.LookPath("gh")
	return AccountStatus{Host: host, Authenticated: true, Login: user.Login, Name: user.Name, AvatarURL: user.AvatarURL, Source: "saved", CLIInstalled: cliErr == nil}, nil
}
func (s *CredentialService) DeleteToken(ctx context.Context, input string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host, err := githubclient.NormalizeHost(input)
	if err != nil {
		return err
	}
	if err := s.store.Delete(credentialKeyringService, host); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errors.New("Unable to remove the saved token from the operating system credential store")
	}
	s.mu.Lock()
	delete(s.accounts, host)
	err = s.persistLocked()
	s.mu.Unlock()
	return err
}
func (s *CredentialService) ListAccounts(ctx context.Context) ([]Account, error) {
	accounts := map[string]Account{}
	s.mu.Lock()
	for host, account := range s.accounts {
		accounts[host] = account
	}
	s.mu.Unlock()
	if data, err := s.cli(ctx, "auth", "status", "--json", "hosts"); err == nil {
		var status struct {
			Hosts map[string][]struct {
				Login  string `json:"login"`
				Active bool   `json:"active"`
			} `json:"hosts"`
		}
		if json.Unmarshal(data, &status) == nil {
			for input, users := range status.Hosts {
				host, err := githubclient.NormalizeHost(input)
				if err != nil {
					continue
				}
				if accounts[host].Source == "saved" {
					continue
				}
				for i, user := range users {
					if i == 0 || user.Active {
						accounts[host] = Account{Host: host, Login: user.Login, Source: "gh"}
						if user.Active {
							break
						}
					}
				}
			}
		}
	}
	result := []Account{}
	for _, account := range accounts {
		result = append(result, account)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Host < result[j].Host })
	return result, nil
}
func (s *CredentialService) persistLocked() error {
	if s.configPath == "" {
		return errors.New("The system configuration directory is unavailable")
	}
	accounts := []Account{}
	for _, account := range s.accounts {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Host < accounts[j].Host })
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.configPath), "accounts-*.json")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.configPath)
}
