package lspclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in test exercises the same downloads, integrity checks, npm command,
// version verification, and activation used by the desktop Install button.
func TestRealManagedToolInstallation(t *testing.T) {
	if os.Getenv("MIER_LSP_INSTALL_SMOKE") != "1" {
		t.Skip("Set MIER_LSP_INSTALL_SMOKE=1 to install and verify official language tools outside the repository")
	}
	directory := os.Getenv("MIER_LSP_INSTALL_TOOLS")
	if directory == "" {
		directory = t.TempDir()
	}
	settings := t.TempDir()
	m := New(Options{ConfigPath: filepath.Join(settings, "language-servers.json"), CacheDirectory: filepath.Join(settings, "cache"), ToolsDirectory: directory})
	defer m.Close()
	for _, language := range []string{"php", "typescript", "vue"} {
		if err := m.InstallServer(context.Background(), language); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { m.tools.workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Minute):
			t.Fatalf("%s installation timed out", language)
		}
		tool := m.tools.statuses(context.Background())[language]
		server := m.detectServer("", language)
		if tool.InstallError != "" || !tool.Ready || !server.Available || server.Source != "managed" {
			t.Fatalf("%s managed installation is unavailable: %#v / %#v", language, tool, server)
		}
		t.Logf("%s %s installed, verified, and detected at %s", language, tool.InstalledVersion, server.Args[0])
	}
}
