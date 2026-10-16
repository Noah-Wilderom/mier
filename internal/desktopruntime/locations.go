package desktopruntime

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

func (e environment) nodeIn(directory string) string {
	if e.platform == "windows" {
		return filepath.Join(directory, "node.exe")
	}
	return filepath.Join(directory, "node")
}

func (e environment) lookPath(name string) string {
	for _, directory := range filepath.SplitList(e.get("PATH")) {
		if !filepath.IsAbs(directory) {
			continue
		}
		filename := name
		if e.platform == "windows" {
			filename += ".exe"
		}
		if candidate := filepath.Join(directory, filename); executable(candidate, e.platform) {
			return candidate
		}
	}
	return ""
}

func (e environment) misePrograms() []string {
	name := "mise"
	if e.platform == "windows" {
		name += ".exe"
	}
	return []string{e.get("MISE_BIN"), e.lookPath("mise"), filepath.Join(e.home, ".local", "bin", name), filepath.Join(e.home, ".cargo", "bin", name), filepath.Join("/opt/homebrew/bin", name), filepath.Join("/usr/local/bin", name)}
}

func (e environment) miseRoot() string {
	if root := e.get("MISE_DATA_DIR"); filepath.IsAbs(root) {
		return root
	}
	if root := e.get("XDG_DATA_HOME"); filepath.IsAbs(root) {
		return filepath.Join(root, "mise")
	}
	if e.platform == "windows" {
		if root := e.get("LOCALAPPDATA"); filepath.IsAbs(root) {
			return filepath.Join(root, "mise")
		}
	}
	return filepath.Join(e.home, ".local", "share", "mise")
}

func (e environment) nvmRoots() []string {
	result := []string{}
	for _, name := range []string{"NVM_DIR", "NVM_HOME"} {
		if root := e.get(name); filepath.IsAbs(root) {
			result = append(result, root)
		}
	}
	if root := e.get("XDG_CONFIG_HOME"); filepath.IsAbs(root) {
		result = append(result, filepath.Join(root, "nvm"))
	}
	return append(result, filepath.Join(e.home, ".nvm"), filepath.Join(e.home, ".config", "nvm"))
}

// Only installed, stable version directories are fallback candidates. ReadDir
// avoids interpreting glob characters in the user's home or manager path.
func installedVersions(root string) []string {
	entries, _ := os.ReadDir(root)
	versions := []string{}
	for _, entry := range entries {
		version := "v" + strings.TrimPrefix(entry.Name(), "v")
		if semver.IsValid(version) && semver.Prerelease(version) == "" {
			versions = append(versions, filepath.Join(root, entry.Name()))
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return semver.Compare("v"+strings.TrimPrefix(filepath.Base(versions[i]), "v"), "v"+strings.TrimPrefix(filepath.Base(versions[j]), "v")) > 0
	})
	return versions
}

func (e environment) nodeCandidates() []string {
	result := []string{}
	nvm := []string{}
	for _, root := range e.nvmRoots() {
		versions := installedVersions(filepath.Join(root, "versions", "node"))
		if e.platform == "windows" {
			versions = append(versions, installedVersions(root)...)
		}
		if preferred := nvmDefault(root, versions); preferred != "" {
			if e.platform != "windows" {
				preferred = filepath.Join(preferred, "bin")
			}
			result = append(result, e.nodeIn(preferred))
		}
		nvm = append(nvm, versions...)
	}
	if e.platform == "windows" {
		for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if root := e.get(name); filepath.IsAbs(root) {
				result = append(result, e.nodeIn(filepath.Join(root, "nodejs")))
			}
		}
	} else {
		for _, root := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
			result = append(result, e.nodeIn(root))
		}
		// Homebrew's versioned formulae can be installed without being linked.
		for _, root := range []string{"/opt/homebrew/opt", "/usr/local/opt"} {
			entries, _ := os.ReadDir(root)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "node@") {
					result = append(result, e.nodeIn(filepath.Join(root, entry.Name(), "bin")))
				}
			}
		}
	}
	for _, root := range append(installedVersions(filepath.Join(e.miseRoot(), "installs", "node")), nvm...) {
		if e.platform != "windows" {
			root = filepath.Join(root, "bin")
		}
		result = append(result, e.nodeIn(root))
	}
	return result
}

// Resolve nvm's default, partial versions, custom aliases and lts aliases from
// its own small data files. No shell profile is evaluated and cycles stop.
func nvmDefault(root string, versions []string) string {
	alias := "default"
	seen := map[string]bool{}
	for range 16 {
		if seen[alias] {
			return ""
		}
		seen[alias] = true
		if alias == "lts/*" {
			entries, _ := os.ReadDir(filepath.Join(root, "alias", "lts"))
			matches := map[string]bool{}
			for _, entry := range entries {
				if entry.Name() != "*" {
					if selected := nvmDefaultAlias(root, "lts/"+entry.Name(), versions); selected != "" {
						matches[selected] = true
					}
				}
			}
			for _, version := range versions {
				if matches[version] {
					return version
				}
			}
			return ""
		}
		if alias == "node" || alias == "stable" {
			if len(versions) > 0 {
				return versions[0]
			}
			return ""
		}
		if match := matchVersion(alias, versions); match != "" {
			return match
		}
		next := readAlias(root, alias)
		if next == "" {
			return ""
		}
		alias = next
	}
	return ""
}

func nvmDefaultAlias(root, alias string, versions []string) string {
	return matchVersion(readAlias(root, alias), versions)
}

func readAlias(root, alias string) string {
	// Accept one alias name or one `lts/name` component, never traversal.
	parts := strings.Split(alias, "/")
	if len(parts) > 2 || len(parts) == 2 && parts[0] != "lts" {
		return ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00\r\n") {
			return ""
		}
	}
	file, err := os.Open(filepath.Join(root, "alias", filepath.FromSlash(alias)))
	if err != nil {
		return ""
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, 257))
	if err != nil || len(value) > 256 {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func matchVersion(request string, versions []string) string {
	request = strings.TrimPrefix(request, "v")
	if request == "" {
		return ""
	}
	for _, directory := range versions {
		version := strings.TrimPrefix(filepath.Base(directory), "v")
		if version == request || strings.HasPrefix(version, request+".") {
			return directory
		}
	}
	return ""
}
