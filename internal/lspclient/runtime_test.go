package lspclient

import (
	"context"
	"mag-ik-een-review/internal/desktopruntime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAndConfiguredNodeServersWorkWithDesktopPATH(t *testing.T) {
	node, err := desktopruntime.FindNode(context.Background(), "", false)
	if err != nil {
		t.Skip("Node is not installed for the real subprocess check")
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NVM_BIN", filepath.Dir(node.Executable))
	root := t.TempDir()
	packageRoot := filepath.Join(root, "node_modules", "yaml-language-server")
	writeTestFile(t, filepath.Join(packageRoot, "package.json"), `{"name":"yaml-language-server","bin":{"yaml-language-server":"bin/server.cjs"}}`)
	script := filepath.Join(packageRoot, "bin", "server.cjs")
	writeTestFile(t, script, `const {spawnSync} = require('node:child_process');
const child = spawnSync('node', ['-p', 'process.execPath'], {encoding: 'utf8'});
if (child.error || child.status !== 0) process.exit(10);
process.stdout.write(child.stdout);`)
	for _, configured := range []bool{false, true} {
		configuration := ServerConfiguration{}
		if configured {
			configuration = ServerConfiguration{Command: "node", Args: []string{script}}
		}
		status := detectServer(root, "yaml", configuration)
		if !status.Available || status.Command != node.Executable || len(status.Environment) == 0 {
			t.Fatalf("desktop runtime was not used (configured=%t): %#v", configured, status)
		}
		command := exec.Command(status.Command, status.Args...)
		command.Dir = root
		command.Env = append(os.Environ(), status.Environment...)
		output, err := command.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != node.Executable {
			t.Fatalf("server could not spawn Node with desktop PATH: %s, %v", output, err)
		}
	}
	if os.Getenv("PATH") == filepath.Dir(node.Executable) {
		t.Fatal("language server detection mutated the app's environment")
	}
}

func TestInstallerChildUsesItsSelectedRuntime(t *testing.T) {
	node, err := desktopruntime.FindNode(context.Background(), "", false)
	if err != nil {
		t.Skip("Node is not installed for the real subprocess check")
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NODE_OPTIONS", "--require=/project/loader-that-must-not-run.js")
	output, err := runToolCommand(context.Background(), t.TempDir(), node.Executable, "-e", `const child = require('node:child_process').spawnSync('node', ['-p', 'process.execPath'], {encoding: 'utf8'}); if (child.error || child.status !== 0) process.exit(10); process.stdout.write(child.stdout);`)
	if err != nil || strings.TrimSpace(string(output)) != node.Executable {
		t.Fatalf("installer child did not inherit its selected Node: %s, %v", output, err)
	}
}
