package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mag-ik-een-review/internal/githubclient"
)

type NotificationPreferences struct {
	ToastsEnabled   bool `json:"toastsEnabled"`
	DesktopEnabled  bool `json:"desktopEnabled"`
	Opened          bool `json:"opened"`
	Closed          bool `json:"closed"`
	Merged          bool `json:"merged"`
	ReviewRequested bool `json:"reviewRequested"`
}

type PRNotificationEvent struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Host      string `json:"host"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Number    int    `json:"number"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Actor     string `json:"actor"`
	Toast     bool   `json:"toast"`
	Desktop   bool   `json:"desktop"`
	CreatedAt string `json:"createdAt"`
}

type notificationAccountProvider interface {
	AccountStatus(context.Context, string) (AccountStatus, error)
}

type observedPullRequest struct {
	status    string
	requested map[string]bool
	updatedAt time.Time
}

type notificationAccount struct {
	login     string
	expiresAt time.Time
}

// PRNotificationService keeps preferences separate from credentials and observes
// successful complete PR snapshots. Native publishing is injected by the app.
type PRNotificationService struct {
	mu                sync.Mutex
	permissionMu      sync.Mutex
	credentials       notificationAccountProvider
	emit              func(PRNotificationEvent)
	requestPermission func() error
	permissionGranted bool
	configPath        string
	configError       error
	loaded            bool
	preferences       map[string]NotificationPreferences
	observed          map[string]map[int]observedPullRequest
	accounts          map[string]notificationAccount
	now               func() time.Time
}

func NewPRNotificationService(credentials notificationAccountProvider, emit func(PRNotificationEvent), requestPermission func() error) *PRNotificationService {
	directory, err := os.UserConfigDir()
	s := &PRNotificationService{
		credentials: credentials, emit: emit, requestPermission: requestPermission,
		preferences: map[string]NotificationPreferences{}, observed: map[string]map[int]observedPullRequest{},
		accounts: map[string]notificationAccount{}, now: time.Now, configError: err,
	}
	if err == nil {
		s.configPath = filepath.Join(directory, "mag-ik-een-review", "notifications.json")
	}
	return s
}

func defaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{ToastsEnabled: true, Opened: true, Closed: true, Merged: true, ReviewRequested: true}
}

func notificationRepositoryKey(input string) (string, error) {
	parts := strings.Split(strings.TrimSpace(input), "/")
	if len(parts) != 3 {
		return "", errors.New("Choose a GitHub repository using host/owner/repository")
	}
	host, err := githubclient.NormalizeHost(parts[0])
	if err != nil {
		return "", err
	}
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "" || strings.ContainsAny(part, "\\\x00\r\n\t ") || part == "." || part == ".." {
			return "", errors.New("Invalid repository identity")
		}
	}
	return host + "/" + strings.ToLower(parts[1]) + "/" + strings.ToLower(parts[2]), nil
}

func (s *PRNotificationService) GetPreferences(key string) (NotificationPreferences, error) {
	key, err := notificationRepositoryKey(key)
	if err != nil {
		return NotificationPreferences{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return NotificationPreferences{}, err
	}
	return s.preferencesLocked(key), nil
}

func (s *PRNotificationService) RequestDesktopPermission() error {
	s.permissionMu.Lock()
	defer s.permissionMu.Unlock()
	if s.permissionGranted {
		return nil
	}
	if s.requestPermission == nil {
		return errors.New("Desktop notifications are unavailable in this app window")
	}
	if err := s.requestPermission(); err != nil {
		return err
	}
	s.permissionGranted = true
	return nil
}

func (s *PRNotificationService) SavePreferences(key string, prefs NotificationPreferences) error {
	key, err := notificationRepositoryKey(key)
	if err != nil {
		return err
	}
	// The backend also enforces permission so direct binding calls cannot bypass it.
	if prefs.DesktopEnabled {
		if err := s.RequestDesktopPermission(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	next := make(map[string]NotificationPreferences, len(s.preferences)+1)
	for identity, preferences := range s.preferences {
		next[identity] = preferences
	}
	next[key] = prefs
	if err := s.persistLocked(next); err != nil {
		return fmt.Errorf("Notification preferences could not be saved: %w", err)
	}
	s.preferences = next
	return nil
}

func (s *PRNotificationService) preferencesLocked(key string) NotificationPreferences {
	if prefs, ok := s.preferences[key]; ok {
		return prefs
	}
	return defaultNotificationPreferences()
}

func (s *PRNotificationService) loadLocked() error {
	if s.loaded {
		return nil
	}
	if s.configError != nil || s.configPath == "" {
		return errors.New("The operating system configuration directory is unavailable")
	}
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("Notification preferences could not be read: %w", err)
	}
	var wire struct {
		Version      int                        `json:"version"`
		Repositories map[string]json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("Notification preferences could not be read: %w", err)
	}
	if wire.Version > 1 {
		return errors.New("Notification preferences were saved by a newer app version")
	}
	next := map[string]NotificationPreferences{}
	for input, raw := range wire.Repositories {
		key, err := notificationRepositoryKey(input)
		if err != nil {
			return err
		}
		prefs := defaultNotificationPreferences()
		if err := json.Unmarshal(raw, &prefs); err != nil {
			return fmt.Errorf("Invalid notification preferences for %s: %w", key, err)
		}
		next[key] = prefs
	}
	s.preferences = next
	s.loaded = true
	return nil
}

func (s *PRNotificationService) persistLocked(preferences map[string]NotificationPreferences) error {
	data, err := json.MarshalIndent(struct {
		Version      int                                `json:"version"`
		Repositories map[string]NotificationPreferences `json:"repositories"`
	}{1, preferences}, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(s.configPath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".notifications-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), s.configPath)
}

func (s *PRNotificationService) viewer(ctx context.Context, host string) string {
	s.mu.Lock()
	account, cached := s.accounts[host]
	now := s.now()
	s.mu.Unlock()
	if cached && now.Before(account.expiresAt) {
		return account.login
	}
	if s.credentials == nil || ctx.Err() != nil {
		return ""
	}
	status, err := s.credentials.AccountStatus(ctx, host)
	login, duration := "", 15*time.Second
	if err == nil && status.Authenticated && status.Login != "" {
		login, duration = strings.ToLower(status.Login), time.Minute
	}
	if ctx.Err() == nil {
		s.mu.Lock()
		s.accounts[host] = notificationAccount{login: login, expiresAt: now.Add(duration)}
		s.mu.Unlock()
	}
	return login
}

// Observe must only receive a successful complete provider snapshot. Missing PRs
// remain in the baseline; list failures or partial/empty lists never imply closes.
func (s *PRNotificationService) Observe(ctx context.Context, host, owner, repo string, prs []githubclient.PullRequestSummary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := notificationRepositoryKey(host + "/" + owner + "/" + repo)
	if err != nil {
		return err
	}
	parts := strings.Split(key, "/")
	// The first import cannot emit events, so it needs no extra account API call.
	s.mu.Lock()
	_, hasBaseline := s.observed[key]
	s.mu.Unlock()
	viewer := ""
	if hasBaseline {
		viewer = s.viewer(ctx, parts[0])
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if err := s.loadLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	prefs := s.preferencesLocked(key)
	previous, baseline := s.observed[key]
	if !baseline {
		previous = map[int]observedPullRequest{}
		s.observed[key] = previous
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	events := []PRNotificationEvent{}
	for _, pr := range prs {
		if pr.Number <= 0 || (pr.Status != "open" && pr.Status != "draft" && pr.Status != "closed" && pr.Status != "merged") {
			continue
		}
		old, known := previous[pr.Number]
		updatedAt, _ := time.Parse(time.RFC3339Nano, pr.UpdatedAt)
		// Different local clones can poll the same remote concurrently. A slower
		// response must not undo a newer baseline and emit a transition twice.
		if known && !old.updatedAt.IsZero() && !updatedAt.IsZero() && updatedAt.Before(old.updatedAt) {
			continue
		}
		next := observedPullRequest{status: pr.Status, requested: map[string]bool{}, updatedAt: updatedAt}
		for _, user := range pr.Reviewers {
			next.requested[strings.ToLower(user.Login)] = true
		}
		appendEvent := func(kind string, enabled bool) {
			if !baseline || !enabled || (!prefs.ToastsEnabled && !prefs.DesktopEnabled) {
				return
			}
			fingerprint := fmt.Sprintf("%s:%d:%s:%s:%s", key, pr.Number, kind, pr.UpdatedAt, now)
			hash := sha256.Sum256([]byte(fingerprint))
			actor := ""
			if kind == "opened" && !known {
				actor = pr.Author.Login
			}
			events = append(events, PRNotificationEvent{
				ID: hex.EncodeToString(hash[:12]), Key: key, Host: parts[0], Owner: parts[1], Repo: parts[2],
				Number: pr.Number, Type: kind, Title: pr.Title, URL: pr.URL, Actor: actor,
				Toast: prefs.ToastsEnabled, Desktop: prefs.DesktopEnabled, CreatedAt: now,
			})
		}
		if pr.Status == "open" || pr.Status == "draft" {
			if !known || (old.status != "open" && old.status != "draft") {
				appendEvent("opened", prefs.Opened)
			}
			if viewer != "" && next.requested[viewer] && !old.requested[viewer] {
				appendEvent("reviewRequested", prefs.ReviewRequested)
			}
		} else if known && old.status != pr.Status {
			if pr.Status == "merged" {
				appendEvent("merged", prefs.Merged)
			} else {
				appendEvent("closed", prefs.Closed)
			}
		}
		previous[pr.Number] = next
	}
	s.mu.Unlock()
	for _, event := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.emit != nil {
			s.emit(event)
		}
	}
	return nil
}
