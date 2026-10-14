package lspclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type toolOutput struct {
	mu   sync.Mutex
	data []byte
}

func (output *toolOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	size := len(data)
	if remaining := 16*1024 - len(output.data); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		output.data = append(output.data, data...)
	}
	return size, nil
}
func runToolCommand(ctx context.Context, directory, program string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = directory
	// Installation uses its own npm configuration and never inherits project
	// loaders, registry credentials, or package-install configuration.
	for _, value := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(value, "=", 2)[0])
		if !strings.HasPrefix(key, "NPM_CONFIG_") && key != "NODE_OPTIONS" && key != "NODE_PATH" && key != "NPM_TOKEN" && key != "NODE_AUTH_TOKEN" {
			command.Env = append(command.Env, value)
		}
	}
	output, diagnostics := &toolOutput{}, &toolOutput{}
	command.Stdout, command.Stderr = output, diagnostics
	command.WaitDelay = 2 * time.Second
	cleanup := configureProcess(command)
	defer cleanup()
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		combined := append(append([]byte{}, output.data...), diagnostics.data...)
		if len(combined) > 0 {
			return output.data, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(combined)))
		}
	}
	return output.data, err
}

func busyTool(state string) bool {
	return state == "installing" || state == "updating" || state == "cancelling"
}
func (t *toolInstaller) start(ctx context.Context, language string) error {
	language, err := canonicalLanguage(language)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.ctx.Err() != nil {
		return errors.New("Language tool management has shut down")
	}
	if t.initErr != nil {
		return t.initErr
	}
	for _, state := range t.states {
		if busyTool(state.State) {
			return errors.New("A language server installation is already running")
		}
	}
	state := t.states[language]
	state.State, state.Progress, state.InstallError = "installing", "Checking prerequisites…", ""
	if t.records[language].Version != "" {
		state.State = "updating"
	}
	t.states[language] = state
	operation, cancel := context.WithTimeout(t.ctx, 5*time.Minute)
	t.cancels[language] = cancel
	t.workers.Add(1)
	go func() {
		defer t.workers.Done()
		defer cancel()
		err := t.install(operation, language)
		t.mu.Lock()
		defer t.mu.Unlock()
		state := t.states[language]
		if err == nil {
			state.State, state.Progress, state.InstallError = "idle", "Installation ready.", ""
		} else if errors.Is(err, context.Canceled) {
			state.State, state.Progress, state.InstallError = "cancelled", "Installation cancelled.", ""
		} else {
			state.State, state.Progress = "failed", ""
			if errors.Is(err, context.DeadlineExceeded) {
				state.InstallError = "Installation timed out. The previous installation is unchanged; try again."
			} else {
				state.InstallError = err.Error()
			}
		}
		t.states[language] = state
		delete(t.cancels, language)
	}()
	return nil
}
func (t *toolInstaller) phase(language, progress string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.states[language]
	if state.State != "cancelling" {
		state.Progress = progress
		t.states[language] = state
	}
}
func (t *toolInstaller) cancel(language string) error {
	language, err := canonicalLanguage(language)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cancel := t.cancels[language]
	if cancel != nil {
		state := t.states[language]
		state.State, state.Progress = "cancelling", "Cancelling installation…"
		t.states[language] = state
		cancel()
	}
	return nil
}
func (t *toolInstaller) close() {
	t.mu.Lock()
	t.closed = true
	for _, cancel := range t.cancels {
		cancel()
	}
	t.mu.Unlock()
	t.workers.Wait()
}

func (t *toolInstaller) install(ctx context.Context, language string) error {
	host, err := t.prerequisite(ctx, language)
	if err != nil {
		return err
	}
	t.phase(language, "Checking the latest stable release…")
	release, err := t.fetchRelease(ctx, language)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.releases[language] = release
	state := t.states[language]
	state.CheckError = ""
	t.states[language] = state
	old := t.records[language]
	t.mu.Unlock()
	if old.Version != "" && newerVersion(old.Version, release.Version) {
		return errors.New("The latest stable release is older than the installed version; automatic downgrades are disabled")
	}
	if err = os.MkdirAll(t.directory, 0700); err != nil {
		return fmt.Errorf("Create the M.I.E.R. language tools directory: %w", err)
	}
	directory, err := os.MkdirTemp(t.directory, language+"-")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(directory)
		}
	}()
	if language == "php" {
		t.phase(language, "Downloading and verifying Phpactor…")
		err = t.downloadPHAR(ctx, release, filepath.Join(directory, "phpactor.phar"))
		if err == nil {
			t.phase(language, "Checking Phpactor with your PHP runtime…")
			_, err = t.run(ctx, directory, host.command, filepath.Join(directory, "phpactor.phar"), "--version")
		}
	} else {
		t.phase(language, "Installing server packages and dependencies…")
		err = t.installNPM(ctx, host, directory, release)
		if err == nil {
			t.phase(language, "Verifying the installed server and TypeScript SDK…")
			err = verifyToolPackages(directory, release)
		}
		if err == nil {
			script := filepath.Join(directory, "node_modules", "typescript-language-server", "lib", "cli.mjs")
			_, err = t.run(ctx, directory, host.command, script, "--version")
			if err == nil && language == "vue" {
				_, err = t.run(ctx, directory, host.command, filepath.Join(directory, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js"), "--version")
			}
		}
	}
	if err != nil {
		return fmt.Errorf("Install %s: %w", languageNames[language], err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	t.phase(language, "Activating the installed server…")
	err = t.activate(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		next := map[string]toolRecord{}
		for key, record := range t.records {
			next[key] = record
		}
		next[language] = toolRecord{Directory: filepath.Base(directory), Version: release.Version, Packages: release.Packages, Previous: old.Directory, Runtime: host.command}
		if err := writeToolJSON(filepath.Join(t.directory, "installed.json"), struct {
			Version int                   `json:"version"`
			Tools   map[string]toolRecord `json:"tools"`
		}{1, next}); err != nil {
			return fmt.Errorf("Activate the language server: %w", err)
		}
		t.records = next
		committed = true
		return nil
	})
	return err
}

func (t *toolInstaller) downloadPHAR(ctx context.Context, release toolRelease, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "MIER-language-tools")
	response, err := t.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Could not download Phpactor; check your connection and try again")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Phpactor download returned HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, release.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != release.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != release.Digest {
		return errors.New("Phpactor download failed its published SHA-256 or size check; the previous installation is unchanged")
	}
	return nil
}
func (t *toolInstaller) installNPM(ctx context.Context, host toolRuntime, directory string, release toolRelease) error {
	data, err := json.Marshal(map[string]any{"name": "mier-managed-language-tools", "version": "0.0.0", "private": true, "dependencies": release.Packages})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "package.json"), data, 0600); err != nil {
		return err
	}
	userConfig, globalConfig := filepath.Join(directory, "npm-user.npmrc"), filepath.Join(directory, "npm-global.npmrc")
	for _, path := range []string{userConfig, globalConfig} {
		if err = os.WriteFile(path, []byte{}, 0600); err != nil {
			return err
		}
	}
	_, err = t.run(ctx, directory, host.command, host.npmCLI, "install", "--prefix", directory, "--ignore-scripts", "--engine-strict", "--no-audit", "--no-fund", "--no-bin-links", "--package-lock", "--registry="+npmRegistry, "--userconfig", userConfig, "--globalconfig", globalConfig, "--cache", filepath.Join(t.directory, "npm-cache"))
	if err != nil {
		return fmt.Errorf("npm could not install the server packages. Check Node.js and your connection: %w", err)
	}
	return nil
}
func verifyToolPackages(directory string, release toolRelease) error {
	for name, version := range release.Packages {
		var manifest struct{ Name, Version string }
		if err := readToolJSON(filepath.Join(directory, "node_modules", filepath.FromSlash(name), "package.json"), &manifest); err != nil || manifest.Name != name || manifest.Version != version {
			return fmt.Errorf("Installed npm package %s does not match the requested version", name)
		}
	}
	for _, path := range []string{filepath.Join("typescript", "lib", "tsserver.js"), filepath.Join("typescript-language-server", "lib", "cli.mjs")} {
		if !existingFile(filepath.Join(directory, "node_modules", path)) {
			return errors.New("Installed packages do not contain the required TypeScript server or SDK")
		}
	}
	if _, vue := release.Packages["@vue/language-server"]; vue && !existingFile(filepath.Join(directory, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js")) {
		return errors.New("Installed packages do not contain the Vue language server")
	}
	return nil
}
