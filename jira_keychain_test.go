package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// Match the limits in go-keyring v0.2.8 without needing a user's keychain.
type platformLimitedJiraStore struct {
	secretStore
	platform string
}

func (s platformLimitedJiraStore) Set(service, user, value string) error {
	if s.platform == "windows" && len(value) > 2560 {
		return keyring.ErrSetDataTooBig
	}
	if s.platform == "darwin" {
		encoded := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(value))
		command := "add-generic-password -U -s " + service + " -a " + user + " -w " + encoded + "\n"
		if len(command) > 4096 {
			return keyring.ErrSetDataTooBig
		}
	}
	return s.secretStore.Set(service, user, value)
}

func TestJiraLargeOAuthCredentialsFitPlatformKeychains(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			h := newJiraHarness(t)
			_ = h.service.ServiceShutdown()
			limited := platformLimitedJiraStore{secretStore: h.store, platform: platform}
			h.service = newJiraIntegration(h.service.configPath, limited, h.service.client)
			access := strings.Repeat("access-", 600)
			refresh := strings.Repeat("refresh-", 500)
			generation := 0
			h.handler = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != "auth.atlassian.com" || r.URL.Path != "/oauth/token" {
					return false
				}
				var request map[string]string
				_ = json.NewDecoder(r.Body).Decode(&request)
				if request["grant_type"] == "refresh_token" {
					if request["refresh_token"] != refresh {
						t.Error("refresh did not use the saved large token")
					}
					generation++
					access += "-rotated"
					refresh += "-rotated"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600, "scope": "read:jira-work offline_access"})
				return true
			}
			h.authorize(t)
			reopened := newJiraIntegration(h.service.configPath, limited, h.service.client)
			defer reopened.ServiceShutdown()
			status, err := reopened.GetJiraStatus(context.Background())
			if err != nil || !status.Connected || status.Error != "" || reopened.secrets.AccessToken != access || reopened.secrets.RefreshToken != refresh {
				t.Fatal("large Jira credentials did not survive restart")
			}
			reopened.secrets.ExpiresAt = time.Now().Add(-time.Minute)
			if value, err := reopened.jiraAccessToken(context.Background(), ""); err != nil || value != access || generation != 1 {
				t.Fatal("large credentials could not be refreshed")
			}
			rotated := newJiraIntegration(h.service.configPath, limited, h.service.client)
			defer rotated.ServiceShutdown()
			status, err = rotated.GetJiraStatus(context.Background())
			if err != nil || !status.Connected || rotated.secrets.RefreshToken != refresh {
				t.Fatal("rotated large token did not survive restart")
			}
			if err := rotated.DisconnectJira(context.Background()); err != nil {
				t.Fatal("large credentials could not be disconnected")
			}
			h.store.mu.Lock()
			defer h.store.mu.Unlock()
			if len(h.store.values) != 1 || strings.Contains(h.store.values[jiraKeyringService+":fixture-client"], access) {
				t.Fatal("disconnect retained account token entries")
			}
		})
	}
}

type failingJiraStore struct {
	secretStore
	failSet    int
	sets       int
	failDelete bool
}

func (s *failingJiraStore) Set(service, user, value string) error {
	s.sets++
	if s.sets == s.failSet {
		return errors.New("fixture keychain access denied")
	}
	return s.secretStore.Set(service, user, value)
}

func (s *failingJiraStore) Delete(service, user string) error {
	if s.failDelete {
		return errors.New("fixture keychain cleanup denied")
	}
	return s.secretStore.Delete(service, user)
}

func TestJiraKeychainFailedSavePreservesPreviousGeneration(t *testing.T) {
	for _, old := range []string{"old-client-secret", strings.Repeat("old-token-", 400)} {
		for _, failure := range []int{1, 3, 5} {
			t.Run(fmt.Sprintf("write-%d/old-size-%d", failure, len(old)), func(t *testing.T) {
				memory := &memorySecrets{values: map[string]string{}}
				base := &failingJiraStore{secretStore: platformLimitedJiraStore{secretStore: memory, platform: "darwin"}}
				store := &jiraKeychainStore{base: base}
				if err := store.Set(jiraKeyringService, "client", old); err != nil {
					t.Fatal("could not prepare old credentials")
				}
				before := len(memory.values)
				base.sets = 0
				base.failSet = failure
				// Four parts followed by the publication of the root manifest.
				if err := store.Set(jiraKeyringService, "client", strings.Repeat("new-token-", 500)); err == nil {
					t.Fatal("interrupted save reported success")
				}
				reopened := &jiraKeychainStore{base: base}
				value, err := reopened.Get(jiraKeyringService, "client")
				if err != nil || value != old || len(memory.values) != before {
					t.Fatal("failed save damaged old credentials or retained staged parts")
				}
			})
		}
	}
}

func TestJiraKeychainLegacyMigrationBoundsAndCleanup(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			memory := &memorySecrets{values: map[string]string{}}
			base := platformLimitedJiraStore{secretStore: memory, platform: platform}
			store := &jiraKeychainStore{base: base}
			user := strings.Repeat("a", 256)
			legacy := `{"clientSecret":"legacy-secret"}`
			_ = base.Set(jiraKeyringService, user, legacy)
			if got, err := store.Get(jiraKeyringService, user); err != nil || got != legacy {
				t.Fatal("existing single-item credentials were not readable")
			}
			for _, value := range []string{strings.Repeat("x", jiraDirectSecretBytes), "a" + strings.Repeat("€", 1100), strings.Repeat("x", maxJiraSecretsBytes)} {
				if err := store.Set(jiraKeyringService, user, value); err != nil {
					t.Fatal("bounded credentials exceeded a platform's entry limit")
				}
				reopened := &jiraKeychainStore{base: base}
				if got, err := reopened.Get(jiraKeyringService, user); err != nil || got != value {
					t.Fatal("credential bytes changed across restart")
				}
			}
			before := len(memory.values)
			if err := store.Set(jiraKeyringService, user, strings.Repeat("x", maxJiraSecretsBytes+1)); !errors.Is(err, keyring.ErrSetDataTooBig) || len(memory.values) != before {
				t.Fatal("oversized credentials were partially saved")
			}
			if err := store.Set(jiraKeyringService, user, legacy); err != nil || len(memory.values) != 1 {
				t.Fatal("small replacement retained obsolete token parts")
			}
			if err := store.Set(jiraKeyringService, user, strings.Repeat("large-token", 500)); err != nil {
				t.Fatal("could not prepare large credentials")
			}
			if err := store.Delete(jiraKeyringService, user); err != nil || len(memory.values) != 0 {
				t.Fatal("account removal did not clean up token parts")
			}
		})
	}
}

func TestJiraKeychainCleanupFailureRetainsPublishedCredentials(t *testing.T) {
	memory := &memorySecrets{values: map[string]string{}}
	base := &failingJiraStore{secretStore: memory}
	store := &jiraKeychainStore{base: base}
	if err := store.Set(jiraKeyringService, "client", strings.Repeat("old", 1500)); err != nil {
		t.Fatal("could not prepare old credentials")
	}
	base.failDelete = true
	value := strings.Repeat("new", 1500)
	if err := store.Set(jiraKeyringService, "client", value); err != nil {
		t.Fatal("successful publication was reported as a failed token rotation")
	}
	reopened := &jiraKeychainStore{base: base}
	if got, err := reopened.Get(jiraKeyringService, "client"); err != nil || got != value {
		t.Fatal("cleanup failure made the old rotating token current")
	}
}

func TestJiraKeychainCorruptionIsBoundedAndRecoverable(t *testing.T) {
	for _, corruption := range []string{"missing-part", "invalid-encoding", "wrong-digest", "huge-parts", "invalid-generation", "unknown-version"} {
		t.Run(corruption, func(t *testing.T) {
			memory := &memorySecrets{values: map[string]string{}}
			store := &jiraKeychainStore{base: memory}
			if err := store.Set(jiraKeyringService, "client", strings.Repeat("secret", 1000)); err != nil {
				t.Fatal("could not prepare large credentials")
			}
			root := jiraKeyringService + ":client"
			m, _ := decodeJiraManifest(memory.values[root])
			part := jiraKeyringService + ".parts:" + jiraChunkUser("client", m, 0)
			switch corruption {
			case "missing-part":
				delete(memory.values, part)
			case "invalid-encoding":
				memory.values[part] = "!not-base64!"
			case "wrong-digest":
				m.Digest = strings.Repeat("0", 64)
			case "huge-parts":
				m.Parts = 1 << 30
			case "invalid-generation":
				m.Generation = "other-client:unsafe:part"
			case "unknown-version":
				m.Version = 2
			}
			manifest, _ := json.Marshal(m)
			memory.values[root] = jiraManifestPrefix + string(manifest)
			if value, err := store.Get(jiraKeyringService, "client"); value != "" || !errors.Is(err, errJiraKeychainCorrupt) || errors.Is(err, keyring.ErrNotFound) {
				t.Fatal("corrupt credentials were returned or treated as a missing account")
			}
			if err := store.Set(jiraKeyringService, "client", "replacement-client-secret"); err != nil {
				t.Fatal("corrupt credentials could not be replaced")
			}
			if value, err := store.Get(jiraKeyringService, "client"); err != nil || value != "replacement-client-secret" {
				t.Fatal("replacement credentials could not be read")
			}
		})
	}
}

func TestJiraKeychainSizeErrorsDoNotReportLockedKeychain(t *testing.T) {
	h := newJiraHarness(t)
	h.configure(t)
	h.service.mu.Lock()
	err := h.service.saveSecretsLocked(jiraSecrets{ClientSecret: "fixture-client-secret", AccessToken: strings.Repeat("private-token", maxJiraSecretsBytes)})
	h.service.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "size limit") || strings.Contains(strings.ToLower(err.Error()), "unlock") || strings.Contains(err.Error(), "private-token") {
		t.Fatal("storage size failure was hidden or exposed private data")
	}
}

// CI explicitly opts in on macOS/Windows. Never inspect existing keychain items;
// use a random test account and remove it when the check finishes.
func TestRealJiraKeychainLargeRecord(t *testing.T) {
	if os.Getenv("MIER_JIRA_KEYCHAIN_SMOKE") != "1" || runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("native keychain smoke test is enabled only on macOS/Windows CI")
	}
	store := &jiraKeychainStore{base: systemSecretStore{}}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("could not prepare a private native keychain test")
	}
	user := "smoke-" + hex.EncodeToString(nonce)
	service := jiraKeyringService + ".test"
	t.Cleanup(func() {
		if err := store.Delete(service, user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			t.Error("temporary native keychain entries could not be removed")
		}
	})
	legacy := `{"clientSecret":"synthetic-test-secret"}`
	if err := store.Set(service, user, legacy); err != nil {
		t.Fatal("native keychain could not save test app credentials")
	}
	large := strings.Repeat("synthetic-oauth-data-", 500)
	if err := store.Set(service, user, large); err != nil {
		t.Fatal("native keychain could not save large test credentials")
	}
	if value, err := store.Get(service, user); err != nil || value != large {
		t.Fatal("native keychain could not reload large test credentials")
	}
	if err := store.Set(service, user, legacy); err != nil {
		t.Fatal("native keychain could not clear test account tokens")
	}
}
