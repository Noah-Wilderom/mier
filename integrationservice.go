package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const jiraKeyringService = "mag-ik-een-review.jira"
const defaultJiraCallbackPort = 43827

var jiraScopes = []string{"read:jira-work", "offline_access"}
var jiraClientID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,256}$`)

type JiraSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}
type JiraStatus struct {
	Configured    bool       `json:"configured"`
	Connected     bool       `json:"connected"`
	ClientID      string     `json:"clientId"`
	CallbackPort  int        `json:"callbackPort"`
	CallbackURL   string     `json:"callbackUrl"`
	Scopes        []string   `json:"scopes"`
	State         string     `json:"state"`
	Error         string     `json:"error"`
	Sites         []JiraSite `json:"sites"`
	DefaultSiteID string     `json:"defaultSiteId"`
}
type JiraAppConfiguration struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	CallbackPort int    `json:"callbackPort"`
}
type JiraRepositoryPreferences struct {
	SiteID          string   `json:"siteId"`
	Projects        []string `json:"projects"`
	AcceptanceField string   `json:"acceptanceField"`
}
type jiraSelection struct {
	Pinned []string `json:"pinned"`
	Hidden []string `json:"hidden"`
}
type jiraConfiguration struct {
	ClientID      string                               `json:"clientId"`
	CallbackPort  int                                  `json:"callbackPort"`
	Sites         []JiraSite                           `json:"sites"`
	DefaultSiteID string                               `json:"defaultSiteId"`
	Repositories  map[string]JiraRepositoryPreferences `json:"repositories"`
	Selections    map[string]jiraSelection             `json:"selections"`
}

// Secret values live in the OS credential store, never in config or responses.
type jiraSecrets struct {
	ClientSecret string    `json:"clientSecret"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}
type IntegrationService struct {
	mu              sync.Mutex
	refreshMu       sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	store           secretStore
	configPath      string
	config          jiraConfiguration
	configError     string
	secrets         jiraSecrets
	secretsLoaded   bool
	secretsDirty    bool
	state           string
	connectionError string
	pending         *jiraAuthorization
	epoch           uint64
	closed          bool
	client          *http.Client
	cache           map[string]jiraCachedIssue
	retryAt         time.Time
}

func NewIntegrationService() *IntegrationService {
	directory, err := os.UserConfigDir()
	path := ""
	if err == nil {
		path = filepath.Join(directory, "mag-ik-een-review", "jira.json")
	}
	return newJiraIntegration(path, systemSecretStore{}, &http.Client{Timeout: 20 * time.Second})
}
func newJiraIntegration(path string, store secretStore, client *http.Client) *IntegrationService {
	ctx, cancel := context.WithCancel(context.Background())
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &IntegrationService{ctx: ctx, cancel: cancel, store: &jiraKeychainStore{base: store}, configPath: path, state: "idle", client: &copyClient, cache: map[string]jiraCachedIssue{}}
	s.config.CallbackPort = defaultJiraCallbackPort
	s.config.Repositories = map[string]JiraRepositoryPreferences{}
	s.config.Selections = map[string]jiraSelection{}
	if path != "" {
		file, err := os.Open(path)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
			_ = file.Close()
			candidate := jiraConfiguration{CallbackPort: defaultJiraCallbackPort}
			if readErr != nil || len(data) > 1024*1024 || json.Unmarshal(data, &candidate) != nil || !validJiraConfiguration(candidate) {
				s.configError = "Jira configuration could not be read. Save your app configuration again."
			} else {
				s.config = candidate
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			s.configError = "Jira configuration could not be read."
		}
	}
	if s.config.CallbackPort < 1024 || s.config.CallbackPort > 65535 {
		s.config.CallbackPort = defaultJiraCallbackPort
	}
	if s.config.Repositories == nil {
		s.config.Repositories = map[string]JiraRepositoryPreferences{}
	}
	if s.config.Selections == nil {
		s.config.Selections = map[string]jiraSelection{}
	}
	return s
}
func validJiraConfiguration(value jiraConfiguration) bool {
	if value.ClientID != "" && !jiraClientID.MatchString(value.ClientID) || value.CallbackPort < 1024 || value.CallbackPort > 65535 || len(value.Sites) > 32 || len(value.Repositories) > 200 || len(value.Selections) > 1000 {
		return false
	}
	for _, site := range value.Sites {
		if !validJiraSite(site) {
			return false
		}
	}
	for _, preferences := range value.Repositories {
		if preferences.SiteID != "" && !jiraSiteID.MatchString(preferences.SiteID) || len(preferences.Projects) > 32 || preferences.AcceptanceField != "" && !jiraCustomField.MatchString(preferences.AcceptanceField) {
			return false
		}
		for _, project := range preferences.Projects {
			if !jiraProjectKey.MatchString(project) {
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
func (s *IntegrationService) requestContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
func (s *IntegrationService) loadSecretsLocked() error {
	if s.secretsLoaded || s.config.ClientID == "" {
		return nil
	}
	data, err := s.store.Get(jiraKeyringService, s.config.ClientID)
	if errors.Is(err, keyring.ErrNotFound) {
		s.secretsLoaded = true
		return nil
	}
	if err != nil {
		if errors.Is(err, errJiraKeychainCorrupt) {
			return errors.New("Saved Jira credentials are incomplete. Configure your app and authorize again.")
		}
		return errors.New("Jira credentials could not be read from the OS keychain. Check keychain access and retry.")
	}
	var credentials jiraSecrets
	if len(data) > maxJiraSecretsBytes || json.Unmarshal([]byte(data), &credentials) != nil {
		return errors.New("Saved Jira credentials could not be read. Configure your app again.")
	}
	s.secrets = credentials
	s.secretsLoaded = true
	s.secretsDirty = false
	return nil
}
func (s *IntegrationService) saveSecretsLocked(value jiraSecrets) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := s.store.Set(jiraKeyringService, s.config.ClientID, string(data)); err != nil {
		if errors.Is(err, keyring.ErrSetDataTooBig) {
			return errors.New("Jira credentials exceed the secure storage size limit (64 KiB).")
		}
		return errors.New("Jira credentials could not be saved in the OS keychain. Check keychain access and retry.")
	}
	s.secrets = value
	s.secretsLoaded = true
	s.secretsDirty = false
	return nil
}
func writeJiraConfiguration(path string, value jiraConfiguration) error {
	if path == "" {
		return errors.New("OS configuration directory is unavailable")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("Saved Jira settings have reached their size limit")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("Jira configuration could not be saved")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".jira-*")
	if err != nil {
		return errors.New("Jira configuration could not be saved")
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("Jira configuration could not be saved")
	}
	return nil
}
func (s *IntegrationService) GetJiraStatus(ctx context.Context) (JiraStatus, error) {
	if err := ctx.Err(); err != nil {
		return JiraStatus{}, err
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
	return JiraStatus{Configured: s.config.ClientID != "" && s.secrets.ClientSecret != "", Connected: s.secrets.RefreshToken != "" && len(s.config.Sites) > 0, ClientID: s.config.ClientID, CallbackPort: s.config.CallbackPort, CallbackURL: jiraCallbackURL(s.config.CallbackPort), Scopes: append([]string{}, jiraScopes...), State: s.state, Error: issue, Sites: append([]JiraSite{}, s.config.Sites...), DefaultSiteID: s.config.DefaultSiteID}, nil
}
func (s *IntegrationService) ConfigureJira(ctx context.Context, input JiraAppConfiguration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ClientSecret = strings.TrimSpace(input.ClientSecret)
	if !jiraClientID.MatchString(input.ClientID) {
		return errors.New("Enter the client ID from your Atlassian OAuth app settings")
	}
	if input.CallbackPort < 1024 || input.CallbackPort > 65535 {
		return errors.New("Callback port must be between 1024 and 65535")
	}
	if len(input.ClientSecret) > 4096 || strings.ContainsAny(input.ClientSecret, "\r\n\x00") {
		return errors.New("Enter a valid OAuth client secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Jira integration has shut down")
	}
	if err := s.loadSecretsLocked(); err != nil && input.ClientSecret == "" {
		return err
	}
	secret := input.ClientSecret
	if secret == "" && input.ClientID == s.config.ClientID {
		secret = s.secrets.ClientSecret
	}
	if secret == "" {
		return errors.New("Enter your OAuth app's client secret")
	}
	oldConfig, oldSecrets := s.config, s.secrets
	changed := input.ClientID != oldConfig.ClientID || secret != oldSecrets.ClientSecret || input.CallbackPort != oldConfig.CallbackPort
	next := oldConfig
	next.ClientID = input.ClientID
	next.CallbackPort = input.CallbackPort
	credentials := oldSecrets
	credentials.ClientSecret = secret
	if changed {
		credentials = jiraSecrets{ClientSecret: secret}
		next.Sites = nil
		next.DefaultSiteID = ""
	}
	s.config = next
	if err := s.saveSecretsLocked(credentials); err != nil {
		s.config = oldConfig
		return err
	}
	if err := writeJiraConfiguration(s.configPath, next); err != nil {
		if input.ClientID == oldConfig.ClientID {
			_ = s.saveSecretsLocked(oldSecrets)
		} else {
			_ = s.store.Delete(jiraKeyringService, input.ClientID)
			s.secrets = oldSecrets
		}
		s.config = oldConfig
		return err
	}
	if oldConfig.ClientID != "" && input.ClientID != oldConfig.ClientID {
		_ = s.store.Delete(jiraKeyringService, oldConfig.ClientID)
	}
	if s.pending != nil {
		s.pending.cancel()
		s.pending = nil
	}
	s.state = "idle"
	s.connectionError = ""
	s.configError = ""
	s.epoch++
	s.cache = map[string]jiraCachedIssue{}
	s.retryAt = time.Time{}
	return nil
}
func (s *IntegrationService) DisconnectJira(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadSecretsLocked(); err != nil {
		return err
	}
	if err := s.saveSecretsLocked(jiraSecrets{ClientSecret: s.secrets.ClientSecret}); err != nil {
		return err
	}
	if s.pending != nil {
		s.pending.cancel()
		s.pending = nil
	}
	s.epoch++
	s.cache = map[string]jiraCachedIssue{}
	s.retryAt = time.Time{}
	s.state = "idle"
	s.connectionError = ""
	s.config.Sites = nil
	s.config.DefaultSiteID = ""
	return writeJiraConfiguration(s.configPath, s.config)
}
func (s *IntegrationService) SetJiraDefaultSite(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, site := range s.config.Sites {
		if site.ID == id {
			next := s.config
			next.DefaultSiteID = id
			if err := writeJiraConfiguration(s.configPath, next); err != nil {
				return err
			}
			s.config = next
			s.epoch++
			s.cache = map[string]jiraCachedIssue{}
			return nil
		}
	}
	return errors.New("Choose a Jira site authorized by your account")
}
func (s *IntegrationService) ServiceShutdown() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.pending != nil {
		s.pending.cancel()
		s.pending = nil
	}
	s.secrets = jiraSecrets{}
	s.cache = map[string]jiraCachedIssue{}
	return nil
}
func (s *IntegrationService) InstallJira(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("Configure your Jira OAuth app in Settings, then authorize your account")
}
