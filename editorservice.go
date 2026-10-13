package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type EditorService struct {
	repositories repositoryReader
	configPath   string
	configErr    error
	mu           sync.Mutex
	lookPath     func(string) (string, error)
	launch       func(string, []string) error
}

func NewEditorService(repositories repositoryReader) *EditorService {
	directory, err := os.UserConfigDir()
	return &EditorService{repositories: repositories, configPath: filepath.Join(directory, "mag-ik-een-review", "editor.json"), configErr: err, lookPath: exec.LookPath, launch: launchEditor}
}

func editorName(editor string) (string, error) {
	switch editor {
	case "vscode":
		return "VS Code", nil
	case "phpstorm":
		return "PhpStorm", nil
	case "zed":
		return "Zed", nil
	default:
		return "", errors.New("Choose VS Code, PhpStorm, or Zed")
	}
}

func (s *EditorService) PreferredEditor() (string, error) {
	if s.configErr != nil {
		return "", s.configErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.configPath)
	if os.IsNotExist(err) {
		return "vscode", nil
	}
	if err != nil {
		return "", err
	}
	var prefs struct {
		Editor string `json:"editor"`
	}
	if json.Unmarshal(data, &prefs) != nil {
		return "vscode", nil
	}
	if _, err := editorName(prefs.Editor); err != nil {
		return "vscode", nil
	}
	return prefs.Editor, nil
}

func (s *EditorService) OpenRepository(ctx context.Context, root, editor string) error {
	if s.configErr != nil {
		return s.configErr
	}
	name, err := editorName(editor)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := s.repositories.Snapshot(ctx, root)
	if err != nil {
		return err
	}
	command, args, err := s.command(editor, snapshot.Root)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.launch(command, args); err != nil {
		return fmt.Errorf("Could not open %s: %w", name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.configPath), ".editor-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(map[string]string{"editor": editor}); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.configPath); err != nil {
		return fmt.Errorf("The editor opened, but its preference could not be saved: %w", err)
	}
	return nil
}

func (s *EditorService) command(editor, root string) (string, []string, error) {
	name, _ := editorName(editor)
	candidates := map[string][]string{"vscode": {"code", "code.exe"}, "phpstorm": {"phpstorm", "phpstorm.sh", "phpstorm64.exe", "pstorm"}, "zed": {"zed", "zeditor", "zed.exe"}}[editor]
	for _, candidate := range candidates {
		if command, err := s.lookPath(candidate); err == nil {
			return command, []string{root}, nil
		}
	}
	if runtime.GOOS == "darwin" {
		appNames := map[string]string{"vscode": "Visual Studio Code", "phpstorm": "PhpStorm", "zed": "Zed"}
		app := appNames[editor]
		home, _ := os.UserHomeDir()
		for _, directory := range []string{"/Applications", filepath.Join(home, "Applications")} {
			if info, err := os.Stat(filepath.Join(directory, app+".app")); err == nil && info.IsDir() {
				if command, err := s.lookPath("open"); err == nil {
					return command, []string{"-a", app, root}, nil
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		var paths []string
		switch editor {
		case "vscode":
			paths = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "Code.exe"), filepath.Join(os.Getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe")}
		case "zed":
			paths = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Zed", "zed.exe"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Zed", "zed.exe")}
		case "phpstorm":
			paths, _ = filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "JetBrains", "PhpStorm*", "bin", "phpstorm64.exe"))
		}
		for _, path := range paths {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, []string{root}, nil
			}
		}
	}
	return "", nil, fmt.Errorf("%s was not found. Install it and enable its command-line launcher", name)
}

func launchEditor(command string, args []string) error {
	// Direct argv keeps repository paths literal. Editors remain open after M.I.E.R. exits.
	cmd := exec.Command(command, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-finished:
		return err
	case <-timer.C:
		return nil
	}
}
