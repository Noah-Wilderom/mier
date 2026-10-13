package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mag-ik-een-review/internal/lspclient"
)

func TestRealTypeScriptWorkspaceDependencyUsesSelectedRevision(t *testing.T) {
	m, root, _ := realLSPFixture(t)
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
	write("packages/foo/package.json", `{"name":"@team/foo","version":"1.0.0","types":"index.ts"}`)
	write("packages/foo/index.ts", "export function getAnswer(): string { return 'Selected commit'; }\n")
	write("src/main.ts", "import { getAnswer } from '@team/foo'\nexport const answer = getAnswer()\n")
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "Workspace dependency fixture")
	sha := git("rev-parse", "HEAD")
	dependencyDirectory := filepath.Join(root, "node_modules", "@team")
	if err := os.MkdirAll(dependencyDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "packages", "foo"), filepath.Join(dependencyDirectory, "foo")); err != nil {
		t.Skipf("Workspace package symlinks unavailable: %v", err)
	}
	// Installed workspace links must not redirect committed analysis to dirty source.
	write("packages/foo/index.ts", "export function getAnswer(): number { return 42; }\n")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	line := "export const answer = getAnswer()"
	request := lspclient.CodePositionRequest{Root: root, Revision: sha, Path: "src/main.ts", Line: 1, Character: strings.Index(line, "getAnswer(") + 3, LineText: &line}
	hover, err := m.Hover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents, "getAnswer(): string") || strings.Contains(hover.Contents, "getAnswer(): number") {
		t.Fatalf("Workspace dependency leaked another source revision: %+v", hover)
	}
	targets, err := m.Definition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Path != "packages/foo/index.ts" || targets[0].External {
		t.Fatalf("Workspace dependency was not rebased into the selected source tree: %+v", targets)
	}
}
