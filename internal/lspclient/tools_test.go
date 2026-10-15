package lspclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type toolTransport func(*http.Request) (*http.Response, error)

func (f toolTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type providerFixture struct {
	serverVersion atomic.Value
	vueVersion    atomic.Value
	phpVersion    atomic.Value
	corruptPHP    atomic.Bool
	fail          atomic.Bool
	requests      atomic.Int32
	client        *http.Client
}

func toolProvider(t *testing.T) *providerFixture {
	t.Helper()
	fixture := &providerFixture{}
	fixture.serverVersion.Store("6.0.1")
	fixture.vueVersion.Store("3.3.12")
	fixture.phpVersion.Store("2026.09.01.0")
	body := []byte("fixture PHAR bytes")
	digest := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture.requests.Add(1)
		if fixture.fail.Load() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/@latest") {
			_ = json.NewEncoder(writer).Encode(map[string]string{"Version": "v" + fixture.serverVersion.Load().(string)})
			return
		}
		if strings.HasPrefix(request.URL.Path, "/pypi/") {
			name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/pypi/"), "/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{"info": map[string]string{"name": name, "version": fixture.serverVersion.Load().(string)}, "urls": []any{map[string]string{"packagetype": "bdist_wheel"}}})
			return
		}
		if strings.HasSuffix(request.URL.Path, "/phpactor.phar") {
			if fixture.corruptPHP.Load() {
				_, _ = writer.Write([]byte("tampered PHAR data"))
			} else {
				_, _ = writer.Write(body)
			}
			return
		}
		if strings.Contains(request.URL.Path, "/releases/latest") {
			version := fixture.phpVersion.Load().(string)
			_ = json.NewEncoder(writer).Encode(map[string]any{"tag_name": version, "assets": []any{map[string]any{"name": "phpactor.phar", "size": len(body), "digest": "sha256:" + hex.EncodeToString(digest[:]), "browser_download_url": "https://github.com/phpactor/phpactor/releases/download/" + version + "/phpactor.phar"}}})
			return
		}
		name, version := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/"), "/latest"), fixture.serverVersion.Load().(string)
		if strings.Contains(request.URL.Path, "@vue/language-server") {
			name, version = "@vue/language-server", fixture.vueVersion.Load().(string)
		} else if strings.Contains(request.URL.Path, "@vue/typescript-plugin") {
			name, version = "@vue/typescript-plugin", fixture.vueVersion.Load().(string)
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{"name": name, "version": version})
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	fixture.client = &http.Client{Transport: toolTransport(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		copyURL := *request.URL
		copyURL.Scheme, copyURL.Host = target.Scheme, target.Host
		clone.URL = &copyURL
		response, err := http.DefaultTransport.RoundTrip(clone)
		if response != nil {
			response.Request = request
		}
		return response, err
	})}
	return fixture
}
func managedHarness(t *testing.T, fixture *providerFixture) *Manager {
	t.Helper()
	directory := t.TempDir()
	m := New(Options{ConfigPath: filepath.Join(directory, "settings", "language-servers.json"), CacheDirectory: filepath.Join(directory, "cache"), ToolsDirectory: filepath.Join(directory, "tools"), ToolHTTPClient: fixture.client})
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m.tools.prerequisite = func(context.Context, string) (toolRuntime, error) {
		return toolRuntime{command: program, npmCLI: filepath.Join(directory, "npm-cli.js"), compiler: program}, nil
	}
	m.tools.run = func(_ context.Context, working, _ string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "install" {
			var manifest struct{ Dependencies map[string]string }
			if err := readToolJSON(filepath.Join(working, "package.json"), &manifest); err != nil {
				return nil, err
			}
			required := map[string]bool{"--ignore-scripts": false, "--engine-strict": false, "--no-bin-links": false, "--registry=" + npmRegistry: false}
			for _, arg := range args {
				if _, ok := required[arg]; ok {
					required[arg] = true
				}
			}
			for arg, found := range required {
				if !found {
					return nil, fmt.Errorf("Missing install isolation flag %s", arg)
				}
			}
			if args[3] != working {
				return nil, errors.New("npm prefix is not the managed staging directory")
			}
			for name, version := range manifest.Dependencies {
				bins := map[string]string{}
				for _, definition := range serverCatalog {
					if definition.Install.Method == "npm" && definition.Install.Package == name {
						bins[definition.Install.Binary] = "bin/fixture.js"
						writeTestFile(t, filepath.Join(working, "node_modules", filepath.FromSlash(name), "bin", "fixture.js"), "fixture server")
					}
				}
				data, _ := json.Marshal(map[string]any{"name": name, "version": version, "bin": bins})
				writeTestFile(t, filepath.Join(working, "node_modules", filepath.FromSlash(name), "package.json"), string(data))
			}
			writeTestFile(t, filepath.Join(working, "node_modules", "typescript", "lib", "tsserver.js"), "fixture SDK")
			writeTestFile(t, filepath.Join(working, "node_modules", "typescript-language-server", "lib", "cli.mjs"), "fixture server")
			if _, vue := manifest.Dependencies["@vue/language-server"]; vue {
				writeTestFile(t, filepath.Join(working, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js"), "fixture Vue server")
			}
		}
		return []byte("fixture version"), nil
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func waitForTool(t *testing.T, m *Manager, language string) ToolStatus {
	t.Helper()
	done := make(chan struct{})
	go func() { m.tools.workers.Wait(); close(done) }()
	select {
	case <-done:
		return m.tools.statuses(context.Background())[language]
	case <-time.After(5 * time.Second):
		t.Fatal("Language tool installation did not finish")
		return ToolStatus{}
	}
}
func installFixture(t *testing.T, m *Manager, language string) ToolStatus {
	t.Helper()
	if err := m.InstallServer(context.Background(), language); err != nil {
		t.Fatal(err)
	}
	status := waitForTool(t, m, language)
	if status.InstallError != "" || !status.Ready {
		t.Fatalf("Installation not ready: %#v", status)
	}
	return status
}

func TestManagedToolsInstallUpdateAndRestorePersistentVersions(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	status := installFixture(t, m, "typescript")
	if status.InstalledVersion != "6.0.1" {
		t.Fatal(status)
	}
	before, _ := m.tools.configuration("typescript")
	fixture.serverVersion.Store("6.0.2")
	if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	status = m.tools.statuses(context.Background())["typescript"]
	if !status.UpdateAvailable || status.LatestVersion != "6.0.2" {
		t.Fatal(status)
	}
	installFixture(t, m, "typescript")
	after, _ := m.tools.configuration("typescript")
	if after.Args[0] == before.Args[0] || !existingFile(before.Args[0]) {
		t.Fatal("Update mutated or removed the previous installation")
	}
	if status = m.tools.statuses(context.Background())["typescript"]; status.UpdateAvailable || status.InstalledVersion != "6.0.2" {
		t.Fatal(status)
	}
	configPath, toolPath := m.options.ConfigPath, m.tools.directory
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restored := New(Options{ConfigPath: configPath, ToolsDirectory: toolPath, ToolHTTPClient: fixture.client})
	t.Cleanup(func() { _ = restored.Close() })
	loaded, ready := restored.tools.configuration("typescript")
	if !ready || loaded.Args[0] != after.Args[0] {
		t.Fatalf("Installed tools were not restored: %#v", loaded)
	}
}

func TestFailedUpdatePreservesTheActiveServerAndOffersRetry(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	var notifications atomic.Int32
	m.options.OnToolsChanged = func() { notifications.Add(1) }
	installFixture(t, m, "typescript")
	if notifications.Load() != 1 {
		t.Fatal("Successful activation did not notify code navigation")
	}
	before, _ := m.tools.configuration("typescript")
	fixture.serverVersion.Store("6.0.2")
	run := m.tools.run
	m.tools.run = func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("download failed")
	}
	if err := m.InstallServer(context.Background(), "typescript"); err != nil {
		t.Fatal(err)
	}
	status := waitForTool(t, m, "typescript")
	after, _ := m.tools.configuration("typescript")
	if after.Args[0] != before.Args[0] || status.InstalledVersion != "6.0.1" || !status.UpdateAvailable || status.InstallError == "" {
		t.Fatalf("Previous installation was lost: %#v", status)
	}
	if notifications.Load() != 1 {
		t.Fatal("A failed update emitted a successful activation event")
	}
	m.tools.run = run
	if status = installFixture(t, m, "typescript"); status.InstalledVersion != "6.0.2" {
		t.Fatal(status)
	}
}

func TestPhpactorUsesItsPublishedDigestAndNeverActivatesCorruptBytes(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	installFixture(t, m, "php")
	before, _ := m.tools.configuration("php")
	fixture.phpVersion.Store("2026.10.07.0")
	fixture.corruptPHP.Store(true)
	if err := m.InstallServer(context.Background(), "php"); err != nil {
		t.Fatal(err)
	}
	status := waitForTool(t, m, "php")
	after, _ := m.tools.configuration("php")
	if !strings.Contains(status.InstallError, "SHA-256") || after.Args[0] != before.Args[0] || status.InstalledVersion != "2026.09.01.0" {
		t.Fatalf("Corrupt download activated: %#v", status)
	}
	entries, err := os.ReadDir(m.tools.directory)
	if err != nil {
		t.Fatal(err)
	}
	installed := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "php-") {
			installed++
		}
	}
	if installed != 1 {
		t.Fatalf("Failed installation was not cleaned up: %d dirs", installed)
	}
}

func TestVueIncludesItsOwnCompatibleTypeScriptCompanionAndPlugin(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	installFixture(t, m, "vue")
	vue, ready := m.tools.configuration("vue")
	peer, peerReady := m.tools.configuration("vue-typescript")
	if !ready || !peerReady || vue.TypeScriptSDKPath != peer.TypeScriptSDKPath || vue.VuePluginPath == "" {
		t.Fatalf("Missing Vue dependencies: %#v / %#v", vue, peer)
	}
	record := m.tools.records["vue"]
	if record.Packages["@vue/typescript-plugin"] != record.Version || record.Packages["typescript"] != managedTypeScriptSDK {
		t.Fatal(record)
	}
	fixture.serverVersion.Store("6.0.2")
	if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	status := m.tools.statuses(context.Background())["vue"]
	if !status.UpdateAvailable || !status.DependencyUpdate || status.LatestVersion != status.InstalledVersion {
		t.Fatalf("Companion updates were hidden: %#v", status)
	}
	if err := os.Remove(peer.Args[0]); err != nil {
		t.Fatal(err)
	}
	if m.tools.statuses(context.Background())["vue"].Ready {
		t.Fatal("A missing Vue companion must offer reinstall")
	}
}

func TestToolCancellationRejectsDuplicateStartsAndRetainsTheOldInstallation(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	installFixture(t, m, "typescript")
	fixture.serverVersion.Store("6.0.2")
	started := make(chan struct{})
	m.tools.run = func(ctx context.Context, _ string, _ string, _ ...string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := m.InstallServer(context.Background(), "typescript"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Install did not start")
	}
	if err := m.InstallServer(context.Background(), "vue"); err == nil {
		t.Fatal("Concurrent installs were allowed")
	}
	if err := m.CancelServerInstall("typescript"); err != nil {
		t.Fatal(err)
	}
	status := waitForTool(t, m, "typescript")
	if status.State != "cancelled" || status.InstalledVersion != "6.0.1" || status.InstallError != "" {
		t.Fatalf("Cancellation lost the installation: %#v", status)
	}
}

func TestManagedToolsKeepCustomConfigurationUntilUserSelectsTheInstalledVersion(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	program, _ := os.Executable()
	custom := ServerConfiguration{Command: program, Args: []string{"custom-php-server"}}
	if err := m.Configure(context.Background(), "php", custom); err != nil {
		t.Fatal(err)
	}
	installFixture(t, m, "php")
	if status := m.detectServer("", "php"); status.Source != "configured" || status.Args[0] != "custom-php-server" {
		t.Fatalf("Custom configuration was overwritten: %#v", status)
	}
	if err := m.Configure(context.Background(), "php", ServerConfiguration{}); err != nil {
		t.Fatal(err)
	}
	if status := m.detectServer("", "php"); status.Source != "managed" {
		t.Fatalf("Automatic detection did not select the managed server: %#v", status)
	}
}

func TestCachedUpdateAvailabilitySurvivesOfflineChecks(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	installFixture(t, m, "typescript")
	fixture.serverVersion.Store("6.0.2")
	if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	fixture.fail.Store(true)
	if _, err := m.CheckServerUpdates(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	status := m.tools.statuses(context.Background())["typescript"]
	if !status.UpdateAvailable || status.CheckError == "" || status.InstalledVersion != "6.0.1" {
		t.Fatal(status)
	}
	requests := fixture.requests.Load()
	if _, err := m.CheckServerUpdates(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if fixture.requests.Load() != requests {
		t.Fatal("Fresh cached updates made redundant network requests")
	}
}

func TestInstallerRejectsInvalidPathsVersionsAndUnsafeProviderRedirects(t *testing.T) {
	for _, directory := range []string{"../typescript-1", "/typescript-1", `typescript-1\outside`, "php-1"} {
		if validRecord("typescript", toolRecord{Directory: directory, Version: "6.0.1", Packages: map[string]string{"typescript": "5.9.3", "typescript-language-server": "6.0.1"}}) {
			t.Fatalf("Unsafe manifest path accepted: %s", directory)
		}
	}
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	for _, endpoint := range []string{"http://github.com/file", "https://evil.example/file", "https://user:password@github.com/file"} {
		request, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if err := m.tools.client.CheckRedirect(request, nil); err == nil {
			t.Fatalf("Unsafe redirect accepted: %s", endpoint)
		}
	}
	fixture.serverVersion.Store("6.0.2-beta.1")
	if err := m.InstallServer(context.Background(), "typescript"); err != nil {
		t.Fatal(err)
	}
	if status := waitForTool(t, m, "typescript"); status.InstalledVersion != "" || status.InstallError == "" {
		t.Fatalf("Unstable provider version was installed: %#v", status)
	}
	for _, language := range []string{"shell", "../php", "typescript;echo bad"} {
		if err := m.InstallServer(context.Background(), language); err == nil {
			t.Fatalf("Unknown tool accepted: %s", language)
		}
	}
}

func TestMissingPrerequisitesAndShutdownDoNotPublishIncompleteInstallations(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	m.tools.prerequisite = func(context.Context, string) (toolRuntime, error) {
		return toolRuntime{}, errors.New("Node.js is missing")
	}
	if err := m.InstallServer(context.Background(), "typescript"); err != nil {
		t.Fatal(err)
	}
	status := waitForTool(t, m, "typescript")
	if status.Ready || status.InstalledVersion != "" || !strings.Contains(status.InstallError, "Node.js") {
		t.Fatal(status)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.InstallServer(context.Background(), "typescript"); err == nil {
		t.Fatal("A closed manager accepted an install")
	}
}

func TestInstallDoesNotReadOrModifyRepositoryDependencyFiles(t *testing.T) {
	fixture := toolProvider(t)
	m := managedHarness(t, fixture)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), "repository manifest")
	writeTestFile(t, filepath.Join(root, "package-lock.json"), "repository lockfile")
	writeTestFile(t, filepath.Join(root, ".npmrc"), "repository npm config")
	installFixture(t, m, "typescript")
	for _, name := range []string{"package.json", "package-lock.json", ".npmrc"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !strings.HasPrefix(string(data), "repository ") {
			t.Fatalf("Repository file was changed: %s", name)
		}
	}
}

func TestVersionComparisonDoesNotOfferDowngrades(t *testing.T) {
	for _, pair := range [][2]string{{"6.0.2", "6.0.1"}, {"2026.10.07.0", "2026.09.01.0"}, {"24.21.0", "22.22.2"}} {
		if !newerVersion(pair[0], pair[1]) || newerVersion(pair[1], pair[0]) {
			t.Fatal(pair)
		}
	}
}
