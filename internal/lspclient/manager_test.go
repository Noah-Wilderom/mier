package lspclient

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var testRevision = strings.Repeat("a", 40)

func sourceArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	for path, text := range files {
		if err := writer.WriteHeader(&tar.Header{Name: path, Mode: 0600, Size: int64(len(text)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func managerHarness(t *testing.T, files map[string]string) (*Manager, string) {
	t.Helper()
	t.Setenv("MIER_LSP_TEST_CHILD", "1")
	t.Setenv("MIER_LSP_DEFINITION_URI", "")
	directory := t.TempDir()
	root := filepath.Join(directory, "repository")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	archive := sourceArchive(t, files)
	m := New(Options{ConfigPath: filepath.Join(directory, "settings", "language-servers.json"), CacheDirectory: filepath.Join(directory, "cache"), Archive: func(_ context.Context, request CodePositionRequest) (ArchiveResult, error) {
		return ArchiveResult{Revision: request.Revision, Tar: archive}, nil
	}})
	command := fakeCommand(t, false)
	if err := m.Configure(context.Background(), "php", ServerConfiguration{Command: command.Command, Args: command.Args}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m, root
}
func requestFor(root, path string) CodePositionRequest {
	return CodePositionRequest{Root: root, Revision: testRevision, Path: path, Line: 0, Character: 6}
}

func TestManagerUsesFullSelectedSourceAndOpaqueRecursiveDefinitionGrants(t *testing.T) {
	selected := "<?php use SELECTED_COMMIT;\n"
	definition := "<?php class SELECTED_COMMIT {}\n"
	m, root := managerHarness(t, map[string]string{"use.php": selected, "definition.php": definition})
	writeTestFile(t, filepath.Join(root, "use.php"), "<?php use LOCAL_DIRTY_HEAD;\n")
	request := requestFor(root, "use.php")
	line := strings.TrimSuffix(selected, "\n")
	request.LineText = &line
	hover, err := m.Hover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents, "SELECTED_COMMIT") || strings.Contains(hover.Contents, "LOCAL_DIRTY_HEAD") || hover.Revision != testRevision {
		t.Fatalf("Hover analyzed wrong source: %#v", hover)
	}
	targets, err := m.Definition(context.Background(), request)
	if err != nil || len(targets) != 1 {
		t.Fatalf("Definition = %#v, %v", targets, err)
	}
	target := targets[0]
	if !strings.HasPrefix(target.URI, "mier-lsp://definition/") || target.External || target.Path != "definition.php" {
		t.Fatalf("Unsafe definition DTO: %#v", target)
	}
	document, err := m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: target.URI})
	if err != nil || document.Text != definition || document.URI != target.URI {
		t.Fatalf("Definition document = %#v, %v", document, err)
	}
	recursive := requestFor(root, target.URI)
	recursive.Line = target.Range.Start.Line
	recursive.Character = target.Range.Start.Character
	hover, err = m.Hover(context.Background(), recursive)
	if err != nil || !strings.Contains(hover.Contents, "class SELECTED_COMMIT") {
		t.Fatalf("Opaque recursive hover failed: %#v, %v", hover, err)
	}
	for _, wrong := range []ReadDefinitionRequest{{Root: root, Revision: strings.Repeat("b", 40), Target: target.URI}, {Root: root, Revision: testRevision, Target: fileURI(filepath.Join(root, "use.php"))}, {Root: t.TempDir(), Revision: testRevision, Target: target.URI}} {
		if _, err = m.ReadDefinition(context.Background(), wrong); err == nil {
			t.Fatal("Cross-repository/revision or arbitrary filesystem target was accepted")
		}
	}
}

func TestStaleLinesAndUTF16SurrogateBoundaries(t *testing.T) {
	text := "<?php $emoji = \"😀\"; useSymbol();\n"
	prefix := strings.Index(text, "😀")
	if err := validatePosition(text, CodePositionRequest{Line: 0, Character: prefix + 1}); err == nil {
		t.Fatal("Position split a surrogate pair")
	}
	if err := validatePosition(text, CodePositionRequest{Line: 0, Character: prefix + 2}); err != nil {
		t.Fatal(err)
	}
	wrong := "<?php $emoji = \"different\";"
	for _, request := range []CodePositionRequest{{Line: -1}, {Line: 99}, {Line: 0, Character: 999}, {Line: 0, Character: 0, LineText: &wrong}} {
		if err := validatePosition(text, request); err == nil {
			t.Fatalf("Invalid/stale position accepted: %#v", request)
		}
	}
}

func TestManagerRejectsTraversalAndUnsafeServerDefinitionTargets(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Example;\n"})
	for _, path := range []string{"../outside.php", "/etc/passwd", "a\\..\\outside.php", ".git/config", "file:///etc/passwd"} {
		if _, err := m.Hover(context.Background(), requestFor(root, path)); err == nil {
			t.Fatalf("Traversal/absolute source accepted: %s", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "private.php")
	writeTestFile(t, outside, "<?php PRIVATE_VALUE;\n")
	t.Setenv("MIER_LSP_DEFINITION_URI", fileURI(outside))
	if _, err := m.Definition(context.Background(), requestFor(root, "use.php")); err == nil {
		t.Fatal("Server-returned arbitrary file was granted")
	}
}

func TestDependencyDefinitionsAreConfinedAndFrozenWhenGranted(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Dependency;\n", "composer.json": "{}"})
	dependency := filepath.Join(root, "vendor", "package", "definition.php")
	original := "<?php class Dependency {}\n"
	writeTestFile(t, dependency, original)
	t.Setenv("MIER_LSP_DEFINITION_URI", fileURI(dependency))
	targets, err := m.Definition(context.Background(), requestFor(root, "use.php"))
	if err != nil || len(targets) != 1 || !targets[0].External {
		t.Fatalf("Installed dependency definition not supported: %#v, %v", targets, err)
	}
	writeTestFile(t, dependency, "<?php CHANGED_AFTER_GRANT;\n")
	document, err := m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: targets[0].URI})
	if err != nil || document.Text != original {
		t.Fatal("Granted dependency content changed underneath its displayed coordinates")
	}
	outside := filepath.Join(t.TempDir(), "secret.php")
	writeTestFile(t, outside, "<?php SECRET;\n")
	link := filepath.Join(root, "vendor", "package", "escaped.php")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("Symlinks unavailable")
	}
	t.Setenv("MIER_LSP_DEFINITION_URI", fileURI(link))
	command := fakeCommand(t, false)
	if err = m.Configure(context.Background(), "php", ServerConfiguration{Command: command.Command, Args: command.Args}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Definition(context.Background(), requestFor(root, "use.php")); err == nil {
		t.Fatal("Dependency symlink escaped its allowed root")
	}
}

func TestPHPAndJavaScriptCaptureOnlyTheirOwnDependencies(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Dependency;\n", "use.ts": "export const selected = true;\n", "composer.json": "{}", "package.json": "{}"})
	writeTestFile(t, filepath.Join(root, "vendor", "library", "Dependency.php"), "<?php class Dependency {}\n")
	writeTestFile(t, filepath.Join(root, "vendor", "library", "frontend.js"), "export const irrelevant = true;\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "library", "index.ts"), "export const dependency = true;\n")
	sdk := filepath.Join(t.TempDir(), "typescript", "lib")
	writeTestFile(t, filepath.Join(sdk, "tsserver.js"), "// SDK detection fixture\n")
	command := fakeCommand(t, false)
	if err := m.Configure(context.Background(), "typescript", ServerConfiguration{Command: command.Command, Args: command.Args, TypeScriptSDKPath: sdk}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"use.php", "use.ts", "use.php", "use.ts"} {
		if _, err := m.Hover(context.Background(), requestFor(root, path)); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.workspaces) != 2 {
		t.Fatalf("Expected reusable PHP and JS snapshots, got %d", len(m.workspaces))
	}
	for _, w := range m.workspaces {
		php := existingFile(filepath.Join(w.directory, "vendor", "library", "Dependency.php"))
		js := existingFile(filepath.Join(w.directory, "node_modules", "library", "index.ts"))
		if php == js {
			t.Fatal("Snapshot copied both dependency families or omitted its required source")
		}
		if php && existingFile(filepath.Join(w.directory, "vendor", "library", "frontend.js")) {
			t.Fatal("PHP capture included unrelated frontend source from vendor")
		}
	}
}

func TestArchiveExtractionRejectsTraversalAndSkipsTrackedSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../secret.php", "/absolute.php", ".git/config", "dir\\escape.php"} {
		_, err := extractWorkspace(context.Background(), root, t.TempDir(), ArchiveResult{Revision: testRevision, Tar: sourceArchive(t, map[string]string{path: "secret"})})
		if err == nil {
			t.Fatalf("Unsafe archive path accepted: %s", path)
		}
	}
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	writer.WriteHeader(&tar.Header{Name: "linked.php", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	writer.Close()
	w, err := extractWorkspace(context.Background(), root, t.TempDir(), ArchiveResult{Revision: testRevision, Tar: data.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	if _, err = os.Lstat(filepath.Join(w.directory, "linked.php")); !os.IsNotExist(err) {
		t.Fatal("Tracked symlink was materialized in the immutable analysis tree")
	}
}

func TestServerConfigurationPersistsReadbackAndMissingToolsAreActionable(t *testing.T) {
	m, root := managerHarness(t, map[string]string{})
	t.Setenv("PATH", t.TempDir())
	statuses, err := m.Servers(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(serverCatalog) || !statuses[0].Available || statuses[0].Source != "configured" || statuses[0].Configuration.Command == "" {
		t.Fatalf("Configured server status incomplete: %d entries", len(statuses))
	}
	for _, status := range statuses[1:] {
		if status.Available || status.Error == "" || !strings.HasPrefix(status.SetupURL, "https://") {
			t.Fatalf("Missing server lacked actionable setup: %#v", status)
		}
	}
	restored := New(Options{ConfigPath: m.options.ConfigPath, CacheDirectory: t.TempDir()})
	defer restored.Close()
	readback, err := restored.Servers(context.Background(), root)
	if err != nil || readback[0].Configuration.Command != statuses[0].Configuration.Command {
		t.Fatal("Language server configuration did not persist")
	}
	if err = m.Configure(context.Background(), "not-a-catalog-server", ServerConfiguration{}); err == nil {
		t.Fatal("Unsupported language accepted")
	}
	if err = m.Configure(context.Background(), "php", ServerConfiguration{Command: "php", Args: []string{"\x00"}}); err == nil {
		t.Fatal("NUL argument accepted")
	}
	if err = m.Configure(context.Background(), "typescript", ServerConfiguration{TypeScriptSDKPath: "../lib"}); err == nil {
		t.Fatal("Relative dependency path accepted")
	}
}

func TestVueUsesPeerTypeScriptBrokerAndPlugin(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"Component.vue": "<script setup lang=\"ts\">const count = 1;</script>\n", "tsconfig.json": "{}"})
	sdk := filepath.Join(t.TempDir(), "typescript", "lib")
	writeTestFile(t, filepath.Join(sdk, "tsserver.js"), "// fixture SDK")
	plugin := filepath.Join(t.TempDir(), "vue-typescript-plugin")
	writeTestFile(t, filepath.Join(plugin, "package.json"), `{"name":"@vue/typescript-plugin"}`)
	typescript := fakeCommand(t, false)
	vue := fakeCommand(t, true)
	for language, command := range map[string]ServerCommand{"typescript": typescript, "vue": vue} {
		if err := m.Configure(context.Background(), language, ServerConfiguration{Command: command.Command, Args: command.Args, TypeScriptSDKPath: sdk, VuePluginPath: plugin}); err != nil {
			t.Fatal(err)
		}
	}
	request := requestFor(root, "Component.vue")
	request.Character = 30
	hover, err := m.Hover(context.Background(), request)
	if err != nil || hover.Contents != "Vue broker reached the peer TypeScript server" {
		t.Fatalf("Hybrid Vue server did not get broker response: %#v, %v", hover, err)
	}
	// Vue can remain alive while its peer exits. The next request must create
	// both a viable peer and a fresh Vue session instead of failing forever.
	for _, w := range m.workspaces {
		if peer := w.sessions["vue-typescript"]; peer != nil {
			peer.rpc.Close()
		}
	}
	hover, err = m.Hover(context.Background(), request)
	if err != nil || hover.Contents != "Vue broker reached the peer TypeScript server" {
		t.Fatalf("Vue did not recover from dead peer: %#v, %v", hover, err)
	}
}

func TestCachedSnapshotsConcurrencyAndEvictedDefinitionGrants(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Cached;\n", "definition.php": "<?php class Cached {}\n"})
	var mu sync.Mutex
	archives := 0
	original := m.options.Archive
	m.options.Archive = func(ctx context.Context, request CodePositionRequest) (ArchiveResult, error) {
		mu.Lock()
		archives++
		mu.Unlock()
		return original(ctx, request)
	}
	var workers sync.WaitGroup
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := m.Hover(context.Background(), requestFor(root, "use.php")); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if archives != 1 {
		t.Fatalf("Same immutable snapshot rebuilt %d times", archives)
	}
	targets, err := m.Definition(context.Background(), requestFor(root, "use.php"))
	if err != nil {
		t.Fatal(err)
	}
	for _, digit := range []string{"b", "c", "d", "e"} {
		request := requestFor(root, "use.php")
		request.Revision = strings.Repeat(digit, 40)
		if _, err = m.Hover(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.workspaces) > 4 {
		t.Fatal("Snapshot cache exceeded its bound")
	}
	if _, err = m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: targets[0].URI}); err == nil {
		t.Fatal("Evicted snapshot retained a stale file grant")
	}
}

func TestLocationAndLocationLinkDefinitionsAndHoverFormats(t *testing.T) {
	for _, value := range []string{`{"uri":"file:///source.php","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}}}`, `[{"targetUri":"file:///source.php","targetRange":{"start":{"line":0,"character":0},"end":{"line":5,"character":0}},"targetSelectionRange":{"start":{"line":2,"character":4},"end":{"line":2,"character":8}}}]`} {
		locations, err := parseDefinitions(json.RawMessage(value))
		if err != nil || len(locations) != 1 {
			t.Fatal("Location / LocationLink rejected")
		}
	}
	if hoverContents(json.RawMessage(`{"kind":"markdown","value":"**Symbol**"}`)) != "**Symbol**" || !strings.Contains(hoverContents(json.RawMessage(`[{"language":"php","value":"class Foo"},"documentation"]`)), "```php\nclass Foo") {
		t.Fatal("LSP hover formats were lost")
	}
}

func TestMutableRequestsRecaptureDigestRejectStaleLinesAndKeepFrozenGrants(t *testing.T) {
	m, root := managerHarness(t, map[string]string{})
	current := "<?php use WORKING_ONE;\n"
	captures := 0
	m.options.Archive = func(_ context.Context, request CodePositionRequest) (ArchiveResult, error) {
		captures++
		data := sourceArchive(t, map[string]string{"use.php": current, "definition.php": "<?php class SnapshotDefinition {}\n"})
		digest := sha256.Sum256(data)
		return ArchiveResult{Revision: hex.EncodeToString(digest[:]), Tar: data}, nil
	}
	request := requestFor(root, "use.php")
	request.Revision = "working"
	line := strings.TrimSuffix(current, "\n")
	request.LineText = &line
	first, err := m.Hover(context.Background(), request)
	if err != nil || len(first.Revision) != 64 {
		t.Fatalf("Mutable source did not become immutable: %#v, %v", first, err)
	}
	targets, err := m.Definition(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	current = "<?php use WORKING_TWO;\n"
	if _, err = m.Hover(context.Background(), request); err == nil {
		t.Fatal("Stale local line was analyzed at new content")
	}
	line = strings.TrimSuffix(current, "\n")
	request.LineText = &line
	second, err := m.Hover(context.Background(), request)
	if err != nil || second.Revision == first.Revision || !strings.Contains(second.Contents, "WORKING_TWO") {
		t.Fatalf("Mutable source reused stale cache: %#v, %v", second, err)
	}
	before := captures
	definitionRequest := requestFor(root, targets[0].URI)
	definitionRequest.Revision = targets[0].Revision
	definitionRequest.Character = 0
	if _, err = m.Hover(context.Background(), definitionRequest); err != nil {
		t.Fatal(err)
	}
	if captures != before {
		t.Fatal("Opaque immutable grant performed mutable Git capture")
	}
	request.ComparisonHead = testRevision
	if _, err = m.Hover(context.Background(), request); err == nil {
		t.Fatal("Merge comparison accepted a mutable layer")
	}
	request.ComparisonHead = ""
	request.LineText = nil
	if _, err = m.Hover(context.Background(), request); err == nil {
		t.Fatal("Mutable position accepted without displayed line validation")
	}
}

func TestReferencesAndHierarchicalSymbolsUseRevisionBoundReader(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Example;", "definition.php": "<?php class Example {}"})
	request := requestFor(root, "use.php")
	references, err := m.References(context.Background(), request)
	if err != nil || len(references) != 1 || references[0].Path != "use.php" {
		t.Fatalf("references: %#v %v", references, err)
	}
	outline, err := m.DocumentSymbols(context.Background(), request)
	if err != nil || len(outline) != 2 || outline[1].Depth != 1 || outline[1].Container != "Example" {
		t.Fatalf("outline: %#v %v", outline, err)
	}
	symbols, err := m.WorkspaceSymbols(context.Background(), SymbolRequest{CodePositionRequest: request, Query: "Example"})
	if err != nil || len(symbols) != 1 || symbols[0].Path != "definition.php" {
		t.Fatalf("workspace symbols: %#v %v", symbols, err)
	}
	doc, err := m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: symbols[0].URI})
	if err != nil || !strings.Contains(doc.Text, "class Example") {
		t.Fatalf("symbol reader: %#v %v", doc, err)
	}
	if _, err = m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: t.TempDir(), Revision: testRevision, Target: references[0].URI}); err == nil {
		t.Fatal("cross-repository reference grant accepted")
	}
}

func TestRestartAnalysisInvalidatesGrantsAndRecapturesInstalledDependencies(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Original;", "definition.php": "<?php class Original {}"})
	request := requestFor(root, "use.php")
	targets, err := m.Definition(context.Background(), request)
	if err != nil || len(targets) != 1 {
		t.Fatal(targets, err)
	}
	if err = m.RestartAnalysis(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: targets[0].URI}); err == nil {
		t.Fatal("restart retained an old navigation grant")
	}
	hover, err := m.Hover(context.Background(), request)
	if err != nil || !strings.Contains(hover.Contents, "Original") {
		t.Fatalf("analysis did not restart: %#v %v", hover, err)
	}
}
