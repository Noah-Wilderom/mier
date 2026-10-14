package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const linearKeyringService = "mag-ik-een-review.linear"
const defaultLinearCallbackPort = 43828

type LinearWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}
type LinearStatus struct {
	Configured   bool             `json:"configured"`
	Connected    bool             `json:"connected"`
	ClientID     string           `json:"clientId"`
	CallbackPort int              `json:"callbackPort"`
	CallbackURL  string           `json:"callbackUrl"`
	Scopes       []string         `json:"scopes"`
	State        string           `json:"state"`
	Error        string           `json:"error"`
	Workspace    *LinearWorkspace `json:"workspace"`
	Account      string           `json:"account"`
}
type LinearAppConfiguration struct {
	ClientID     string `json:"clientId"`
	CallbackPort int    `json:"callbackPort"`
}
type LinearRepositoryPreferences struct {
	Teams []string `json:"teams"`
}
type linearConfiguration struct {
	ClientID     string                                 `json:"clientId"`
	CallbackPort int                                    `json:"callbackPort"`
	CredentialID string                                 `json:"credentialId"`
	Workspace    *LinearWorkspace                       `json:"workspace"`
	Account      string                                 `json:"account"`
	Repositories map[string]LinearRepositoryPreferences `json:"repositories"`
	Selections   map[string]jiraSelection               `json:"selections"`
}
type linearSecrets struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}
type linearCachedIssue struct {
	issue   TicketIssue
	expires time.Time
}
type LinearService struct {
	mu, refreshMu                       sync.Mutex
	ctx                                 context.Context
	cancel                              context.CancelFunc
	store                               secretStore
	client                              *http.Client
	configPath                          string
	config                              linearConfiguration
	configError, state, connectionError string
	secrets                             linearSecrets
	secretsLoaded, secretsDirty, closed bool
	pending                             *linearAuthorization
	epoch                               uint64
	cache                               map[string]linearCachedIssue
	retryAt                             time.Time
}

func NewLinearService() *LinearService {
	directory, err := os.UserConfigDir()
	path := ""
	if err == nil {
		path = filepath.Join(directory, "mag-ik-een-review", "linear.json")
	}
	return newLinearService(path, systemSecretStore{}, &http.Client{Timeout: 20 * time.Second})
}

func newLinearService(path string, store secretStore, client *http.Client) *LinearService {
	ctx, cancel := context.WithCancel(context.Background())
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &LinearService{ctx: ctx, cancel: cancel, store: &jiraKeychainStore{base: store}, client: &copyClient, configPath: path, state: "idle", cache: map[string]linearCachedIssue{}}
	s.config = linearConfiguration{CallbackPort: defaultLinearCallbackPort}
	if path != "" {
		file, err := os.Open(path)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
			_ = file.Close()
			var candidate linearConfiguration
			if readErr != nil || len(data) > 1024*1024 || json.Unmarshal(data, &candidate) != nil || !validLinearConfiguration(candidate) {
				s.configError = "Linear configuration could not be read. Save your app configuration again."
			} else {
				s.config = candidate
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			s.configError = "Linear configuration could not be read."
		}
	}
	if s.config.Repositories == nil {
		s.config.Repositories = map[string]LinearRepositoryPreferences{}
	}
	if s.config.Selections == nil {
		s.config.Selections = map[string]jiraSelection{}
	}
	return s
}

func validLinearConfiguration(value linearConfiguration) bool {
	if value.CallbackPort < 1024 || value.CallbackPort > 65535 || len(value.Repositories) > 200 || len(value.Selections) > 1000 || len(value.Account) > 1024 {
		return false
	}
	if value.ClientID != "" && (!jiraClientID.MatchString(value.ClientID) || !jiraHexValue(value.CredentialID, 16)) {
		return false
	}
	if value.Workspace != nil && !validLinearWorkspace(*value.Workspace) {
		return false
	}
	for _, preferences := range value.Repositories {
		if len(preferences.Teams) > 32 {
			return false
		}
		for _, key := range preferences.Teams {
			if !jiraProjectKey.MatchString(key) {
				return false
			}
		}
	}
	for _, selection := range value.Selections {
		if len(selection.Pinned) > 25 || len(selection.Hidden) > 100 {
			return false
		}
		for _, key := range append(append([]string{}, selection.Pinned...), selection.Hidden...) {
			if !jiraIssueKey.MatchString(key) {
				return false
			}
		}
	}
	return true
}

func writeLinearConfiguration(path string, value linearConfiguration) error {
	if path == "" {
		return errors.New("OS configuration directory is unavailable")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(data) > 1024*1024 {
		return errors.New("Saved Linear settings have reached their size limit")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("Linear configuration could not be saved")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".linear-*")
	if err != nil {
		return errors.New("Linear configuration could not be saved")
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		return errors.New("Linear configuration could not be saved")
	}
	return nil
}

func (s *LinearService) loadSecretsLocked() error {
	if s.secretsLoaded || s.config.CredentialID == "" {
		return nil
	}
	data, err := s.store.Get(linearKeyringService, s.config.CredentialID)
	if errors.Is(err, keyring.ErrNotFound) {
		s.secretsLoaded = true
		return nil
	}
	if err != nil {
		return errors.New("Linear credentials could not be read from the OS keychain. Check keychain access and authorize again.")
	}
	var credentials linearSecrets
	if len(data) > maxJiraSecretsBytes || json.Unmarshal([]byte(data), &credentials) != nil || strings.ContainsAny(credentials.AccessToken+credentials.RefreshToken, "\r\n\x00") {
		return errors.New("Saved Linear credentials could not be read. Authorize again in Settings.")
	}
	s.secrets = credentials
	s.secretsLoaded = true
	return nil
}

func (s *LinearService) saveSecretsLocked(value linearSecrets) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = s.store.Set(linearKeyringService, s.config.CredentialID, string(data)); err != nil {
		return errors.New("Linear credentials could not be saved in the OS keychain. Check keychain access and retry.")
	}
	s.secrets, s.secretsLoaded, s.secretsDirty = value, true, false
	return nil
}

func (s *LinearService) GetLinearStatus(ctx context.Context) (LinearStatus, error) {
	if err := ctx.Err(); err != nil {
		return LinearStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issue := s.configError
	if err := s.loadSecretsLocked(); err != nil {
		issue = err.Error()
	}
	if s.connectionError != "" {
		issue = s.connectionError
	}
	var workspace *LinearWorkspace
	if s.config.Workspace != nil {
		copy := *s.config.Workspace
		workspace = &copy
	}
	return LinearStatus{Configured: s.config.ClientID != "", Connected: s.secrets.RefreshToken != "" && workspace != nil, ClientID: s.config.ClientID, CallbackPort: s.config.CallbackPort, CallbackURL: linearCallbackURL(s.config.CallbackPort), Scopes: []string{"read"}, State: s.state, Error: issue, Workspace: workspace, Account: s.config.Account}, nil
}

func (s *LinearService) ConfigureLinear(ctx context.Context, input LinearAppConfiguration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input.ClientID = strings.TrimSpace(input.ClientID)
	if !jiraClientID.MatchString(input.ClientID) {
		return errors.New("Enter the client ID from your Linear OAuth app settings")
	}
	if input.CallbackPort < 1024 || input.CallbackPort > 65535 {
		return errors.New("Callback port must be between 1024 and 65535")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Linear integration has shut down")
	}
	next := s.config
	changed := next.ClientID != input.ClientID || next.CallbackPort != input.CallbackPort || s.configError != ""
	if changed {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return errors.New("Linear configuration could not be prepared")
		}
		next.CredentialID = hex.EncodeToString(nonce[:])
		next.Workspace, next.Account = nil, ""
	}
	next.ClientID, next.CallbackPort = input.ClientID, input.CallbackPort
	if err := writeLinearConfiguration(s.configPath, next); err != nil {
		return err
	}
	if changed {
		if s.pending != nil {
			_ = s.pending.listener.Close()
			s.pending.cancel()
			s.pending = nil
		}
		// A new credential reference prevents old grants from being reused even
		// if removal is temporarily denied by a locked OS keychain.
		if s.config.CredentialID != "" {
			_ = s.store.Delete(linearKeyringService, s.config.CredentialID)
		}
		s.secrets, s.secretsLoaded, s.secretsDirty = linearSecrets{}, true, false
		s.epoch++
		s.cache = map[string]linearCachedIssue{}
		s.state, s.connectionError, s.retryAt = "idle", "", time.Time{}
	}
	s.config, s.configError = next, ""
	return nil
}

func (s *LinearService) DisconnectLinear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.ClientID == "" {
		return nil
	}
	if err := s.loadSecretsLocked(); err != nil {
		return err
	}
	old := s.secrets
	if err := s.saveSecretsLocked(linearSecrets{}); err != nil {
		return err
	}
	next := s.config
	next.Workspace, next.Account = nil, ""
	if err := writeLinearConfiguration(s.configPath, next); err != nil {
		_ = s.saveSecretsLocked(old)
		return err
	}
	if s.pending != nil {
		_ = s.pending.listener.Close()
		s.pending.cancel()
		s.pending = nil
	}
	s.config = next
	s.epoch++
	s.cache = map[string]linearCachedIssue{}
	s.state, s.connectionError, s.retryAt = "idle", "", time.Time{}
	return nil
}

func (s *LinearService) requestContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (s *LinearService) ServiceShutdown() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.pending != nil {
		_ = s.pending.listener.Close()
		s.pending.cancel()
		s.pending = nil
	}
	s.secrets = linearSecrets{}
	s.cache = map[string]linearCachedIssue{}
	return nil
}
