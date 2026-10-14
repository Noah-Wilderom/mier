package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	maxJiraSecretsBytes   = 64 * 1024
	jiraDirectSecretBytes = 2000
	// Base64 yields 2048 bytes, below Windows' 2560-byte limit. go-keyring's
	// additional macOS encoding plus service/account fields fit its 4096-byte
	// command limit even with the longest supported client ID.
	jiraSecretChunkBytes = 1536
	jiraManifestPrefix   = "mier-jira-keychain-v1:"
)

var errJiraKeychainCorrupt = errors.New("incomplete Jira keychain record")

type jiraKeychainManifest struct {
	Version    int    `json:"version"`
	Generation string `json:"generation"`
	Parts      int    `json:"parts"`
	Bytes      int    `json:"bytes"`
	Digest     string `json:"sha256"`
}

// A small root item publishes one complete generation of larger credentials.
// Parts are written first so a failed save cannot damage the current tokens.
// All data, including the manifest, remains in the OS credential store.
type jiraKeychainStore struct {
	mu   sync.Mutex
	base secretStore
}

func decodeJiraManifest(value string) (*jiraKeychainManifest, error) {
	if !strings.HasPrefix(value, jiraManifestPrefix) {
		return nil, nil // Existing single-item JSON records remain readable.
	}
	var m jiraKeychainManifest
	if len(value) > jiraDirectSecretBytes || json.Unmarshal([]byte(strings.TrimPrefix(value, jiraManifestPrefix)), &m) != nil || m.Version != 1 || m.Bytes <= jiraDirectSecretBytes || m.Bytes > maxJiraSecretsBytes || m.Parts != (m.Bytes+jiraSecretChunkBytes-1)/jiraSecretChunkBytes || !jiraHexValue(m.Generation, 16) || !jiraHexValue(m.Digest, sha256.Size) {
		return nil, errJiraKeychainCorrupt
	}
	return &m, nil
}

func jiraHexValue(value string, size int) bool {
	if len(value) != size*2 || strings.ToLower(value) != value {
		return false
	}
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == size
}

func jiraChunkUser(user string, m *jiraKeychainManifest, part int) string {
	return user + ":" + m.Generation + ":" + strconv.Itoa(part)
}

func (s *jiraKeychainStore) Get(service, user string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.base.Get(service, user)
	if err != nil {
		return "", err
	}
	if len(value) > maxJiraSecretsBytes {
		return "", errJiraKeychainCorrupt
	}
	m, err := decodeJiraManifest(value)
	if err != nil {
		return "", err
	}
	if m == nil {
		return value, nil
	}
	data := make([]byte, 0, m.Bytes)
	for part := 0; part < m.Parts; part++ {
		encoded, err := s.base.Get(service+".parts", jiraChunkUser(user, m, part))
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errJiraKeychainCorrupt
		}
		if err != nil {
			return "", err
		}
		if len(encoded) > base64.StdEncoding.EncodedLen(jiraSecretChunkBytes) {
			return "", errJiraKeychainCorrupt
		}
		chunk, err := base64.StdEncoding.Strict().DecodeString(encoded)
		expected := min(jiraSecretChunkBytes, m.Bytes-len(data))
		if err != nil || len(chunk) != expected {
			return "", errJiraKeychainCorrupt
		}
		data = append(data, chunk...)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != m.Digest {
		return "", errJiraKeychainCorrupt
	}
	return string(data), nil
}

func (s *jiraKeychainStore) Set(service, user, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(value) > maxJiraSecretsBytes {
		return keyring.ErrSetDataTooBig
	}
	previous, err := s.base.Get(service, user)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	// A malformed old manifest can be replaced when the user supplies fresh
	// credentials, but cannot name arbitrary entries for cleanup.
	old, _ := decodeJiraManifest(previous)
	if len(value) <= jiraDirectSecretBytes {
		if err := s.base.Set(service, user, value); err != nil {
			return err
		}
		_ = s.cleanup(service, user, old)
		return nil
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return errors.New("secure credential storage could not be prepared")
	}
	digest := sha256.Sum256([]byte(value))
	m := &jiraKeychainManifest{Version: 1, Generation: hex.EncodeToString(nonce), Parts: (len(value) + jiraSecretChunkBytes - 1) / jiraSecretChunkBytes, Bytes: len(value), Digest: hex.EncodeToString(digest[:])}
	for part := 0; part < m.Parts; part++ {
		start := part * jiraSecretChunkBytes
		encoded := base64.StdEncoding.EncodeToString([]byte(value[start:min(start+jiraSecretChunkBytes, len(value))]))
		if err := s.base.Set(service+".parts", jiraChunkUser(user, m, part), encoded); err != nil {
			_ = s.cleanup(service, user, m)
			return err
		}
	}
	manifest, err := json.Marshal(m)
	if err == nil {
		err = s.base.Set(service, user, jiraManifestPrefix+string(manifest))
	}
	if err != nil {
		_ = s.cleanup(service, user, m)
		return err
	}
	// Publication succeeded. Cleanup failures must not report that the old
	// rotating token is still current; the next read uses the new generation.
	_ = s.cleanup(service, user, old)
	return nil
}

func (s *jiraKeychainStore) Delete(service, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.base.Get(service, user)
	if err != nil {
		return err
	}
	m, _ := decodeJiraManifest(previous)
	if err := s.base.Delete(service, user); err != nil {
		return err
	}
	return s.cleanup(service, user, m)
}

func (s *jiraKeychainStore) cleanup(service, user string, m *jiraKeychainManifest) error {
	if m == nil {
		return nil
	}
	var first error
	for part := 0; part < m.Parts; part++ {
		if err := s.base.Delete(service+".parts", jiraChunkUser(user, m, part)); err != nil && !errors.Is(err, keyring.ErrNotFound) && first == nil {
			first = err
		}
	}
	return first
}
