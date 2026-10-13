package lspclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledDependencyCaptureRebasesWorkspaceLinksAndFreezesSources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "packages", "foo", "index.ts"), "export const answer = 42;\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "normal", "index.ts"), "export const dependency = 'installed';\n")
	writeTestFile(t, filepath.Join(root, "vendor", "composer", "autoload_static.php"), "<?php $baseDir = '"+filepath.ToSlash(root)+"';\n")
	alias := filepath.Join(root, "node_modules", "@team", "foo")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "packages", "foo"), alias); err != nil {
		t.Skip("Workspace symlinks unavailable")
	}
	outside := filepath.Join(t.TempDir(), "private.ts")
	writeTestFile(t, outside, "export const secret = true;\n")
	if err := os.Symlink(outside, filepath.Join(root, "node_modules", "escaped.ts")); err != nil {
		t.Fatal(err)
	}
	w, err := extractWorkspace(context.Background(), root, t.TempDir(), ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{
		"package.json": "{}", "composer.json": "{}", "packages/foo/index.ts": "export const answer = 'selected';\n",
		"node_modules/tracked/index.ts": "export const tracked = 'selected';\n",
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	linked, err := filepath.EvalSymlinks(filepath.Join(w.directory, "node_modules", "@team", "foo", "index.ts"))
	if err != nil || linked != filepath.Join(w.directory, "packages", "foo", "index.ts") {
		t.Fatalf("Workspace alias escaped selected revision: %s, %v", linked, err)
	}
	text, path, external, err := w.read(linked)
	if err != nil || !strings.Contains(text, "'selected'") || path != "packages/foo/index.ts" || external {
		t.Fatalf("Project package classified as dependency or wrong source: %q %q %v %v", text, path, external, err)
	}
	writeTestFile(t, filepath.Join(root, "node_modules", "normal", "index.ts"), "export const dependency = 'dirty';\n")
	text, path, external, err = w.read(filepath.Join(w.directory, "node_modules", "normal", "index.ts"))
	if err != nil || !strings.Contains(text, "'installed'") || !external || path != "node_modules/normal/index.ts" {
		t.Fatalf("Installed source was not frozen/scoped: %q %q %v %v", text, path, external, err)
	}
	metadata, err := os.ReadFile(filepath.Join(w.directory, "vendor", "composer", "autoload_static.php"))
	if err != nil || strings.Contains(string(metadata), filepath.ToSlash(root)) || !strings.Contains(string(metadata), filepath.ToSlash(w.directory)) {
		t.Fatalf("Composer metadata points at original project: %s %v", metadata, err)
	}
	if _, err := os.Lstat(filepath.Join(w.directory, "node_modules", "escaped.ts")); !os.IsNotExist(err) {
		t.Fatal("Outside dependency link was captured")
	}
}

func TestPlaintextHoverPreservesLiteralMarkupCharacters(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Example;\n"})
	t.Setenv("MIER_LSP_PLAINTEXT_HOVER", "1")
	hover, err := m.Hover(context.Background(), requestFor(root, "use.php"))
	if err != nil || hover.Kind != "plaintext" || hover.Contents != "List<T> contains **literal** markers" {
		t.Fatalf("Plaintext hover was changed into Markdown: %#v, %v", hover, err)
	}
	for _, raw := range []string{`"MarkedString **markdown**"`, `{"kind":"markdown","value":"**markdown**"}`, `[{"language":"typescript","value":"List<T>"}]`} {
		if hoverKind([]byte(raw)) != "markdown" {
			t.Fatalf("Protocol Markdown hover incorrectly treated as plaintext: %s", raw)
		}
	}
}
