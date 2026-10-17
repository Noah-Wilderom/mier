package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateWorkspaceState(t *testing.T) {
	s := &WorkspaceService{configPath: filepath.Join(t.TempDir(), "workspace.json")}
	scope := "review:/private/repo:account:42"
	if got, err := s.LoadState(scope); err != nil || got != "" {
		t.Fatalf("missing: %q %v", got, err)
	}
	if err := s.SaveState(scope, `{"draft":"unfinished reply"}`); err != nil {
		t.Fatal(err)
	}
	// A new service instance must see the saved data, without raw repo names in paths.
	restarted := &WorkspaceService{configPath: s.configPath}
	got, err := restarted.LoadState(scope)
	if err != nil || !strings.Contains(got, "unfinished reply") {
		t.Fatalf("restore: %q %v", got, err)
	}
	path, _ := s.statePath(scope)
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("draft file is not private")
	}
	if other, _ := s.LoadState(scope + "other"); other != "" {
		t.Fatal("state leaked across scopes")
	}
	for _, invalid := range []string{"not json", strings.Repeat("x", workspaceStateLimit+1)} {
		if s.SaveState(scope, invalid) == nil {
			t.Fatal("invalid state accepted")
		}
	}
	if err := s.SaveState(scope, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadState(scope); got != "" {
		t.Fatal("record was not deleted")
	}
}
