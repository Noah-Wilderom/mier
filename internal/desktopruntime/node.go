// Package desktopruntime finds user-installed runtimes when a desktop launcher
// supplies a smaller PATH than the user's terminal.
package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

type Node struct {
	Executable string
	Version    string
	NPMCLI     string
}

type environment struct {
	home, platform string
	values         map[string]string
}

func currentEnvironment() environment {
	home, _ := os.UserHomeDir()
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	return environment{home, runtime.GOOS, values}
}

func (e environment) get(key string) string {
	if e.platform == "windows" {
		key = strings.ToUpper(key)
	}
	return e.values[key]
}

type nodeResult struct {
	node Node
	err  error
	at   time.Time
}

type nodeResolver struct {
	mu       sync.Mutex
	cache    map[string]nodeResult
	revision uint64
}

var defaultResolver nodeResolver

// RefreshNode makes an explicit "check again" re-read manager selections.
func RefreshNode() {
	defaultResolver.mu.Lock()
	defer defaultResolver.mu.Unlock()
	clear(defaultResolver.cache)
	defaultResolver.revision++
}

// FindNode checks real executables, not just the existence of a shell shim.
// Repeated catalog and startup requests share a short cache; installing a
// runtime or changing a manager's default is picked up within thirty seconds.
func FindNode(ctx context.Context, minimumVersion string, requireNPM bool) (Node, error) {
	return defaultResolver.find(ctx, currentEnvironment(), minimumVersion, requireNPM)
}

func (r *nodeResolver) find(ctx context.Context, env environment, minimum string, requireNPM bool) (Node, error) {
	if err := ctx.Err(); err != nil {
		return Node{}, err
	}
	key := env.home + "\x00" + env.platform + "\x00" + minimum
	if requireNPM {
		key += "\x00npm"
	}
	for _, name := range []string{"PATH", "PATHEXT", "NVM_BIN", "NVM_DIR", "NVM_HOME", "NVM_SYMLINK", "MISE_BIN", "MISE_DATA_DIR", "MISE_CONFIG_DIR", "MISE_GLOBAL_CONFIG_FILE", "MISE_ENV", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
		key += "\x00" + env.get(name)
	}
	r.mu.Lock()
	cached, ok := r.cache[key]
	revision := r.revision
	r.mu.Unlock()
	ttl := 30 * time.Second
	if cached.err != nil {
		ttl = 5 * time.Second
	}
	if ok && time.Since(cached.at) < ttl && (cached.err != nil || executable(cached.node.Executable, env.platform) && (!requireNPM || regularFile(cached.node.NPMCLI))) {
		return cached.node, cached.err
	}
	lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	node, err := env.findNode(lookup, minimum, requireNPM)
	if ctx.Err() == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.revision != revision {
			return node, err
		}
		if len(r.cache) >= 8 {
			clear(r.cache)
		}
		if r.cache == nil {
			r.cache = map[string]nodeResult{}
		}
		r.cache[key] = nodeResult{node, err, time.Now()}
	}
	return node, err
}

func (e environment) findNode(ctx context.Context, minimum string, requireNPM bool) (Node, error) {
	seen := map[string]bool{}
	foundNode := false
	try := func(candidates []string) (Node, bool) {
		for _, candidate := range candidates {
			if ctx.Err() != nil {
				break
			}
			if seen[candidate] || !executable(candidate, e.platform) {
				continue
			}
			seen[candidate] = true
			output, err := e.query(ctx, candidate, "-p", "JSON.stringify({version:process.versions.node,executable:process.execPath})")
			var value struct{ Version, Executable string }
			if err != nil || json.Unmarshal(output, &value) != nil || !semver.IsValid("v"+value.Version) || minimum != "" && semver.Compare("v"+value.Version, "v"+minimum) < 0 || !executable(value.Executable, e.platform) {
				continue
			}
			actual, err := filepath.EvalSymlinks(value.Executable)
			if err != nil {
				continue
			}
			node := Node{Executable: actual, Version: value.Version}
			foundNode = true
			if requireNPM {
				node.NPMCLI = e.npmCLI(actual)
				if node.NPMCLI == "" {
					continue
				}
			}
			return node, true
		}
		return Node{}, false
	}
	// An inherited terminal selection always wins. NVM_BIN and NVM_SYMLINK
	// remain useful when PATH was stripped by a launcher.
	active := []string{e.lookPath("node")}
	for _, name := range []string{"NVM_BIN", "NVM_SYMLINK"} {
		if directory := e.get(name); filepath.IsAbs(directory) {
			active = append(active, e.nodeIn(directory))
		}
	}
	if node, ok := try(active); ok {
		return node, nil
	}
	// `mise which` is a read-only lookup of the user's selection. Run from
	// home, never a reviewed repository; do not source shell startup files.
	for _, mise := range e.misePrograms() {
		if !executable(mise, e.platform) {
			continue
		}
		output, err := e.query(ctx, mise, "which", "node")
		if err == nil {
			if node, ok := try([]string{strings.TrimSpace(string(output))}); ok {
				return node, nil
			}
		}
		break
	}
	if node, ok := try(e.nodeCandidates()); ok {
		return node, nil
	}
	if err := ctx.Err(); err != nil {
		return Node{}, err
	}
	if foundNode && requireNPM {
		return Node{}, errors.New("npm's CLI script was not found; install Node.js with npm and check again")
	}
	if minimum != "" {
		return Node{}, errors.New("Install Node.js " + minimum + " or newer and npm, then check again")
	}
	return Node{}, errors.New("Node.js was not found in PATH or your installed mise/nvm runtimes")
}

func (e environment) npmCLI(node string) string {
	directory := filepath.Dir(node)
	// Prefer npm belonging to this Node installation over an unrelated npm
	// in PATH, including Windows' node_modules/npm/bin layout.
	candidates := []string{
		filepath.Join(directory, "node_modules", "npm", "bin", "npm-cli.js"),
		filepath.Join(directory, "..", "lib", "node_modules", "npm", "bin", "npm-cli.js"),
		filepath.Join(directory, "..", "share", "nodejs", "npm", "bin", "npm-cli.js"),
	}
	if npm := e.lookPath("npm"); npm != "" {
		if resolved, err := filepath.EvalSymlinks(npm); err == nil && filepath.Base(resolved) == "npm-cli.js" {
			candidates = append(candidates, resolved)
		}
	}
	for _, candidate := range candidates {
		if regularFile(candidate) {
			if absolute, err := filepath.EvalSymlinks(candidate); err == nil {
				return absolute
			}
		}
	}
	return ""
}

func (e environment) query(parent context.Context, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = e.home
	if command.Dir == "" {
		command.Dir = string(filepath.Separator)
	}
	for key, value := range e.values {
		if key != "NODE_OPTIONS" && key != "NODE_PATH" && key != "MISE_NOT_FOUND_AUTO_INSTALL" {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	command.Env = append(command.Env, "MISE_NOT_FOUND_AUTO_INSTALL=0")
	command.WaitDelay = 100 * time.Millisecond
	output := &boundedOutput{}
	command.Stdout, command.Stderr = output, io.Discard
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return output.data, err
}

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(value []byte) (int, error) {
	if len(b.data)+len(value) > 8192 {
		return 0, errors.New("Runtime lookup output exceeds size limit")
	}
	b.data = append(b.data, value...)
	return len(value), nil
}

func executable(path, platform string) bool {
	if !filepath.IsAbs(path) || platform == "windows" && (strings.EqualFold(filepath.Ext(path), ".cmd") || strings.EqualFold(filepath.Ext(path), ".bat")) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && (platform == "windows" || info.Mode()&0111 != 0)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// WithPath changes only the child environment, preserving the caller's PATH
// and handling Windows' case-insensitive environment variable names.
func WithPath(env []string, directories ...string) []string {
	result := make([]string, 0, len(env)+1)
	current := ""
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") {
			current = value
		} else {
			result = append(result, entry)
		}
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, directory := range append(append([]string{}, directories...), filepath.SplitList(current)...) {
		key := filepath.Clean(directory)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if directory != "" && !seen[key] {
			seen[key] = true
			paths = append(paths, directory)
		}
	}
	return append(result, "PATH="+strings.Join(paths, string(os.PathListSeparator)))
}
