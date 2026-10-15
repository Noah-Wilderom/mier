package lspclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The SDK is the combination exercised by the real TypeScript and Vue tests.
// Updating server packages must not silently migrate to a native TypeScript SDK
// that no longer exposes the tsserver.js protocol used by these servers.
const managedTypeScriptSDK = "5.9.3"
const npmRegistry = "https://registry.npmjs.org"

var toolLanguages = managedLanguages()
var npmVersion = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
var phpactorVersion = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}\.\d{1,4}$`)
var toolDirectoryName = regexp.MustCompile(`^[a-z][a-z0-9-]*-[0-9]+$`)

type toolRelease struct {
	Version   string            `json:"version"`
	Packages  map[string]string `json:"packages,omitempty"`
	URL       string            `json:"url,omitempty"`
	Digest    string            `json:"digest,omitempty"`
	Size      int64             `json:"size,omitempty"`
	CheckedAt time.Time         `json:"checkedAt"`
}
type toolRecord struct {
	Directory string            `json:"directory"`
	Version   string            `json:"version"`
	Packages  map[string]string `json:"packages,omitempty"`
	Previous  string            `json:"previous,omitempty"`
	Runtime   string            `json:"runtime"`
}
type toolRuntime struct {
	command  string
	npmCLI   string
	compiler string
}
type runtimeResult struct {
	runtime toolRuntime
	err     error
	at      time.Time
}
type toolInstaller struct {
	ctx          context.Context
	directory    string
	client       *http.Client
	mu           sync.Mutex
	runtimeMu    sync.Mutex
	checkGate    chan struct{}
	records      map[string]toolRecord
	releases     map[string]toolRelease
	states       map[string]ToolStatus
	runtimes     map[string]runtimeResult
	cancels      map[string]context.CancelFunc
	initErr      error
	closed       bool
	workers      sync.WaitGroup
	run          func(context.Context, string, string, ...string) ([]byte, error)
	prerequisite func(context.Context, string) (toolRuntime, error)
	activate     func(context.Context, func() error) error
}

func newToolInstaller(ctx context.Context, directory string, client *http.Client, activate func(context.Context, func() error) error) *toolInstaller {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		host := strings.ToLower(request.URL.Hostname())
		allowed := host == "api.github.com" || host == "github.com" || host == "registry.npmjs.org" || host == "pypi.org" || host == "proxy.golang.org" || host == "release-assets.githubusercontent.com" || host == "objects.githubusercontent.com" || host == "github-releases.githubusercontent.com"
		if len(via) >= 5 || request.URL.Scheme != "https" || request.URL.User != nil || !allowed {
			return errors.New("Language tool downloads must stay on their official HTTPS providers")
		}
		return nil
	}
	client = &copyClient
	t := &toolInstaller{ctx: ctx, directory: directory, client: client, checkGate: make(chan struct{}, 1), records: map[string]toolRecord{}, releases: map[string]toolRelease{}, states: map[string]ToolStatus{}, runtimes: map[string]runtimeResult{}, cancels: map[string]context.CancelFunc{}, activate: activate}
	t.checkGate <- struct{}{}
	t.run = runToolCommand
	t.prerequisite = t.detectRuntime
	var installed struct {
		Version int                   `json:"version"`
		Tools   map[string]toolRecord `json:"tools"`
	}
	if directory == "" {
		t.initErr = errors.New("The OS configuration directory is unavailable")
	} else if err := readToolJSON(filepath.Join(directory, "installed.json"), &installed); err != nil && !os.IsNotExist(err) {
		t.initErr = fmt.Errorf("Read installed language tools: %w", err)
	} else if err == nil {
		if installed.Version != 1 {
			t.initErr = errors.New("Installed language tools have an unsupported manifest version")
		} else {
			for language, record := range installed.Tools {
				if _, err := canonicalLanguage(language); err != nil || !validRecord(language, record) {
					t.initErr = errors.New("Installed language tools contain an invalid path or version")
					break
				}
				t.records[language] = record
			}
		}
	}
	var cached struct {
		Version int                    `json:"version"`
		Tools   map[string]toolRelease `json:"tools"`
	}
	if readToolJSON(filepath.Join(directory, "latest.json"), &cached) == nil && cached.Version == 1 {
		for language, release := range cached.Tools {
			if validRelease(language, release) {
				t.releases[language] = release
			}
		}
	}
	return t
}

func readToolJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("Language tool metadata exceeds the size limit")
	}
	return json.Unmarshal(data, value)
}

func writeToolJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".metadata-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func validRecord(language string, record toolRecord) bool {
	definition := catalogByID[language]
	if definition == nil || definition.Install.Method == "external" {
		return false
	}
	if !filepath.IsAbs(record.Runtime) || len(record.Runtime) > 8192 || strings.ContainsRune(record.Runtime, 0) {
		return false
	}
	if !toolDirectoryName.MatchString(record.Directory) || !strings.HasPrefix(record.Directory, language+"-") {
		return false
	}
	if record.Previous != "" && (!toolDirectoryName.MatchString(record.Previous) || !strings.HasPrefix(record.Previous, language+"-")) {
		return false
	}
	if language == "php" {
		return phpactorVersion.MatchString(record.Version)
	}
	if definition.Install.Method == "go" {
		return goToolVersion.MatchString(record.Version) && len(record.Packages) == 0
	}
	if definition.Install.Method == "python" {
		return validPythonPackages(definition, record.Version, record.Packages)
	}
	return validPackages(language, record.Version, record.Packages)
}
func validPackages(language, version string, packages map[string]string) bool {
	main := "typescript-language-server"
	if language == "vue" {
		main = "@vue/language-server"
	} else if language != "typescript" {
		definition := catalogByID[language]
		if definition == nil || definition.Install.Method != "npm" {
			return false
		}
		expected := 1 + len(definition.Install.Dependencies)
		if usesTypeScriptSDK(language) {
			expected++
		}
		for dependency := range definition.Install.Dependencies {
			if !npmVersion.MatchString(packages[dependency]) {
				return false
			}
		}
		return len(packages) == expected && npmVersion.MatchString(version) && packages[definition.Install.Package] == version && (!usesTypeScriptSDK(language) || npmVersion.MatchString(packages["typescript"]))
	}
	expected := 2
	if language == "vue" {
		expected = 4
	}
	if len(packages) != expected || !npmVersion.MatchString(version) || packages[main] != version || !npmVersion.MatchString(packages["typescript-language-server"]) || !npmVersion.MatchString(packages["typescript"]) {
		return false
	}
	return language != "vue" || packages["@vue/typescript-plugin"] == version
}
func validRelease(language string, release toolRelease) bool {
	if language == "php" {
		expected := "https://github.com/phpactor/phpactor/releases/download/" + release.Version + "/phpactor.phar"
		return phpactorVersion.MatchString(release.Version) && release.URL == expected && regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(release.Digest) && release.Size > 0 && release.Size <= 64*1024*1024
	}
	definition := catalogByID[language]
	if definition == nil || definition.Install.Method == "external" {
		return false
	}
	if definition.Install.Method == "go" {
		return goToolVersion.MatchString(release.Version) && len(release.Packages) == 0
	}
	if definition.Install.Method == "python" {
		return validPythonPackages(definition, release.Version, release.Packages) && validPinnedDependencies(definition, release.Packages)
	}
	return validPackages(language, release.Version, release.Packages) && validPinnedDependencies(definition, release.Packages) && (!usesTypeScriptSDK(language) || release.Packages["typescript"] == managedTypeScriptSDK)
}

func validPinnedDependencies(definition *serverDefinition, packages map[string]string) bool {
	for name, version := range definition.Install.Dependencies {
		if packages[name] != version {
			return false
		}
	}
	return true
}

func validPythonPackages(definition *serverDefinition, version string, packages map[string]string) bool {
	if !pythonToolVersion.MatchString(version) || packages[definition.Install.Package] != version || len(packages) != 1+len(definition.Install.Dependencies) {
		return false
	}
	for name := range definition.Install.Dependencies {
		if !pythonToolVersion.MatchString(packages[name]) {
			return false
		}
	}
	return true
}

func newerVersion(candidate, installed string) bool {
	if installed == "" {
		return false
	}
	a, b := strings.Split(strings.TrimPrefix(candidate, "v"), "."), strings.Split(strings.TrimPrefix(installed, "v"), ".")
	for len(a) < len(b) {
		a = append(a, "0")
	}
	for len(b) < len(a) {
		b = append(b, "0")
	}
	for index := range a {
		left, leftErr := strconv.ParseUint(a[index], 10, 32)
		right, rightErr := strconv.ParseUint(b[index], 10, 32)
		if leftErr != nil || rightErr != nil {
			return false
		}
		if left != right {
			return left > right
		}
	}
	return false
}

func (t *toolInstaller) getJSON(ctx context.Context, endpoint string, value any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "MIER-language-tools")
	response, err := t.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Could not reach the language tool provider; check your connection and try again")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("The language tool provider returned HTTP %d; try again later", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil {
		return err
	}
	if len(body) > 4*1024*1024 {
		return errors.New("Language tool provider metadata exceeds the size limit")
	}
	if err = json.Unmarshal(body, value); err != nil {
		return errors.New("The language tool provider returned invalid metadata")
	}
	return nil
}
func (t *toolInstaller) npmPackage(ctx context.Context, name, version string) (string, error) {
	var pkg struct{ Name, Version string }
	if err := t.getJSON(ctx, npmRegistry+"/"+url.PathEscape(name)+"/"+version, &pkg); err != nil {
		return "", err
	}
	if pkg.Name != name || !npmVersion.MatchString(pkg.Version) || (version != "latest" && pkg.Version != version) {
		return "", errors.New("The npm package name or stable version does not match the requested tool")
	}
	return pkg.Version, nil
}
func (t *toolInstaller) fetchRelease(ctx context.Context, language string) (toolRelease, error) {
	release := toolRelease{CheckedAt: time.Now().UTC()}
	definition := catalogByID[language]
	if definition == nil || definition.Install.Method == "external" {
		return release, errors.New("This server is installed with its SDK or upstream setup instructions")
	}
	if definition.Install.Method == "go" || definition.Install.Method == "python" {
		return t.fetchEcosystemRelease(ctx, definition)
	}
	if language == "php" {
		var upstream struct {
			Tag               string `json:"tag_name"`
			Draft, Prerelease bool
			Assets            []struct {
				Name, Digest string
				Size         int64
				URL          string `json:"browser_download_url"`
			}
		}
		if err := t.getJSON(ctx, "https://api.github.com/repos/phpactor/phpactor/releases/latest", &upstream); err != nil {
			return release, err
		}
		if upstream.Draft || upstream.Prerelease {
			return release, errors.New("Phpactor did not return a stable release")
		}
		for _, asset := range upstream.Assets {
			if asset.Name == "phpactor.phar" {
				release.Version, release.URL, release.Digest, release.Size = upstream.Tag, asset.URL, asset.Digest, asset.Size
				break
			}
		}
	} else if language != "typescript" && language != "vue" {
		version, err := t.npmPackage(ctx, definition.Install.Package, "latest")
		if err != nil {
			return release, err
		}
		release.Version, release.Packages = version, map[string]string{definition.Install.Package: version}
		for name, version := range definition.Install.Dependencies {
			release.Packages[name] = version
		}
		if usesTypeScriptSDK(language) {
			release.Packages["typescript"] = managedTypeScriptSDK
		}
	} else {
		server, err := t.npmPackage(ctx, "typescript-language-server", "latest")
		if err != nil {
			return release, err
		}
		release.Version = server
		release.Packages = map[string]string{"typescript-language-server": server, "typescript": managedTypeScriptSDK}
		if language == "vue" {
			vue, err := t.npmPackage(ctx, "@vue/language-server", "latest")
			if err != nil {
				return release, err
			}
			if _, err = t.npmPackage(ctx, "@vue/typescript-plugin", vue); err != nil {
				return release, fmt.Errorf("A matching Vue TypeScript plugin is not available: %w", err)
			}
			release.Version = vue
			release.Packages["@vue/language-server"], release.Packages["@vue/typescript-plugin"] = vue, vue
		}
	}
	if !validRelease(language, release) {
		return release, errors.New("The provider did not return a supported stable release with verifiable download metadata")
	}
	return release, nil
}

func (t *toolInstaller) checkUpdates(ctx context.Context, force bool) error {
	if force {
		t.runtimeMu.Lock()
		t.runtimes = map[string]runtimeResult{}
		t.runtimeMu.Unlock()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.ctx.Done():
		return errors.New("Language tool management has shut down")
	case <-t.checkGate:
	}
	defer func() { t.checkGate <- struct{}{} }()
	var workers sync.WaitGroup
	parallel := make(chan struct{}, 6)
	for _, language := range toolLanguages {
		t.mu.Lock()
		last := t.releases[language]
		installed := t.records[language].Version != ""
		t.mu.Unlock()
		if !installed {
			continue
		}
		if !force && !last.CheckedAt.IsZero() && time.Since(last.CheckedAt) < 6*time.Hour {
			continue
		}
		workers.Add(1)
		go func(language string) {
			defer workers.Done()
			select {
			case parallel <- struct{}{}:
				defer func() { <-parallel }()
			case <-ctx.Done():
				return
			}
			release, err := t.fetchRelease(ctx, language)
			t.mu.Lock()
			defer t.mu.Unlock()
			state := t.states[language]
			state.CheckError = ""
			if err != nil {
				state.CheckError = err.Error()
			} else {
				t.releases[language] = release
			}
			t.states[language] = state
		}(language)
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.directory != "" {
		if err := writeToolJSON(filepath.Join(t.directory, "latest.json"), struct {
			Version int                    `json:"version"`
			Tools   map[string]toolRelease `json:"tools"`
		}{1, t.releases}); err != nil {
			return fmt.Errorf("Save language tool update metadata: %w", err)
		}
	}
	return nil
}

func (t *toolInstaller) runtimeFor(ctx context.Context, language string) (toolRuntime, error) {
	key := "node"
	if definition := catalogByID[language]; definition != nil && definition.Install.Method != "npm" {
		key = definition.Install.Method
		if definition.Install.CGO {
			key += "-cgo"
		}
	}
	t.runtimeMu.Lock()
	defer t.runtimeMu.Unlock()
	if cached, ok := t.runtimes[key]; ok && time.Since(cached.at) < 30*time.Second {
		return cached.runtime, cached.err
	}
	value, err := t.prerequisite(ctx, language)
	if ctx.Err() == nil {
		t.runtimes[key] = runtimeResult{value, err, time.Now()}
	}
	return value, err
}
func (t *toolInstaller) detectRuntime(ctx context.Context, language string) (toolRuntime, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if definition := catalogByID[language]; definition != nil && (definition.Install.Method == "go" || definition.Install.Method == "python") {
		var compiler string
		if definition.Install.CGO {
			var err error
			compiler, err = resolveProgram("gcc")
			if err != nil {
				compiler, err = resolveProgram("clang")
			}
			if err != nil {
				return toolRuntime{}, errors.New("This server also needs a C compiler for Go's cgo support; install GCC or Clang and check again")
			}
		}
		host, err := t.detectEcosystemRuntime(ctx, definition.Install.Method)
		host.compiler = compiler
		return host, err
	}
	if language == "php" {
		if runtime.GOOS == "windows" {
			return toolRuntime{}, errors.New("Managed Phpactor needs PHP's POSIX extension, which is unavailable in native Windows PHP. TypeScript and Vue installation remain supported.")
		}
		for _, candidate := range runtimeCandidates("php") {
			program, err := resolveProgram(candidate)
			if err != nil {
				continue
			}
			output, err := t.run(ctx, "", program, "-r", `echo json_encode(['version'=>PHP_VERSION,'executable'=>PHP_BINARY,'missing'=>array_values(array_filter(['mbstring','tokenizer','posix'],fn($ext)=>!extension_loaded($ext)))]);`)
			var result struct {
				Version, Executable string
				Missing             []string
			}
			if err != nil || json.Unmarshal(output, &result) != nil || !(result.Version == "8.2.0" || newerVersion(strings.Split(result.Version, "-")[0], "8.2.0")) || len(result.Missing) != 0 {
				continue
			}
			if actual, err := resolveProgram(result.Executable); err == nil {
				program = actual
			}
			return toolRuntime{command: program}, nil
		}
		return toolRuntime{}, errors.New("Install PHP 8.2 or newer with mbstring, tokenizer, and posix, then check again")
	}
	program := ""
	for _, candidate := range runtimeCandidates("node") {
		executable, err := resolveProgram(candidate)
		if err != nil {
			continue
		}
		output, err := t.run(ctx, "", executable, "-p", "JSON.stringify({version:process.versions.node,executable:process.execPath})")
		var result struct{ Version, Executable string }
		if err != nil || json.Unmarshal(output, &result) != nil || !npmVersion.MatchString(result.Version) || !(result.Version == "22.22.2" || newerVersion(result.Version, "22.22.2")) {
			continue
		}
		if actual, err := resolveProgram(result.Executable); err == nil {
			program = actual
			break
		}
	}
	if program == "" {
		return toolRuntime{}, errors.New("Install Node.js 22.22.2 or newer and npm, then check again")
	}
	nodePath := program
	if resolved, err := filepath.EvalSymlinks(program); err == nil {
		nodePath = resolved
	}
	candidates := []string{}
	if npm, err := exec.LookPath("npm"); err == nil {
		if resolved, err := filepath.EvalSymlinks(npm); err == nil {
			candidates = append(candidates, resolved)
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(npm), "node_modules", "npm", "bin", "npm-cli.js"))
	}
	for _, directory := range []string{filepath.Dir(program), filepath.Dir(nodePath)} {
		candidates = append(candidates, filepath.Join(directory, "node_modules", "npm", "bin", "npm-cli.js"), filepath.Join(directory, "..", "lib", "node_modules", "npm", "bin", "npm-cli.js"), filepath.Join(directory, "..", "share", "nodejs", "npm", "bin", "npm-cli.js"))
	}
	for _, candidate := range candidates {
		if filepath.Base(candidate) == "npm-cli.js" && existingFile(candidate) {
			absolute, err := filepath.Abs(candidate)
			if err == nil {
				return toolRuntime{command: program, npmCLI: absolute}, nil
			}
		}
	}
	if runtime.GOOS == "windows" {
		return toolRuntime{}, errors.New("npm's CLI script was not found beside Node.js; install Node.js with npm and check again")
	}
	return toolRuntime{}, errors.New("npm's CLI script was not found; install Node.js with npm and check again")
}

func runtimeCandidates(name string) []string {
	result := []string{name}
	if runtime.GOOS == "darwin" {
		result = append(result, filepath.Join("/opt/homebrew/bin", name), filepath.Join("/usr/local/bin", name))
	}
	if runtime.GOOS == "windows" && name == "node" {
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root != "" {
				result = append(result, filepath.Join(root, "nodejs", "node.exe"))
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}
	mise := filepath.Join(home, ".local", "share", "mise")
	if directory := os.Getenv("MISE_DATA_DIR"); filepath.IsAbs(directory) {
		mise = directory
	}
	versions, _ := filepath.Glob(filepath.Join(mise, "installs", name, "*"))
	sort.Slice(versions, func(i, j int) bool { return newerVersion(filepath.Base(versions[i]), filepath.Base(versions[j])) })
	for _, directory := range versions {
		if runtime.GOOS == "windows" {
			result = append(result, filepath.Join(directory, name+".exe"))
		} else {
			result = append(result, filepath.Join(directory, "bin", name))
		}
	}
	return result
}

func (t *toolInstaller) configuration(language string) (ServerConfiguration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.configurationLocked(language)
}
func (t *toolInstaller) configurationLocked(language string) (ServerConfiguration, bool) {
	key := language
	if language == "vue-typescript" {
		key = "vue"
	}
	record, ok := t.records[key]
	if !ok || t.initErr != nil {
		return ServerConfiguration{}, false
	}
	directory := filepath.Join(t.directory, record.Directory)
	configuration := ServerConfiguration{Command: record.Runtime, Args: []string{}, TypeScriptSDKPath: filepath.Join(directory, "node_modules", "typescript", "lib")}
	switch language {
	case "php":
		configuration = ServerConfiguration{Command: record.Runtime, Args: []string{filepath.Join(directory, "phpactor.phar"), "language-server"}}
		if !existingFile(configuration.Args[0]) {
			return ServerConfiguration{}, false
		}
	case "typescript", "vue-typescript":
		configuration.Args = []string{filepath.Join(directory, "node_modules", "typescript-language-server", "lib", "cli.mjs"), "--stdio"}
	case "vue":
		configuration.Args = []string{filepath.Join(directory, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js"), "--stdio"}
		configuration.VuePluginPath = filepath.Join(directory, "node_modules", "@vue", "typescript-plugin")
		if !existingFile(filepath.Join(configuration.VuePluginPath, "package.json")) || !existingFile(filepath.Join(directory, "node_modules", "typescript-language-server", "lib", "cli.mjs")) {
			return ServerConfiguration{}, false
		}
	default:
		definition := catalogByID[language]
		if definition == nil {
			return ServerConfiguration{}, false
		}
		if !usesTypeScriptSDK(language) {
			configuration.TypeScriptSDKPath = ""
		}
		configuration.Args = append([]string{}, definition.Args...)
		switch definition.Install.Method {
		case "npm":
			entry, err := npmServerEntry(directory, definition)
			if err != nil {
				return ServerConfiguration{}, false
			}
			configuration.Args = append([]string{entry}, configuration.Args...)
			return configuration, existingFile(record.Runtime) && (!usesTypeScriptSDK(language) || existingFile(filepath.Join(configuration.TypeScriptSDKPath, "tsserver.js")))
		case "go":
			configuration.Command = toolBinary(directory, "go", definition.Install.Binary)
		case "python":
			configuration.Command = toolBinary(directory, "python", definition.Install.Binary)
		default:
			return ServerConfiguration{}, false
		}
		return configuration, existingFile(configuration.Command)
	}
	return configuration, existingFile(record.Runtime) && existingFile(configuration.Args[0]) && (language == "php" || existingFile(filepath.Join(configuration.TypeScriptSDKPath, "tsserver.js")))
}
func (t *toolInstaller) statuses(ctx context.Context) map[string]ToolStatus {
	result := map[string]ToolStatus{}
	for _, language := range toolLanguages {
		_, prerequisiteErr := t.runtimeFor(ctx, language)
		t.mu.Lock()
		status := t.states[language]
		if status.State == "" {
			status.State = "idle"
		}
		record, release := t.records[language], t.releases[language]
		status.InstalledVersion, status.LatestVersion = record.Version, release.Version
		_, status.Ready = t.configurationLocked(language)
		status.UpdateAvailable = newerVersion(release.Version, record.Version)
		for name, version := range release.Packages {
			if record.Version != "" && newerVersion(version, record.Packages[name]) {
				status.UpdateAvailable = true
				if release.Version == record.Version {
					status.DependencyUpdate = true
				}
			}
		}
		if !release.CheckedAt.IsZero() {
			status.CheckedAt = release.CheckedAt.Format(time.RFC3339)
		}
		if prerequisiteErr != nil {
			status.PrerequisiteError = prerequisiteErr.Error()
		}
		if t.initErr != nil {
			status.InstallError = t.initErr.Error()
		} else if record.Version != "" && !status.Ready && status.InstallError == "" {
			status.InstallError = "The M.I.E.R. installation is incomplete. Reinstall this server to repair it."
		}
		result[language] = status
		t.mu.Unlock()
	}
	return result
}
