package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const workspaceStateLimit = 4 * 1024 * 1024

func (s *WorkspaceService) statePath(scope string) (string, error) {
	if scope == "" || len(scope) > 8192 || strings.ContainsRune(scope, 0) {
		return "", errors.New("Invalid workspace state scope")
	}
	digest := sha256.Sum256([]byte(scope))
	return filepath.Join(filepath.Dir(s.configPath), "sessions", hex.EncodeToString(digest[:])+".json"), nil
}

// LoadState restores private, local UI data. Credentials and live execution
// capabilities belong to their own services and must never be stored here.
func (s *WorkspaceService) LoadState(scope string) (string, error) {
	path, err := s.statePath(scope)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, workspaceStateLimit+2))
	if err != nil {
		return "", err
	}
	data = bytes.TrimSpace(data)
	if len(data) > workspaceStateLimit || !json.Valid(data) {
		return "", errors.New("Saved workspace state is damaged or too large")
	}
	return string(data), nil
}

// SaveState uses a private atomic file so interruption cannot truncate a draft.
// Empty data removes a record. Old records are bounded by count and disk usage.
func (s *WorkspaceService) SaveState(scope, data string) error {
	path, err := s.statePath(scope)
	if err != nil {
		return err
	}
	if len(data) > workspaceStateLimit || (data != "" && !json.Valid([]byte(data))) {
		return errors.New("Workspace state must be valid JSON of at most 4 MiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if data == "" {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err = writePrivateJSON(path, json.RawMessage(data)); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	type record struct {
		path string
		info os.FileInfo
	}
	var records []record
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			continue
		}
		total += info.Size()
		records = append(records, record{filepath.Join(filepath.Dir(path), entry.Name()), info})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].info.ModTime().Before(records[j].info.ModTime()) })
	count := len(records)
	for _, record := range records {
		if count <= 256 && total <= 64*1024*1024 {
			break
		}
		if record.path == path {
			continue
		}
		if os.Remove(record.path) == nil {
			total -= record.info.Size()
			count--
		}
	}
	return nil
}
