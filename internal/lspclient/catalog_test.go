package lspclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogContainsUsableDefinitions(t *testing.T) {
	if len(serverCatalog) < 40 || len(toolLanguages) < 40 {
		t.Fatalf("Expected at least 40 catalog entries and managed recipes, got %d / %d", len(serverCatalog), len(toolLanguages))
	}
	for _, definition := range serverCatalog {
		t.Run(definition.ID, func(t *testing.T) {
			if !catalogID.MatchString(definition.ID) || definition.Name == "" || definition.Description == "" || definition.Label == "" || definition.Category == "" || definition.Program == "" || definition.Install.Requires == "" || len(definition.Files) == 0 {
				t.Fatal("Incomplete catalog entry")
			}
			homepage, err := url.Parse(definition.Homepage)
			if err != nil || homepage.Scheme != "https" || homepage.Host == "" {
				t.Fatal("Missing official HTTPS setup documentation")
			}
			switch definition.Install.Method {
			case "npm", "python", "go":
				if definition.Install.Package == "" || definition.Install.Binary == "" || strings.ContainsAny(definition.Install.Binary, "/\\") {
					t.Fatal("Incomplete managed recipe")
				}
				if definition.Install.Method == "go" && definition.Install.Module == "" {
					t.Fatal("Go recipe has no module version source")
				}
			case "php", "external":
			default:
				t.Fatal("Unknown installation method")
			}
			for _, file := range definition.Files {
				if file.LanguageID == "" || file.Pattern == "" || strings.ContainsAny(file.Pattern, "\\\x00[]") || strings.Contains(file.Pattern, "..") {
					t.Fatalf("Invalid file association: %#v", file)
				}
				filename := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(file.Pattern, "**/", ""), "*", "sample"), "?", "x")
				found := false
				for _, match := range serversForPath(filename) {
					found = found || (match.ID == definition.ID && match.LanguageID == file.LanguageID)
				}
				if !found {
					t.Fatalf("Association cannot route %q", filename)
				}
			}
		})
	}
}

func TestCatalogRoutingPrefersSpecificFileContexts(t *testing.T) {
	for _, example := range []struct{ filename, server, language string }{
		{"Dockerfile", "dockerfile", "dockerfile"}, {"build/Dockerfile.prod", "dockerfile", "dockerfile"},
		{"build/Containerfile", "dockerfile", "dockerfile"}, {"build/test.dockerfile", "dockerfile", "dockerfile"},
		{".github/workflows/ci.yml", "github-actions", "yaml"}, {".github/actions/test/action.yaml", "github-actions", "yaml"},
		{"docker-compose.override.yml", "docker-compose", "dockercompose"}, {"infra/compose.yaml", "docker-compose", "dockercompose"},
		{"config/settings.yaml", "yaml", "yaml"}, {"infra/playbooks/deploy/main.yml", "ansible", "yaml.ansible"},
		{"roles/api/tasks/main.yaml", "ansible", "yaml.ansible"}, {"azure-pipelines.yml", "azure-pipelines", "yaml"},
		{"templates/deployment.yaml", "helm", "helm"}, {"deploy/charts/api/templates/nested/service.yml", "helm", "helm"},
		{".vscode/settings.jsonc", "json", "jsonc"}, {"main.go", "go", "go"}, {"main.cpp", "cpp", "cpp"},
		{"hello.py", "python", "python"}, {"library.rs", "rust", "rust"}, {"index.tsx", "typescript", "typescriptreact"},
		{"index.jsx", "typescript", "javascriptreact"}, {"App.vue", "vue", "vue"}, {"src/App.svelte", "svelte", "svelte"},
	} {
		t.Run(example.filename, func(t *testing.T) {
			candidates := serversForPath(example.filename)
			if len(candidates) == 0 || candidates[0].ID != example.server || candidates[0].LanguageID != example.language {
				t.Fatalf("Wrong server/protocol language for %s: %#v", example.filename, candidates)
			}
		})
	}
	for _, unsupported := range []string{"image.png", "README", "package.lock", ".github/other/ci.unknown"} {
		if len(serversForPath(unsupported)) > 0 {
			t.Fatalf("Unexpected association for %s", unsupported)
		}
	}
}

func TestNewCatalogServersHandleHoverDefinitionsAndYAMLFallback(t *testing.T) {
	for _, example := range []struct{ server, filename, language string }{
		{"dockerfile", "Dockerfile", "dockerfile"}, {"github-actions", ".github/workflows/ci.yml", "yaml"},
		{"yaml", ".github/workflows/ci.yaml", "yaml"}, {"docker-compose", "compose.yaml", "dockercompose"},
		{"go", "main.go", "go"}, {"python", "main.py", "python"}, {"cpp", "main.cpp", "cpp"},
	} {
		t.Run(example.server, func(t *testing.T) {
			m, root := managerHarness(t, map[string]string{example.filename: "selected source text\n"})
			command := fakeCommand(t, false)
			if err := m.Configure(context.Background(), example.server, ServerConfiguration{Command: command.Command, Args: command.Args}); err != nil {
				t.Fatal(err)
			}
			hover, err := m.Hover(context.Background(), requestFor(root, example.filename))
			if err != nil || hover.Language != example.language || !strings.Contains(hover.Contents, "selected source") {
				t.Fatalf("Hover = %#v, %v", hover, err)
			}
			targets, err := m.Definition(context.Background(), requestFor(root, example.filename))
			if err != nil || len(targets) != 1 || targets[0].Path != example.filename {
				t.Fatalf("Definitions = %#v, %v", targets, err)
			}
			document, err := m.ReadDefinition(context.Background(), ReadDefinitionRequest{Root: root, Revision: testRevision, Target: targets[0].URI})
			if err != nil || document.Text != "selected source text\n" {
				t.Fatalf("Read definition = %#v, %v", document, err)
			}
			for _, w := range m.workspaces {
				if w.sessions[example.server] == nil {
					t.Fatal("The selected catalog server was not started")
				}
				var initializedRoot string
				if err := w.sessions[example.server].rpc.Request(context.Background(), "test/initializationRoot", map[string]any{}, &initializedRoot); err != nil || initializedRoot != w.directory {
					t.Fatalf("Legacy rootPath must identify the selected snapshot: %q, %v", initializedRoot, err)
				}
				var language string
				if err := w.sessions[example.server].rpc.Request(context.Background(), "test/documentLanguage", map[string]any{"uri": fileURI(filepath.Join(w.directory, example.filename))}, &language); err != nil || language != example.language {
					t.Fatalf("didOpen language = %q, %v", language, err)
				}
			}
		})
	}
}

func TestCatalogServerSettingsAndSDKInitialization(t *testing.T) {
	if configurationSection("yaml", "yaml.hover") != true || configurationSection("yaml", "yaml.schemaStore.url") == nil {
		t.Fatal("YAML schema/hover settings were not routed")
	}
	if _, ok := configurationSection("yaml", "unknown.option").(map[string]any); !ok {
		t.Fatal("Missing configuration must return an empty object")
	}
	for _, language := range []string{"astro", "mdx"} {
		t.Run(language, func(t *testing.T) {
			filename := "example." + language
			m, root := managerHarness(t, map[string]string{filename: "selected source\n"})
			command := fakeCommand(t, false)
			sdk := filepath.Join(t.TempDir(), "typescript", "lib")
			writeTestFile(t, filepath.Join(sdk, "tsserver.js"), "// fixture SDK")
			if err := m.Configure(context.Background(), language, ServerConfiguration{Command: command.Command, Args: command.Args, TypeScriptSDKPath: sdk}); err != nil {
				t.Fatal(err)
			}
			if err := m.Warmup(context.Background(), requestFor(root, filename)); err != nil {
				t.Fatal(err)
			}
			for _, w := range m.workspaces {
				var initialization struct {
					TypeScript struct {
						TSDK    string `json:"tsdk"`
						Enabled bool   `json:"enabled"`
					} `json:"typescript"`
				}
				if err := w.sessions[language].rpc.Request(context.Background(), "test/initializationOptions", nil, &initialization); err != nil || initialization.TypeScript.TSDK != sdk || !initialization.TypeScript.Enabled {
					t.Fatalf("Missing Volar SDK initialization: %#v, %v", initialization, err)
				}
			}
		})
	}
}

func TestConfigurationFilesSkipUnrelatedInstalledDependencies(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"compose.yaml": "services: {}\n", "package.json": "{}", "composer.json": "{}"})
	writeTestFile(t, filepath.Join(root, "node_modules", "large", "index.ts"), "export const ignored = true")
	writeTestFile(t, filepath.Join(root, "vendor", "large", "ignored.php"), "<?php")
	command := fakeCommand(t, false)
	if err := m.Configure(context.Background(), "docker-compose", ServerConfiguration{Command: command.Command, Args: command.Args}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Hover(context.Background(), requestFor(root, "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, w := range m.workspaces {
		if existingDirectory(filepath.Join(w.directory, "node_modules")) || existingDirectory(filepath.Join(w.directory, "vendor")) {
			t.Fatal("Configuration analysis copied unrelated dependencies")
		}
	}
}

func TestGenericNPMPackagesInstallUpdateAndRetainPreviousCopy(t *testing.T) {
	for _, language := range []string{"dockerfile", "docker-compose", "yaml", "github-actions", "python", "json", "astro", "mdx"} {
		t.Run(language, func(t *testing.T) {
			fixture := toolProvider(t)
			m := managedHarness(t, fixture)
			installFixture(t, m, language)
			before, ready := m.tools.configuration(language)
			if !ready || (before.TypeScriptSDKPath != "") != usesTypeScriptSDK(language) || !reflect.DeepEqual(before.Args[1:], catalogByID[language].Args) {
				t.Fatalf("Incorrect server invocation: %#v", before)
			}
			fixture.serverVersion.Store("6.0.2")
			if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
				t.Fatal(err)
			}
			if !m.tools.statuses(context.Background())[language].UpdateAvailable {
				t.Fatal("Update was not detected")
			}
			installFixture(t, m, language)
			after, _ := m.tools.configuration(language)
			if before.Args[0] == after.Args[0] || !existingFile(before.Args[0]) {
				t.Fatal("Update overwrote the working installation")
			}
		})
	}
}

func TestNPMExecutableMetadataIsConfinedToItsPackage(t *testing.T) {
	directory := t.TempDir()
	definition := catalogByID["yaml"]
	root := filepath.Join(directory, "node_modules", definition.Install.Package)
	writeTestFile(t, filepath.Join(root, "bin", "server.js"), "// server")
	for _, bin := range []any{"bin/server.js", map[string]string{definition.Install.Binary: "bin/server.js"}} {
		data, _ := json.Marshal(map[string]any{"name": definition.Install.Package, "bin": bin})
		writeTestFile(t, filepath.Join(root, "package.json"), string(data))
		if _, err := npmServerEntry(directory, definition); err != nil {
			t.Fatal(err)
		}
	}
	for _, bin := range []any{"../outside.js", "/etc/passwd", `bin\server.js`, map[string]string{"other": "bin/server.js"}} {
		data, _ := json.Marshal(map[string]any{"name": definition.Install.Package, "bin": bin})
		writeTestFile(t, filepath.Join(root, "package.json"), string(data))
		if _, err := npmServerEntry(directory, definition); err == nil {
			t.Fatalf("Invalid bin accepted: %#v", bin)
		}
	}
}

func TestGoAndPythonRecipesUsePrivatePinnedInstallations(t *testing.T) {
	for _, language := range []string{"go", "helm", "cmake", "fortran", "robot"} {
		t.Run(language, func(t *testing.T) {
			fixture := toolProvider(t)
			m := managedHarness(t, fixture)
			definition := catalogByID[language]
			m.tools.run = func(ctx context.Context, directory, program string, args ...string) ([]byte, error) {
				if definition.Install.Method == "go" {
					env, _ := ctx.Value(toolEnvironmentKey{}).(map[string]string)
					cgo := "0"
					if definition.Install.CGO {
						cgo = "1"
					}
					if env["GOBIN"] != filepath.Join(directory, "bin") || env["GOTOOLCHAIN"] != "local" || env["GOSUMDB"] != "sum.golang.org" || env["GOENV"] != "off" || env["CGO_ENABLED"] != cgo || env["GOPROXY"] != "https://proxy.golang.org" || env["GOPRIVATE"] != "" {
						t.Fatal("Go install inherited project/toolchain configuration")
					}
					if !reflect.DeepEqual(args, []string{"install", "-p=2", "-trimpath", definition.Install.Package + "@v6.0.1"}) || env["GOMAXPROCS"] != "2" {
						t.Fatalf("Unpinned Go install: %v", args)
					}
					writeTestFile(t, toolBinary(directory, "go", path.Base(definition.Install.Package)), "fixture compiled binary")
				} else if reflect.DeepEqual(args[:2], []string{"-m", "venv"}) {
					if args[2] != "--copies" || args[3] != filepath.Join(directory, "venv") {
						t.Fatal("Python environment is not private")
					}
					writeTestFile(t, toolBinary(directory, "python", "python"), "fixture interpreter")
				} else {
					if !strings.Contains(strings.Join(args, " "), "--index-url https://pypi.org/simple ") || !strings.Contains(strings.Join(args, " "), definition.Install.Package+"==6.0.1") || !strings.Contains(strings.Join(args, " "), "--isolated") || !strings.Contains(strings.Join(args, " "), "--only-binary=:all:") {
						t.Fatalf("Unpinned/nonisolated pip install: %v", args)
					}
					for dependency, version := range definition.Install.Dependencies {
						if !strings.Contains(strings.Join(args, " "), dependency+"=="+version) {
							t.Fatal("Missing pinned compatibility dependency")
						}
					}
					prefix := filepath.Dir(filepath.Dir(filepath.Dir(program)))
					writeTestFile(t, toolBinary(prefix, "python", definition.Install.Binary), "fixture Python launcher")
				}
				return nil, nil
			}
			installFixture(t, m, language)
			before, ready := m.tools.configuration(language)
			if !ready || before.TypeScriptSDKPath != "" || !reflect.DeepEqual(before.Args, definition.Args) {
				t.Fatalf("Wrong managed launch: %#v", before)
			}
			fixture.serverVersion.Store("6.0.2")
			m.tools.run = func(context.Context, string, string, ...string) ([]byte, error) {
				return nil, errors.New("build/download failed")
			}
			if err := m.InstallServer(context.Background(), language); err != nil {
				t.Fatal(err)
			}
			status := waitForTool(t, m, language)
			after, _ := m.tools.configuration(language)
			if !status.Ready || status.InstallError == "" || !status.UpdateAvailable || before.Command != after.Command {
				t.Fatal("Failed update lost the previous working server")
			}
			if !validRecord(language, m.tools.records[language]) {
				t.Fatal("Installed record cannot be restored")
			}
		})
	}
}

func TestCatalogUpdateChecksSkipUninstalledServersAndBoundConcurrency(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	if fixture.requests.Load() != 0 {
		t.Fatal("Opening settings fetched releases for uninstalled servers")
	}
	var active, peak atomic.Int32
	m.tools.client.Transport = toolTransport(func(request *http.Request) (*http.Response, error) {
		count := active.Add(1)
		for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
		}
		defer active.Add(-1)
		time.Sleep(10 * time.Millisecond)
		name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/"), "/latest")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"` + name + `","version":"1.0.1"}`)), Request: request}, nil
	})
	for _, language := range toolLanguages {
		if catalogByID[language].Install.Method == "npm" && language != "typescript" && language != "vue" {
			m.tools.records[language] = toolRecord{Version: "1.0.0"}
		}
	}
	if err := m.tools.checkUpdates(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 6 || peak.Load() < 2 {
		t.Fatalf("Unbounded or serial catalog requests: %d", peak.Load())
	}
}

func TestExternalServersRejectManagedInstallation(t *testing.T) {
	m := managedHarness(t, toolProvider(t))
	for _, definition := range serverCatalog {
		if definition.Install.Method == "external" {
			if err := m.InstallServer(context.Background(), definition.ID); err == nil {
				t.Fatalf("External SDK %s accepted a fake managed installation", definition.ID)
			}
		}
	}
}
