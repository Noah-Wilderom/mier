package lspclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var goToolVersion = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
var pythonToolVersion = regexp.MustCompile(`^(0|[1-9]\d*)(\.(0|[1-9]\d*)){1,3}$`)

func npmServerEntry(directory string, definition *serverDefinition) (string, error) {
	root := filepath.Join(directory, "node_modules", filepath.FromSlash(definition.Install.Package))
	var manifest struct {
		Name string          `json:"name"`
		Bin  json.RawMessage `json:"bin"`
	}
	if err := readToolJSON(filepath.Join(root, "package.json"), &manifest); err != nil || manifest.Name != definition.Install.Package {
		return "", errors.New("The installed npm package does not match the catalog")
	}
	var entry string
	if json.Unmarshal(manifest.Bin, &entry) != nil {
		var entries map[string]string
		if json.Unmarshal(manifest.Bin, &entries) != nil {
			return "", errors.New("The installed server does not declare an executable")
		}
		entry = entries[definition.Install.Binary]
	}
	if entry == "" || filepath.IsAbs(entry) || strings.ContainsAny(entry, "\x00\\") {
		return "", errors.New("The installed server declares an invalid executable")
	}
	absolute, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(entry)))
	canonical, rootErr := filepath.EvalSymlinks(root)
	if err != nil || rootErr != nil || !insideDirectory(canonical, absolute) || !existingFile(absolute) {
		return "", errors.New("The installed server executable is missing or outside its package")
	}
	return absolute, nil
}

func (t *toolInstaller) fetchEcosystemRelease(ctx context.Context, definition *serverDefinition) (toolRelease, error) {
	release := toolRelease{CheckedAt: time.Now().UTC()}
	if definition.Install.Method == "go" {
		var metadata struct{ Version string }
		if err := t.getJSON(ctx, "https://proxy.golang.org/"+definition.Install.Module+"/@latest", &metadata); err != nil {
			return release, err
		}
		release.Version = metadata.Version
	} else {
		var metadata struct {
			Info struct{ Name, Version string }
			URLs []struct {
				PackageType string `json:"packagetype"`
			}
		}
		if err := t.getJSON(ctx, "https://pypi.org/pypi/"+definition.Install.Package+"/json", &metadata); err != nil {
			return release, err
		}
		if strings.ReplaceAll(strings.ToLower(metadata.Info.Name), "_", "-") != definition.Install.Package {
			return release, errors.New("The Python package name does not match the catalog")
		}
		wheel := false
		for _, file := range metadata.URLs {
			wheel = wheel || file.PackageType == "bdist_wheel"
		}
		if !wheel {
			return release, errors.New("The Python provider has no prebuilt wheel for this server release")
		}
		release.Version, release.Packages = metadata.Info.Version, map[string]string{definition.Install.Package: metadata.Info.Version}
		for name, version := range definition.Install.Dependencies {
			release.Packages[name] = version
		}
	}
	if !validRelease(definition.ID, release) {
		return release, errors.New("The provider did not return a supported stable language-server release")
	}
	return release, nil
}

func (t *toolInstaller) detectEcosystemRuntime(ctx context.Context, kind string) (toolRuntime, error) {
	if kind == "go" {
		for _, candidate := range runtimeCandidates("go") {
			program, err := resolveProgram(candidate)
			if err != nil {
				continue
			}
			output, err := t.run(withToolEnvironment(ctx, map[string]string{"GOTOOLCHAIN": "local", "GOENV": "off"}), "", program, "version")
			fields := strings.Fields(string(output))
			if err == nil && len(fields) >= 3 {
				version := strings.TrimPrefix(fields[2], "go")
				if version == "1.24.0" || newerVersion(version, "1.24.0") {
					// Resolve version-manager shims before starting a server in a
					// captured repository containing its own toolchain settings.
					root, rootErr := t.run(withToolEnvironment(ctx, map[string]string{"GOTOOLCHAIN": "local", "GOENV": "off"}), "", program, "env", "GOROOT")
					if rootErr == nil {
						if actual, err := resolveProgram(filepath.Join(strings.TrimSpace(string(root)), "bin", "go")); err == nil {
							program = actual
						}
					}
					return toolRuntime{command: program}, nil
				}
			}
		}
		return toolRuntime{}, errors.New("Install Go 1.24 or newer, then check again; the selected server may require a newer Go toolchain")
	}
	candidates := append(runtimeCandidates("python3"), runtimeCandidates("python")...)
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "py")
	}
	for _, candidate := range candidates {
		program, err := resolveProgram(candidate)
		if err != nil {
			continue
		}
		output, err := t.run(ctx, "", program, "-c", `import json,sys,venv,ensurepip; print(json.dumps({'version':'.'.join(map(str,sys.version_info[:3])),'executable':sys.executable}))`)
		var metadata struct{ Version, Executable string }
		if err == nil && json.Unmarshal(output, &metadata) == nil && (metadata.Version == "3.10.0" || newerVersion(metadata.Version, "3.10.0")) {
			if actual, err := resolveProgram(metadata.Executable); err == nil {
				program = actual
			}
			return toolRuntime{command: program}, nil
		}
	}
	return toolRuntime{}, errors.New("Install Python 3.10 or newer with venv and ensurepip, then check again")
}

func toolBinary(directory, kind, name string) string {
	if runtime.GOOS == "windows" {
		if kind == "python" {
			return filepath.Join(directory, "venv", "Scripts", name+".exe")
		}
		return filepath.Join(directory, "bin", name+".exe")
	}
	if kind == "python" {
		return filepath.Join(directory, "venv", "bin", name)
	}
	return filepath.Join(directory, "bin", name)
}

func (t *toolInstaller) installEcosystem(ctx context.Context, language string, host toolRuntime, directory string, release toolRelease) error {
	definition := catalogByID[language]
	if definition.Install.Method == "go" {
		t.phase(language, "Building the pinned server with Go and verifying module checksums…")
		if err := os.MkdirAll(filepath.Join(directory, "bin"), 0700); err != nil {
			return err
		}
		env := map[string]string{"GOBIN": filepath.Join(directory, "bin"), "GOENV": "off", "GOWORK": "off", "GOFLAGS": "", "GOTOOLCHAIN": "local",
			"GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "CGO_ENABLED": "0",
			"GOCACHE": filepath.Join(t.directory, "go-build-cache"), "GOMODCACHE": filepath.Join(t.directory, "go-module-cache")}
		env["GOOS"], env["GOARCH"] = runtime.GOOS, runtime.GOARCH
		env["GOMAXPROCS"] = "2"
		if definition.Install.CGO {
			env["CGO_ENABLED"] = "1"
			if host.compiler == "" {
				return errors.New("Install GCC or Clang to build this server")
			}
			env["CC"] = host.compiler
		}
		if _, err := t.run(withToolEnvironment(ctx, env), directory, host.command, "install", "-p=2", "-trimpath", definition.Install.Package+"@"+release.Version); err != nil {
			return fmt.Errorf("Go could not build the server; check its toolchain requirement and your connection: %w", err)
		}
		// Go names the binary after the package, while some upstream launchers
		// (notably Helm) use a different executable name in editor configs.
		built := path.Base(definition.Install.Package)
		if built != definition.Install.Binary {
			if err := os.Rename(toolBinary(directory, "go", built), toolBinary(directory, "go", definition.Install.Binary)); err != nil {
				return fmt.Errorf("Locate the compiled server executable: %w", err)
			}
		}
	} else {
		t.phase(language, "Creating a private Python environment…")
		if _, err := t.run(ctx, directory, host.command, "-m", "venv", "--copies", filepath.Join(directory, "venv")); err != nil {
			return fmt.Errorf("Python could not create the server environment; install the venv/ensurepip components: %w", err)
		}
		t.phase(language, "Installing the pinned server from prebuilt Python wheels…")
		interpreter := toolBinary(directory, "python", "python")
		args := []string{"-m", "pip", "--isolated", "--disable-pip-version-check", "install", "--no-input", "--only-binary=:all:", "--index-url", "https://pypi.org/simple"}
		var packages []string
		for name, version := range release.Packages {
			packages = append(packages, name+"=="+version)
		}
		sort.Strings(packages)
		args = append(args, packages...)
		if _, err := t.run(ctx, directory, interpreter, args...); err != nil {
			return fmt.Errorf("Python could not install the server; check runtime compatibility and your connection: %w", err)
		}
	}
	if !existingFile(toolBinary(directory, definition.Install.Method, definition.Install.Binary)) {
		return errors.New("The installed package did not contain the catalog server executable")
	}
	return nil
}

type toolEnvironmentKey struct{}

func withToolEnvironment(ctx context.Context, env map[string]string) context.Context {
	return context.WithValue(ctx, toolEnvironmentKey{}, env)
}
