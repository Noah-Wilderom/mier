package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fixtureMetadata struct {
	Version, Executable, MiseNode, Directory, CountFile string
	Hang, Invalid, SafeEnvironment                      bool
}

// The test binary doubles as Node/mise so executable discovery and its bounded
// subprocess probes run on all supported OSes without downloading a runtime.
func TestMain(m *testing.M) {
	if os.Getenv("MIER_NODE_FIXTURE") == "1" {
		binary, _ := os.Executable()
		data, _ := os.ReadFile(binary + ".json")
		var metadata fixtureMetadata
		if json.Unmarshal(data, &metadata) != nil {
			os.Exit(20)
		}
		if metadata.Hang {
			time.Sleep(10 * time.Second)
			os.Exit(21)
		}
		if metadata.Invalid {
			fmt.Print("invalid runtime output")
			return
		}
		if metadata.SafeEnvironment && (os.Getenv("NODE_OPTIONS") != "" || os.Getenv("NODE_PATH") != "" || os.Getenv("MISE_NOT_FOUND_AUTO_INSTALL") != "0") {
			os.Exit(22)
		}
		if metadata.Directory != "" {
			current, _ := os.Getwd()
			expected, _ := filepath.EvalSymlinks(metadata.Directory)
			if current != expected {
				os.Exit(23)
			}
		}
		if metadata.CountFile != "" {
			file, _ := os.OpenFile(metadata.CountFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if file != nil {
				_, _ = file.WriteString("probe\n")
				_ = file.Close()
			}
		}
		if metadata.MiseNode != "" {
			if len(os.Args) != 3 || os.Args[1] != "which" || os.Args[2] != "node" {
				os.Exit(24)
			}
			fmt.Print(metadata.MiseNode)
			return
		}
		if len(os.Args) > 1 && os.Args[1] != "-p" {
			fmt.Print("node child ready")
			return
		}
		if metadata.Executable == "" {
			metadata.Executable = binary
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"version": metadata.Version, "executable": metadata.Executable})
		return
	}
	os.Exit(m.Run())
}

func fixtureEnvironment(t *testing.T) environment {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return environment{home: home, platform: runtime.GOOS, values: map[string]string{"MIER_NODE_FIXTURE": "1", "GORACE": "atexit_sleep_ms=0", "PATH": t.TempDir(), "SYSTEMROOT": os.Getenv("SystemRoot")}}
}

func fixtureProgram(t *testing.T, path string, metadata fixtureMetadata) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	if err := os.Link(binary, path); err != nil {
		source, err := os.Open(binary)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		target, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(target, source)
		closeErr := target.Close()
		if copyErr != nil || closeErr != nil {
			t.Fatal(copyErr, closeErr)
		}
	}
	data, _ := json.Marshal(metadata)
	if err := os.WriteFile(path+".json", data, 0600); err != nil {
		t.Fatal(err)
	}
	// Discovery returns a canonical executable path. Temp directories may use
	// /var aliases on macOS or short names on Windows, including PATH fixtures.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func fixtureNode(t *testing.T, env environment, directory, version string, npm bool) string {
	t.Helper()
	program := fixtureProgram(t, env.nodeIn(directory), fixtureMetadata{Version: version})
	if npm {
		npmPath := filepath.Join(directory, "node_modules", "npm", "bin", "npm-cli.js")
		if err := os.MkdirAll(filepath.Dir(npmPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(npmPath, []byte("// fixture npm"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return program
}

func fixtureInstalledNode(t *testing.T, env environment, root, version string, npm bool) string {
	t.Helper()
	if env.platform != "windows" {
		root = filepath.Join(root, version, "bin")
	} else {
		root = filepath.Join(root, version)
	}
	return fixtureNode(t, env, root, strings.TrimPrefix(version, "v"), npm)
}

func writeFixtureAlias(t *testing.T, root, alias, value string) {
	t.Helper()
	path := filepath.Join(root, "alias", filepath.FromSlash(alias))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestActiveNodeSelectionAndActualExecutable(t *testing.T) {
	env := fixtureEnvironment(t)
	actual := fixtureNode(t, env, filepath.Join(env.home, "runtime with [brackets]", "bin"), "124.2.0", true)
	shim := fixtureProgram(t, env.nodeIn(env.get("PATH")), fixtureMetadata{Version: "124.2.0", Executable: actual, Directory: env.home, SafeEnvironment: true})
	env.values["NVM_BIN"] = filepath.Dir(fixtureNode(t, env, filepath.Join(env.home, "other", "bin"), "126.0.0", true))
	env.values["NODE_OPTIONS"] = "--require=/untrusted/project/loader.js"
	env.values["NODE_PATH"] = "/untrusted/project/node_modules"
	node, err := env.findNode(context.Background(), "122.22.2", true)
	if err != nil || node.Executable != actual || node.Executable == shim || !strings.HasPrefix(node.NPMCLI, filepath.Dir(actual)) {
		t.Fatalf("active shim did not resolve to its actual runtime and npm: %#v, %v", node, err)
	}
}

func TestMiseSelectedNodeWorksOutsidePATHAndRepository(t *testing.T) {
	for _, variable := range []string{"MISE_DATA_DIR", "XDG_DATA_HOME", "default"} {
		t.Run(variable, func(t *testing.T) {
			env := fixtureEnvironment(t)
			if variable != "default" {
				env.values[variable] = filepath.Join(env.home, "custom [data] directory")
			}
			root := filepath.Join(env.miseRoot(), "installs", "node")
			selected := fixtureInstalledNode(t, env, root, "124.2.0", true)
			_ = fixtureInstalledNode(t, env, root, "126.0.0", true)
			name := "mise"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			fixtureProgram(t, filepath.Join(env.home, ".local", "bin", name), fixtureMetadata{MiseNode: selected, Directory: env.home, SafeEnvironment: true})
			node, err := env.findNode(context.Background(), "122.22.2", true)
			if err != nil || node.Executable != selected {
				t.Fatalf("mise's selected runtime was ignored: %#v, %v", node, err)
			}
		})
	}
}

func TestMiseFallbackSkipsBrokenShimsOldVersionsAndMissingNPM(t *testing.T) {
	env := fixtureEnvironment(t)
	env.values["MISE_DATA_DIR"] = filepath.Join(env.home, "mis[e] data")
	fixtureProgram(t, env.nodeIn(env.get("PATH")), fixtureMetadata{Invalid: true})
	root := filepath.Join(env.miseRoot(), "installs", "node")
	_ = fixtureInstalledNode(t, env, root, "120.0.0", true)
	_ = fixtureInstalledNode(t, env, root, "126.0.0", false)
	_ = fixtureInstalledNode(t, env, root, "200.0.0-rc.1", true)
	wanted := fixtureInstalledNode(t, env, root, "124.10.0", true)
	_ = fixtureInstalledNode(t, env, root, "124.9.0", true)
	node, err := env.findNode(context.Background(), "122.22.2", true)
	if err != nil || node.Executable != wanted {
		t.Fatalf("installed fallback chose unusable or incorrectly sorted Node: %#v, %v", node, err)
	}
}

func TestNVMDefaultsAndAliasesWithRestrictedPATH(t *testing.T) {
	for _, alias := range []string{"124", "124.10", "v124.10.0", "work", "node", "lts/example", "lts/*"} {
		t.Run(alias, func(t *testing.T) {
			env := fixtureEnvironment(t)
			root := filepath.Join(env.home, ".nvm")
			versions := filepath.Join(root, "versions", "node")
			older := fixtureInstalledNode(t, env, versions, "v124.10.0", true)
			newer := fixtureInstalledNode(t, env, versions, "v126.0.0", true)
			writeFixtureAlias(t, root, "default", alias)
			writeFixtureAlias(t, root, "work", "lts/example")
			writeFixtureAlias(t, root, "lts/example", "v124.10.0")
			wanted := older
			if alias == "node" {
				wanted = newer
			}
			node, err := env.findNode(context.Background(), "122.22.2", true)
			if err != nil || node.Executable != wanted {
				t.Fatalf("nvm default %s was ignored: %#v, %v", alias, node, err)
			}
		})
	}
}

func TestNVMEnvironmentAndXDGDirectories(t *testing.T) {
	for _, variable := range []string{"NVM_BIN", "NVM_SYMLINK", "NVM_DIR", "XDG_CONFIG_HOME"} {
		t.Run(variable, func(t *testing.T) {
			env := fixtureEnvironment(t)
			env.values[variable] = filepath.Join(env.home, "Node installations with spaces")
			directory := env.get(variable)
			if variable == "NVM_DIR" || variable == "XDG_CONFIG_HOME" {
				if variable == "XDG_CONFIG_HOME" {
					directory = filepath.Join(directory, "nvm")
				}
				writeFixtureAlias(t, directory, "default", "124")
				directory = filepath.Join(directory, "versions", "node", "v124.10.0")
				if runtime.GOOS != "windows" {
					directory = filepath.Join(directory, "bin")
				}
			}
			wanted := fixtureNode(t, env, directory, "124.10.0", true)
			node, err := env.findNode(context.Background(), "122.22.2", true)
			if err != nil || node.Executable != wanted {
				t.Fatalf("%s runtime not detected: %#v, %v", variable, node, err)
			}
		})
	}
}

func TestNodeLookupBoundsSlowProgramsAndCancellation(t *testing.T) {
	env := fixtureEnvironment(t)
	fixtureProgram(t, env.nodeIn(env.get("PATH")), fixtureMetadata{Hang: true})
	wanted := fixtureNode(t, env, filepath.Join(env.home, "active"), "124.0.0", true)
	env.values["NVM_BIN"] = filepath.Dir(wanted)
	start := time.Now()
	node, err := env.findNode(context.Background(), "122.22.2", true)
	if err != nil || node.Executable != wanted || time.Since(start) > 4*time.Second {
		t.Fatalf("a hung shim prevented fallback: %#v, %v", node, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&nodeResolver{}).find(ctx, env, "", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup still ran: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := (&nodeResolver{}).find(ctx, env, "", false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("running runtime lookup ignored cancellation: %v", err)
	}
}

func TestExplicitRefreshRereadsTheRuntime(t *testing.T) {
	env := fixtureEnvironment(t)
	program := fixtureNode(t, env, filepath.Join(env.home, "active"), "124.0.0", false)
	t.Setenv("MIER_NODE_FIXTURE", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("PATH", env.get("PATH"))
	t.Setenv("NVM_BIN", filepath.Dir(program))
	defer RefreshNode()
	if node, err := FindNode(context.Background(), "", false); err != nil || node.Version != "124.0.0" {
		t.Fatal(node, err)
	}
	data, _ := json.Marshal(fixtureMetadata{Version: "126.0.0"})
	if err := os.WriteFile(program+".json", data, 0600); err != nil {
		t.Fatal(err)
	}
	if node, err := FindNode(context.Background(), "", false); err != nil || node.Version != "124.0.0" {
		t.Fatal("short cache was not reused", node, err)
	}
	RefreshNode()
	if node, err := FindNode(context.Background(), "", false); err != nil || node.Version != "126.0.0" {
		t.Fatal("check again did not refresh the runtime", node, err)
	}
}

func TestNodeCacheRefreshesWhenExecutableDisappearsOrSelectionChanges(t *testing.T) {
	env := fixtureEnvironment(t)
	first := fixtureNode(t, env, filepath.Join(env.home, "first"), "124.0.0", true)
	second := fixtureNode(t, env, filepath.Join(env.home, "second"), "126.0.0", true)
	count := filepath.Join(env.home, "probes")
	data, _ := json.Marshal(fixtureMetadata{Version: "124.0.0", CountFile: count})
	_ = os.WriteFile(first+".json", data, 0600)
	env.values["NVM_BIN"] = filepath.Dir(first)
	resolver := &nodeResolver{}
	for range 2 {
		if node, err := resolver.find(context.Background(), env, "", false); err != nil || node.Executable != first {
			t.Fatal(node, err)
		}
	}
	probes, _ := os.ReadFile(count)
	if string(probes) != "probe\n" {
		t.Fatalf("repeated catalog lookup started Node again: %q", probes)
	}
	env.values["NVM_BIN"] = filepath.Dir(second)
	if node, err := resolver.find(context.Background(), env, "", false); err != nil || node.Executable != second {
		t.Fatalf("environment change did not invalidate cache: %#v, %v", node, err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	fallback := fixtureNode(t, env, env.get("PATH"), "124.0.0", true)
	if node, err := resolver.find(context.Background(), env, "", false); err != nil || node.Executable != fallback {
		t.Fatalf("missing runtime did not fall back: %#v, %v", node, err)
	}
}

func TestNVMAliasCyclesAndTraversalDoNotEscapeManager(t *testing.T) {
	root := t.TempDir()
	writeFixtureAlias(t, root, "default", "one")
	writeFixtureAlias(t, root, "one", "default")
	if value := nvmDefault(root, nil); value != "" {
		t.Fatal("alias cycle returned a runtime", value)
	}
	for _, alias := range []string{"../outside", "/absolute", "lts/../../outside", "lts\\outside"} {
		if value := readAlias(root, alias); value != "" {
			t.Fatal("unsafe alias was read", alias, value)
		}
	}
}

func TestWindowsRuntimeLocationsAndNPMLayout(t *testing.T) {
	env := fixtureEnvironment(t)
	env.platform = "windows"
	env.values["LOCALAPPDATA"] = filepath.Join(env.home, "Local App Data")
	if env.miseRoot() != filepath.Join(env.get("LOCALAPPDATA"), "mise") {
		t.Fatal("Windows mise data directory missing")
	}
	env.values["NVM_HOME"] = filepath.Join(env.home, "nvm-windows")
	wanted := fixtureInstalledNode(t, env, env.get("NVM_HOME"), "v124.2.0", true)
	found := false
	for _, candidate := range env.nodeCandidates() {
		found = found || candidate == wanted
	}
	if !found || env.npmCLI(wanted) != filepath.Join(filepath.Dir(wanted), "node_modules", "npm", "bin", "npm-cli.js") {
		t.Fatal("Windows nvm or npm layout not detected", wanted)
	}
	if executable(filepath.Join(env.home, "node.cmd"), "windows") {
		t.Fatal("command shell wrapper accepted as Node")
	}
}

func TestCLIEnvironmentPreservesPATHAndRunsNodeShebangs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shebang execution; Windows runtime layouts are tested separately")
	}
	env := fixtureEnvironment(t)
	node := fixtureNode(t, env, filepath.Join(env.home, "nvm node", "bin"), "124.0.0", false)
	t.Setenv("MIER_NODE_FIXTURE", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	// Empty PATH makes the fixture the only active inherited selection.
	t.Setenv("PATH", filepath.Join(env.home, "desktop-path"))
	t.Setenv("NVM_BIN", filepath.Dir(node))
	cli := filepath.Join(env.home, "agent cli")
	if err := os.WriteFile(cli, []byte("#!/usr/bin/env node\n// fixture CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(cli)
	command.Env = CLIEnvironment(context.Background(), cli)
	output, err := command.Output()
	if err != nil || string(output) != "node child ready" {
		t.Fatalf("Node shebang still failed with desktop PATH: %s, %v", output, err)
	}
	if os.Getenv("PATH") != filepath.Join(env.home, "desktop-path") {
		t.Fatal("runtime discovery modified the app's global PATH")
	}
	if got := CLIEnvironment(context.Background(), node); !reflect.DeepEqual(got, os.Environ()) {
		t.Fatal("native executable environment was changed")
	}
	path := WithPath([]string{"KEEP=value", "Path=/original", "PATH=/final"}, "/node with spaces", "/node with spaces")
	if !reflect.DeepEqual(path, []string{"KEEP=value", "PATH=/node with spaces" + string(os.PathListSeparator) + "/final"}) {
		t.Fatal("child PATH was duplicated or discarded", path)
	}
}
