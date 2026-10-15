package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// WorkspaceService owns native dialogs and non-secret application preferences.
type WorkspaceService struct {
	app        *application.App
	mu         sync.Mutex
	configPath string
}
type workspacePreferences struct {
	Recent []string `json:"recentRepositories"`
}

func NewWorkspaceService() *WorkspaceService {
	directory, err := os.UserConfigDir()
	if err != nil {
		directory = os.TempDir()
	}
	return &WorkspaceService{configPath: filepath.Join(directory, "mag-ik-een-review", "workspace.json")}
}
func (s *WorkspaceService) ChooseRepository() (string, error) {
	if s.app == nil {
		return "", errors.New("Native repository picker is unavailable")
	}
	return s.app.Dialog.OpenFile().SetTitle("Open a Git repository").CanChooseDirectories(true).CanChooseFiles(false).PromptForSingleSelection()
}
func (s *WorkspaceService) ChoosePatch() (string, error) {
	if s.app == nil {
		return "", errors.New("The native file picker is unavailable")
	}
	path, err := s.app.Dialog.OpenFile().SetTitle("Import a Git patch").CanChooseFiles(true).CanChooseDirectories(false).AddFilter("Git patches", "*.patch;*.diff;*.mbox").PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil {
		return "", err
	}
	if len(data) > 8*1024*1024 {
		return "", errors.New("Choose a patch of at most 8 MiB")
	}
	return string(data), nil
}
func (s *WorkspaceService) SavePatch(content, name string) (string, error) {
	if s.app == nil {
		return "", errors.New("The native file picker is unavailable")
	}
	if len(content) > 8*1024*1024 {
		return "", errors.New("The patch exceeds 8 MiB")
	}
	path, err := s.app.Dialog.SaveFile().SetMessage("Export Git patch").SetFilename(filepath.Base(name)).AddFilter("Git patches", "*.patch;*.diff;*.mbox").PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", err
	}
	return path, nil
}
func (s *WorkspaceService) RecentRepositories() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefs, err := s.read()
	if err != nil {
		return nil, err
	}
	return prefs.Recent, nil
}
func (s *WorkspaceService) RememberRepository(path string) error {
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("Choose a repository directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prefs, err := s.read()
	if err != nil {
		return err
	}
	recent := []string{root}
	for _, old := range prefs.Recent {
		if old != root && len(recent) < 12 {
			recent = append(recent, old)
		}
	}
	if err = os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(workspacePreferences{Recent: recent}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.configPath), ".workspace-*.json")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.configPath)
}
func (s *WorkspaceService) read() (workspacePreferences, error) {
	prefs := workspacePreferences{Recent: []string{}}
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	if err = json.Unmarshal(data, &prefs); err != nil {
		return workspacePreferences{Recent: []string{}}, nil
	}
	if prefs.Recent == nil {
		prefs.Recent = []string{}
	}
	return prefs, nil
}
