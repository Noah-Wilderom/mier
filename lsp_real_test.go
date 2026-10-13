package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/lspclient"
)

// Opt-in real-server smoke tests keep tooling installation outside user projects.
// Set MIER_LSP_SMOKE_TOOLS to a directory with the official npm packages installed.
func realLSPFixture(t *testing.T) (*lspclient.Manager, string, string) {
	t.Helper()
	tools := os.Getenv("MIER_LSP_SMOKE_TOOLS")
	if tools == "" {
		t.Skip("Set MIER_LSP_SMOKE_TOOLS for real TypeScript/Vue server tests")
	}
	root := t.TempDir()
	files := map[string]string{
		"package.json":    `{"name":"mier-lsp-smoke","private":true}`,
		"tsconfig.json":   `{"compilerOptions":{"strict":true,"target":"ES2022","module":"ESNext","moduleResolution":"Bundler"},"include":["src/**/*"]}`,
		"src/lib.ts":      "export function greet(name: string): string { return `Hello ${name}` }\n",
		"src/main.ts":     "import { greet } from './lib'\nexport const greeting = greet('developer')\nexport const letter = greeting.charAt(0)\n",
		"src/Demo.vue":    "<script setup lang=\"ts\">\nimport { greet } from './lib'\nconst greeting = greet('Vue')\n</script>\n<template>{{ greeting }}</template>\n",
		"composer.json":   `{"name":"mier/lsp-smoke","autoload":{"psr-4":{"Smoke\\":"src/"}}}`,
		"src/Greeter.php": "<?php\nnamespace Smoke;\nclass Greeter {\n    public function greet(string $name): string { return 'Hello ' . $name; }\n}\n",
		"src/use.php":     "<?php\nuse Smoke\\Greeter;\n$greeter = new Greeter();\n$message = $greeter->greet('PHP');\n",
	}
	for path, text := range files {
		destination := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("config", "user.name", "M.I.E.R. test")
	git("config", "user.email", "test@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("add", ".")
	git("commit", "--quiet", "-m", "LSP smoke fixture")
	sha := git("rev-parse", "HEAD")
	client, err := gitclient.New(gitclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := lspclient.New(lspclient.Options{ConfigPath: filepath.Join(t.TempDir(), "servers.json"), CacheDirectory: t.TempDir(), Archive: func(ctx context.Context, request lspclient.CodePositionRequest) (lspclient.ArchiveResult, error) {
		var archive gitclient.CommitArchiveResult
		var err error
		if request.Revision == "working" || request.Revision == "index" {
			archive, err = client.ArchiveWorkspace(ctx, request.Root, request.Revision)
		} else {
			archive, err = client.ArchiveCommit(ctx, request.Root, gitclient.CommitArchiveRequest{Revision: request.Revision, ComparisonHead: request.ComparisonHead})
		}
		return lspclient.ArchiveResult{Revision: archive.Revision, Tar: archive.Data}, err
	}})
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	sdk := filepath.Join(tools, "node_modules", "typescript", "lib")
	plugin := filepath.Join(tools, "node_modules", "@vue", "typescript-plugin")
	for language, script := range map[string]string{"typescript": filepath.Join(tools, "node_modules", "typescript-language-server", "lib", "cli.mjs"), "vue": filepath.Join(tools, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js")} {
		if err := m.Configure(context.Background(), language, lspclient.ServerConfiguration{Command: "node", Args: []string{script, "--stdio"}, TypeScriptSDKPath: sdk, VuePluginPath: plugin}); err != nil {
			t.Fatal(err)
		}
	}
	return m, root, sha
}

func TestRealPHPHoverAndDefinition(t *testing.T) {
	m, root, sha := realLSPFixture(t)
	phar := filepath.Join(os.Getenv("MIER_LSP_SMOKE_TOOLS"), "phpactor.phar")
	if _, err := os.Stat(phar); err != nil {
		t.Skip("Download the official Phpactor PHAR into MIER_LSP_SMOKE_TOOLS for PHP smoke testing")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := m.Configure(context.Background(), "php", lspclient.ServerConfiguration{Command: "php", Args: []string{phar, "language-server"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	line := "$message = $greeter->greet('PHP');"
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/use.php", Line: 3, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	hover, err := m.Hover(ctx, request)
	if err != nil || !strings.Contains(hover.Contents, "greet") || !strings.Contains(hover.Contents, "string") {
		t.Fatalf("PHP signature unavailable: %+v %v", hover, err)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil || len(targets) != 1 || targets[0].Path != "src/Greeter.php" {
		t.Fatalf("PHP definition unavailable: %+v %v", targets, err)
	}
	doc, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil || !strings.Contains(doc.Text, "public function greet") {
		t.Fatalf("PHP source unavailable: %+v %v", doc, err)
	}
}

func TestRealTypeScriptWorkingAndStagedSource(t *testing.T) {
	m, root, _ := realLSPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	line := "export const greeting = greet('developer')"
	request := lspclient.CodePositionRequest{Root: root, Revision: "index", Path: "src/main.ts", Line: 1, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	// The index and working tree intentionally disagree about a cross-file signature.
	if err := os.WriteFile(filepath.Join(root, "src/lib.ts"), []byte("export function greet(name: string): number { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "src/lib.ts")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stage fixture: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "src/lib.ts"), []byte("export function greet(name: string): boolean { return true }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	staged, err := m.Hover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(staged.Contents, "number") || strings.Contains(staged.Contents, "boolean") {
		t.Fatalf("index signature unavailable: %+v", staged)
	}
	request.Revision = "working"
	working, err := m.Hover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(working.Contents, "boolean") || working.Revision == staged.Revision {
		t.Fatalf("working signature unavailable: %+v", working)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil || len(targets) != 1 {
		t.Fatalf("working definition: %+v %v", targets, err)
	}
	doc, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil || !strings.Contains(doc.Text, "boolean") {
		t.Fatalf("working definition content: %+v %v", doc, err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/lib.ts"), []byte("export function greet(name: string): string[] { return [name] }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := m.Hover(ctx, request)
	if err != nil || updated.Revision == working.Revision || !strings.Contains(updated.Contents, "string[]") {
		t.Fatalf("changed working tree was not recaptured: %+v %v", updated, err)
	}
}

func TestRealTypeScriptRevisionHoverAndDependencyDefinition(t *testing.T) {
	m, root, sha := realLSPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	line := "export const greeting = greet('developer')"
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/main.ts", Line: 1, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	// Source changes on disk must not change a committed PR review's analysis.
	if err := os.WriteFile(filepath.Join(root, "src/lib.ts"), []byte("export function greet(name: string): number { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hover, err := m.Hover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents, "greet") || !strings.Contains(hover.Contents, "string") || hover.Revision != sha {
		t.Fatalf("unexpected hover: %+v", hover)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Path != "src/lib.ts" || targets[0].External {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	doc, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text, ": string") || strings.Contains(doc.Text, "return 42") || doc.URI != targets[0].URI {
		t.Fatalf("revision source changed: %+v", doc)
	}
	line = "export const letter = greeting.charAt(0)"
	request.Line = 2
	request.Character = strings.Index(line, "charAt") + 2
	request.LineText = &line
	targets, err = m.Definition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) == 0 || !targets[0].External {
		t.Fatalf("SDK definition unavailable: %+v", targets)
	}
	doc, err = m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text, "charAt") || !doc.External {
		t.Fatalf("SDK source unavailable: %+v", doc)
	}
}

func TestRealVueHoverAndTypeScriptDefinition(t *testing.T) {
	m, root, sha := realLSPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	line := "const greeting = greet('Vue')"
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/Demo.vue", Line: 2, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	hover, err := m.Hover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents, "greet") || !strings.Contains(hover.Contents, "string") {
		t.Fatalf("Vue signature unavailable: %+v", hover)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Path != "src/lib.ts" {
		t.Fatalf("Vue definition unavailable: %+v", targets)
	}
	doc, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text, "export function greet") {
		t.Fatalf("definition text unavailable: %+v", doc)
	}
}
