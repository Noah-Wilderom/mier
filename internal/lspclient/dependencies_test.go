package lspclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyCaptureSupportsMoreThan128MiBOfPHPSource(t *testing.T) {
	root := t.TempDir()
	source := "<?php /*" + strings.Repeat(" ", 2*1024*1024) + "*/\n"
	for index := 0; index < 65; index++ {
		writeTestFile(t, filepath.Join(root, "vendor", "fixture", "library", fmt.Sprintf("Source%02d.php", index)), source)
	}
	writeTestFile(t, filepath.Join(root, "vendor", "fixture", "library", "Greeter.php"), "<?php class DependencyGreeter {}\n")
	cache := t.TempDir()
	w, err := extractWorkspace(context.Background(), root, cache, ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{"composer.json": "{}", "use.php": "<?php use DependencyGreeter;\n"})})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	if w.dependencyBytes <= 128*1024*1024 {
		t.Fatal("Fixture did not exceed the original dependency budget")
	}
	path := filepath.Join(w.directory, "vendor", "fixture", "library", "Greeter.php")
	writeTestFile(t, filepath.Join(root, "vendor", "fixture", "library", "Greeter.php"), "<?php CHANGED_DEPENDENCY;\n")
	text, _, external, err := w.read(path)
	if err != nil || !external || !strings.Contains(text, "class DependencyGreeter") {
		t.Fatalf("Large dependency capture lost frozen definition source: %q %v", text, err)
	}
}

func TestDependencyCaptureKeepsLargeComposerMetadataAndChecksItsBudget(t *testing.T) {
	root := t.TempDir()
	metadata := "<?php /*" + strings.Repeat(" ", maxDocumentBytes) + "*/ $baseDir = '" + filepath.ToSlash(root) + "';\n"
	writeTestFile(t, filepath.Join(root, "vendor", "composer", "autoload_classmap.php"), metadata)
	w, err := extractWorkspace(context.Background(), root, t.TempDir(), ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{"composer.json": "{}"})}, "vendor")
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	data, err := os.ReadFile(filepath.Join(w.directory, "vendor", "composer", "autoload_classmap.php"))
	if err != nil || !strings.Contains(string(data), filepath.ToSlash(w.directory)) || strings.Contains(string(data), filepath.ToSlash(root)) {
		t.Fatalf("Large Composer metadata was omitted or pointed to the dirty checkout: %v", err)
	}
	// Exercise the actual bound without allocating a GiB in a regression test.
	w.dependencyBytes = maxDependencyBytes - 1
	destination := filepath.Join(w.directory, "budget-test")
	if err := w.captureDependencies(context.Background(), filepath.Join(root, "vendor"), destination, "vendor", "vendor"); err == nil || !strings.Contains(err.Error(), "1 GiB") {
		t.Fatalf("Dependency budget was not enforced: %v", err)
	}
	if existingFile(filepath.Join(destination, "composer", "autoload_classmap.php")) {
		t.Fatal("Over-budget metadata was copied")
	}
}

func TestDependencyCopyCancellationRemovesPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.php")
	writeTestFile(t, path, "<?php class Source {}\n")
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "cancelled.php")
	_, err = copyDependency(ctx, source, target, info, maxDocumentBytes, make([]byte, 64*1024))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dependency copy ignored cancellation: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("Cancelled copy left a partial dependency file")
	}
}

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

func TestAnalysisScopesCanonicalizeRepositoryAndCacheAliases(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "actual")
	writeTestFile(t, filepath.Join(real, "repository", "node_modules", "normal", "index.ts"), "export const installed = true;\n")
	if err := os.MkdirAll(filepath.Join(real, "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("Directory symlinks unavailable")
	}
	writeTestFile(t, filepath.Join(real, "repository", "vendor", "composer", "autoload_static.php"), "<?php $baseDir = '"+filepath.ToSlash(filepath.Join(alias, "repository"))+"';\n")
	w, err := extractWorkspace(context.Background(), filepath.Join(alias, "repository"), filepath.Join(alias, "cache"), ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{"package.json": "{}", "use.ts": "export const selected = true;\n"})})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	canonical, err := filepath.EvalSymlinks(w.directory)
	if err != nil || canonical != w.directory {
		t.Fatalf("Workspace directory was not canonical: %q %q %v", w.directory, canonical, err)
	}
	for _, path := range []string{"use.ts", "node_modules/normal/index.ts"} {
		if _, _, _, err := w.read(filepath.Join(w.directory, filepath.FromSlash(path))); err != nil {
			t.Fatalf("Aliased source was incorrectly outside its scope: %s: %v", path, err)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(w.directory, "vendor", "composer", "autoload_static.php"))
	if err != nil || strings.Contains(string(metadata), filepath.ToSlash(alias)) || !strings.Contains(string(metadata), filepath.ToSlash(w.directory)) {
		t.Fatalf("Composer retained an alias to the original checkout: %s %v", metadata, err)
	}
}
