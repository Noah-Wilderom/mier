package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type editorRepository struct {
	root string
	err  error
}

func (r editorRepository) Snapshot(context.Context, string) (RepositorySnapshot, error) {
	return RepositorySnapshot{Root: r.root}, r.err
}

func editorFixture(t *testing.T) *EditorService {
	t.Helper()
	s := NewEditorService(editorRepository{root: filepath.Join(t.TempDir(), "repository with $(literal) spaces")})
	s.configPath = filepath.Join(t.TempDir(), "editor.json")
	s.lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	s.launch = func(string, []string) error { return nil }
	return s
}

func TestEditorDefaultAndSuccessfulChoicePersist(t *testing.T) {
	s := editorFixture(t)
	if preferred, err := s.PreferredEditor(); err != nil || preferred != "vscode" {
		t.Fatalf("default=%q error=%v", preferred, err)
	}
	var command string
	var arguments []string
	s.launch = func(bin string, args []string) error { command, arguments = bin, args; return nil }
	if err := s.OpenRepository(context.Background(), "requested path", "phpstorm"); err != nil {
		t.Fatal(err)
	}
	if command != "/tools/phpstorm" || !reflect.DeepEqual(arguments, []string{s.repositories.(editorRepository).root}) {
		t.Fatalf("shell-free argv changed: %q %q", command, arguments)
	}
	reopened := editorFixture(t)
	reopened.configPath = s.configPath
	if preferred, err := reopened.PreferredEditor(); err != nil || preferred != "phpstorm" {
		t.Fatalf("saved=%q error=%v", preferred, err)
	}
	info, err := os.Stat(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("preference mode=%v", info.Mode())
	}
}

func TestEditorFailedLaunchDoesNotBecomeLastUsed(t *testing.T) {
	s := editorFixture(t)
	s.launch = func(string, []string) error { return errors.New("launcher failed") }
	if err := s.OpenRepository(context.Background(), "root", "zed"); err == nil {
		t.Fatal("failed launch accepted")
	}
	if preferred, _ := s.PreferredEditor(); preferred != "vscode" {
		t.Fatalf("failed editor became default: %s", preferred)
	}
}

func TestEditorMissingUnsupportedAndCancelledNeverLaunch(t *testing.T) {
	s := editorFixture(t)
	calls := 0
	s.launch = func(string, []string) error { calls++; return nil }
	if err := s.OpenRepository(context.Background(), "root", "arbitrary-command"); err == nil {
		t.Fatal("unknown editor accepted")
	}
	s.lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := s.OpenRepository(context.Background(), "root", "zed"); err == nil {
		t.Fatal("missing editor accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.OpenRepository(ctx, "root", "vscode"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if calls != 0 {
		t.Fatalf("unexpected launches=%d", calls)
	}
}
