package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			archive, err = client.ArchiveCommit(ctx, request.Root, gitclient.CommitArchiveRequest{Revision: request.Revision, ComparisonHead: request.ComparisonHead, Host: request.Host, Owner: request.Owner, Repo: request.Repo, PullRequest: request.PullRequest})
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
	// Phpactor reports partial symbols while indexing. Wait for the actual
	// signature within a bound instead of relying on one machine's startup speed.
	warmup := time.Now().Add(12 * time.Second)
	for err == nil && !strings.Contains(hover.Contents, "string") && time.Now().Before(warmup) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
		hover, err = m.Hover(ctx, request)
	}
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

func TestRealPHPColdAndWarmHoverTiming(t *testing.T) {
	m, root, sha := realLSPFixture(t)
	phar := filepath.Join(os.Getenv("MIER_LSP_SMOKE_TOOLS"), "phpactor.phar")
	if _, err := os.Stat(phar); err != nil {
		t.Skip("Download the official Phpactor PHAR for PHP timing checks")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := m.Configure(context.Background(), "php", lspclient.ServerConfiguration{Command: "php", Args: []string{phar, "language-server"}}); err != nil {
		t.Fatal(err)
	}
	line := "$message = $greeter->greet('PHP');"
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/use.php", Line: 3, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	preparing := time.Now()
	if err := m.Warmup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	t.Logf("visible-file preparation: %s", time.Since(preparing).Round(time.Millisecond))
	started := time.Now()
	hover, err := m.Hover(context.Background(), request)
	t.Logf("first hover after preparation: %s; complete signature: %v", time.Since(started).Round(time.Millisecond), strings.Contains(hover.Contents, "string"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Second)
	for !strings.Contains(hover.Contents, "string") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		hover, err = m.Hover(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("complete signature after visible-file preparation: %s", time.Since(started).Round(time.Millisecond))
	if !strings.Contains(hover.Contents, "string") {
		t.Fatal("PHP signature did not become available")
	}
	started = time.Now()
	hover, err = m.Hover(context.Background(), request)
	t.Logf("warm hover: %s", time.Since(started).Round(time.Millisecond))
	if err != nil || !strings.Contains(hover.Contents, "string") {
		t.Fatalf("Warm hover: %+v %v", hover, err)
	}
}

func TestRealPHPReviewFetchesMissingCommitThroughSCPRemote(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	m, source, _ := realLSPFixture(t)
	phar := filepath.Join(os.Getenv("MIER_LSP_SMOKE_TOOLS"), "phpactor.phar")
	if _, err := os.Stat(phar); err != nil {
		t.Skip("Download the official Phpactor PHAR into MIER_LSP_SMOKE_TOOLS for PHP smoke testing")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := m.Configure(context.Background(), "php", lspclient.ServerConfiguration{Command: "php", Args: []string{phar, "language-server"}}); err != nil {
		t.Fatal(err)
	}
	git := func(root string, args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	bare := filepath.Join(t.TempDir(), "review remote.git")
	git(source, "clone", "--bare", "--", source, bare)
	barePath := filepath.ToSlash(bare)
	if runtime.GOOS == "windows" {
		barePath = "/" + barePath
	}
	bareURL := (&url.URL{Scheme: "file", Path: barePath}).String()
	remote := "git@github.com:team/lsp-review.git"
	// Only Git rewrites this URL. The application must accept its original SSH
	// syntax and the PR must still match the configured repository identity.
	git(source, "config", "--global", "url."+bareURL+".insteadOf", remote)
	review := filepath.Join(t.TempDir(), "review checkout")
	git(source, "clone", "--", remote, review)
	line := "$message = $greeter->reviewOnlyMethod('PHP');"
	if err := os.WriteFile(filepath.Join(source, "src", "Greeter.php"), []byte("<?php\nnamespace Smoke;\nclass Greeter {\n    public function reviewOnlyMethod(string $name): string { return 'Review ' . $name; }\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "use.php"), []byte("<?php\nuse Smoke\\Greeter;\n$greeter = new Greeter();\n"+line+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(source, "commit", "--all", "--quiet", "-m", "Review-only PHP method")
	sha := git(source, "rev-parse", "HEAD")
	git(source, "push", "--", bare, "HEAD:refs/pull/12/head")
	// Dirty code in the current checkout must not replace the displayed PR.
	if err := os.WriteFile(filepath.Join(review, "src", "use.php"), []byte("<?php LOCAL_DIRTY_SOURCE;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := git(review, "status", "--porcelain=v2", "--branch")
	refs := git(review, "show-ref")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	request := lspclient.CodePositionRequest{Root: review, Revision: sha, Path: "src/use.php", Host: "github.com", Owner: "team", Repo: "lsp-review", PullRequest: 12, Line: 3, Character: strings.Index(line, "reviewOnlyMethod") + 4, LineText: &line}
	hover, err := m.Hover(ctx, request)
	warmup := time.Now().Add(12 * time.Second)
	for err == nil && !strings.Contains(hover.Contents, "string") && time.Now().Before(warmup) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
		hover, err = m.Hover(ctx, request)
	}
	if err != nil || hover.Revision != sha || !strings.Contains(hover.Contents, "reviewOnlyMethod") || !strings.Contains(hover.Contents, "string") {
		t.Fatalf("PHP hover could not use the missing PR commit: %+v %v", hover, err)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil || len(targets) != 1 || targets[0].Path != "src/Greeter.php" || targets[0].Revision != sha {
		t.Fatalf("PHP definition could not use the missing PR commit: %+v %v", targets, err)
	}
	document, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: review, Revision: sha, Target: targets[0].URI})
	if err != nil || !strings.Contains(document.Text, "public function reviewOnlyMethod") {
		t.Fatalf("PHP definition source could not be read: %+v %v", document, err)
	}
	if git(review, "status", "--porcelain=v2", "--branch") != before || git(review, "show-ref") != refs {
		t.Fatal("PHP code navigation modified the checkout or its refs")
	}
}

func TestRealPHPHoverAndDependencyDefinitionWithLargeVendor(t *testing.T) {
	m, root, _ := realLSPFixture(t)
	phar := filepath.Join(os.Getenv("MIER_LSP_SMOKE_TOOLS"), "phpactor.phar")
	if _, err := os.Stat(phar); err != nil {
		t.Skip("Download the official Phpactor PHAR into MIER_LSP_SMOKE_TOOLS for PHP smoke testing")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := m.Configure(context.Background(), "php", lspclient.ServerConfiguration{Command: "php", Args: []string{phar, "language-server"}}); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		file := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	line := "$message = (new Greeter())->greet('PHP');"
	write("src/use.php", "<?php\nuse Installed\\Greeter;\n"+line+"\n")
	git("commit", "--all", "--quiet", "-m", "Installed dependency call")
	sha := git("rev-parse", "HEAD")
	// A real Composer-compatible loader exercises dependency lookup. The
	// absolute base path must be rebased to the reviewed snapshot.
	base := strings.ReplaceAll(filepath.ToSlash(root), "'", "\\'")
	write("vendor/autoload.php", "<?php\n$baseDir = '"+base+"';\n$loader = new \\Composer\\Autoload\\ClassLoader();\n$loader->addPsr4('Installed\\\\', $baseDir . '/vendor/fixture/library/src');\n$loader->register();\nreturn $loader;\n")
	write("vendor/fixture/library/src/Greeter.php", "<?php\nnamespace Installed;\nclass Greeter {\n    public function greet(string $name): string { return 'Dependency ' . $name; }\n}\n")
	large := "<?php /*" + strings.Repeat(" ", 2*1024*1024) + "*/\n"
	for index := 0; index < 65; index++ {
		write(fmt.Sprintf("vendor/fixture/library/fixtures/Source%02d.php", index), large)
	}
	write("vendor/fixture/library/assets/frontend.js", "export const unusedByPHP = true;\n")
	// The checkout's local code intentionally differs from the reviewed SHA.
	write("src/use.php", "<?php LOCAL_DIRTY_CODE;\n")
	before := git("status", "--porcelain=v2", "--branch")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/use.php", Line: 2, Character: strings.Index(line, "greet(") + 2, LineText: &line}
	hover, err := m.Hover(ctx, request)
	warmup := time.Now().Add(12 * time.Second)
	for err == nil && !strings.Contains(hover.Contents, "string") && time.Now().Before(warmup) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
		hover, err = m.Hover(ctx, request)
	}
	if err != nil || hover.Revision != sha || !strings.Contains(hover.Contents, "greet") || !strings.Contains(hover.Contents, "string") {
		t.Fatalf("PHP hover failed with >128 MiB vendor source: %+v %v", hover, err)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil || len(targets) != 1 || !targets[0].External || targets[0].Path != "vendor/fixture/library/src/Greeter.php" {
		t.Fatalf("Large vendor dependency definition failed: %+v %v", targets, err)
	}
	write("vendor/fixture/library/src/Greeter.php", "<?php CHANGED_AFTER_ANALYSIS;\n")
	document, err := m.ReadDefinition(ctx, lspclient.ReadDefinitionRequest{Root: root, Revision: targets[0].Revision, Target: targets[0].URI})
	if err != nil || !strings.Contains(document.Text, "public function greet") {
		t.Fatalf("Large vendor definition was not frozen: %+v %v", document, err)
	}
	if git("status", "--porcelain=v2", "--branch") != before {
		t.Fatal("Dependency analysis modified tracked source or Git state")
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
