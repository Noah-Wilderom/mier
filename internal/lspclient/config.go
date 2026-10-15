package lspclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func canonicalLanguage(value string) (string, error) {
	if value == "javascript" {
		value = "typescript"
	}
	if catalogByID[value] != nil {
		return value, nil
	}
	return "", errors.New("Choose a supported language server from the Settings catalog")
}

func usesTypeScriptSDK(language string) bool {
	return language == "typescript" || language == "vue" || language == "astro" || language == "mdx"
}
func languageForPath(path string) string {
	if matches := serversForPath(path); len(matches) > 0 {
		return matches[0].LanguageID
	}
	return ""
}
func serverLanguage(language string) string {
	if catalogByID[language] != nil {
		return language
	}
	for _, server := range serverCatalog {
		for _, file := range server.Files {
			if file.LanguageID == language {
				return server.ID
			}
		}
	}
	return language
}
func validateConfiguration(configuration ServerConfiguration) error {
	if len(configuration.Args) > 128 {
		return errors.New("Too many language server arguments")
	}
	for _, value := range append([]string{configuration.Command, configuration.TypeScriptSDKPath, configuration.VuePluginPath}, configuration.Args...) {
		if strings.ContainsRune(value, 0) || len(value) > 8192 {
			return errors.New("Invalid language server path or argument")
		}
	}
	if configuration.Command == "" && len(configuration.Args) > 0 {
		return errors.New("Choose an executable when setting custom language server arguments")
	}
	for _, path := range []string{configuration.TypeScriptSDKPath, configuration.VuePluginPath} {
		if path != "" && !filepath.IsAbs(path) {
			return errors.New("Language dependency paths must be absolute")
		}
	}
	return nil
}
func readConfigurations(path string) (map[string]ServerConfiguration, error) {
	result := map[string]ServerConfiguration{}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, errors.New("Language server configuration exceeds size limit")
	}
	var wire struct {
		Version int                            `json:"version"`
		Servers map[string]ServerConfiguration `json:"servers"`
	}
	if err = json.Unmarshal(data, &wire); err != nil || wire.Version != 1 {
		return nil, errors.New("Language server configuration is invalid")
	}
	for language, configuration := range wire.Servers {
		if _, err = canonicalLanguage(language); err != nil {
			return nil, err
		}
		if err = validateConfiguration(configuration); err != nil {
			return nil, err
		}
		result[language] = configuration
	}
	return result, nil
}
func saveConfigurations(path string, configurations map[string]ServerConfiguration) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Version int                            `json:"version"`
		Servers map[string]ServerConfiguration `json:"servers"`
	}{1, configurations}, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".language-servers-*")
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

func projectDirectories(root string) []string {
	directories := map[string]bool{root: true}
	visited := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 10000 {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			relative, _ := filepath.Rel(root, path)
			if path != root && (strings.Count(relative, string(filepath.Separator)) > 5 || entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "package.json" || entry.Name() == "composer.json" {
			directories[filepath.Dir(path)] = true
		}
		return nil
	})
	result := []string{root}
	for directory := range directories {
		if directory != root {
			result = append(result, directory)
		}
	}
	sort.Strings(result[1:])
	return result
}
func existingFile(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }
func existingDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
func resolveProgram(program string) (string, error) {
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(program), ".cmd") || strings.EqualFold(filepath.Ext(program), ".bat")) {
		return "", errors.New("Use node.exe or php.exe with the server script as an argument; command shell wrappers are not supported")
	}
	if !filepath.IsAbs(program) {
		resolved, err := exec.LookPath(program)
		if err != nil {
			return "", err
		}
		program = resolved
	}
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(program), ".cmd") || strings.EqualFold(filepath.Ext(program), ".bat")) {
		return "", errors.New("Use node.exe or php.exe with the server script as an argument; command shell wrappers are not supported")
	}
	if !existingFile(program) {
		return "", errors.New("Executable does not exist")
	}
	absolute, err := filepath.Abs(program)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(absolute)
		if info.Mode()&0111 == 0 {
			return "", errors.New("Configured language server is not executable")
		}
	}
	return absolute, nil
}
func dependencyCandidates(directories []string, command string, args []string, relative string) []string {
	result := []string{}
	for _, directory := range directories {
		result = append(result, filepath.Join(directory, "node_modules", relative))
	}
	sources := append([]string{command}, args...)
	for _, source := range sources {
		if !filepath.IsAbs(source) {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(source); err == nil {
			source = resolved
		}
		directory := filepath.Dir(source)
		for i := 0; i < 7; i++ {
			result = append(result, filepath.Join(directory, "node_modules", relative), filepath.Join(directory, relative))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	return result
}
func findDependency(paths []string, filename string) string {
	for _, path := range paths {
		if existingFile(filepath.Join(path, filename)) {
			if absolute, err := filepath.EvalSymlinks(path); err == nil {
				return absolute
			}
		}
	}
	return ""
}
func detectServer(root, language string, configuration ServerConfiguration, managed ...ServerConfiguration) ServerStatus {
	var directories []string
	if root != "" {
		directories = projectDirectories(root)
	}
	return detectServerInDirectories(directories, language, configuration, managed...)
}
func detectServerInDirectories(directories []string, language string, configuration ServerConfiguration, managed ...ServerConfiguration) ServerStatus {
	status := catalogStatus(language)
	status.Configuration = configuration
	status.Configuration.Args = append([]string{}, configuration.Args...)
	definition := catalogByID[language]
	if definition == nil {
		status.Error = "Choose a supported language server from the catalog"
		return status
	}
	if configuration.Command != "" {
		status.Command = configuration.Command
		status.Args = append([]string{}, configuration.Args...)
		status.Source = "configured"
	} else if len(managed) > 0 && managed[0].Command != "" {
		status.Command = managed[0].Command
		status.Args = append([]string{}, managed[0].Args...)
		status.Source = "managed"
		if configuration.TypeScriptSDKPath == "" {
			configuration.TypeScriptSDKPath = managed[0].TypeScriptSDKPath
		}
		if configuration.VuePluginPath == "" {
			configuration.VuePluginPath = managed[0].VuePluginPath
		}
	} else {
		status.Args = append([]string{}, definition.Args...)
		for _, directory := range directories {
			var script, interpreter string
			switch language {
			case "php":
				script = filepath.Join(directory, "vendor", "bin", "phpactor")
				interpreter = "php"
			case "typescript":
				script = filepath.Join(directory, "node_modules", "typescript-language-server", "lib", "cli.mjs")
				interpreter = "node"
			case "vue":
				script = filepath.Join(directory, "node_modules", "@vue", "language-server", "bin", "vue-language-server.js")
				interpreter = "node"
			default:
				if definition.Install.Method == "npm" {
					script, _ = npmServerEntry(directory, definition)
					interpreter = "node"
				}
			}
			if existingFile(script) {
				if executable, err := resolveProgram(interpreter); err == nil {
					status.Command = executable
					status.Args = append([]string{script}, status.Args...)
					status.Source = "project"
					break
				}
			}
		}
		if status.Command == "" {
			if executable, err := resolveProgram(definition.Program); err == nil {
				status.Command = executable
				status.Source = "PATH"
			}
		}
	}
	if status.Command == "" {
		status.Error = fmt.Sprintf("%s is not installed. Install it in Settings or configure an existing executable", status.Name)
		if definition.Install.Method == "external" {
			status.Error = fmt.Sprintf("Install %s with its SDK or upstream setup instructions, then configure its executable", status.Name)
		}
		return status
	}
	executable, err := resolveProgram(status.Command)
	if err != nil {
		status.Error = fmt.Sprintf("Cannot run %s: %v", status.Name, err)
		return status
	}
	status.Command = executable
	if usesTypeScriptSDK(language) {
		sdk := configuration.TypeScriptSDKPath
		if sdk == "" {
			sdk = findDependency(dependencyCandidates(directories, status.Command, status.Args, filepath.Join("typescript", "lib")), "tsserver.js")
		}
		if !existingFile(filepath.Join(sdk, "tsserver.js")) {
			status.Error = "TypeScript SDK not found. Install TypeScript separately or configure its lib directory"
			return status
		}
		status.Configuration.TypeScriptSDKPath = sdk
		if language == "vue" {
			plugin := configuration.VuePluginPath
			if plugin == "" {
				plugin = findDependency(dependencyCandidates(directories, status.Command, status.Args, filepath.Join("@vue", "typescript-plugin")), "package.json")
			}
			if !existingFile(filepath.Join(plugin, "package.json")) {
				status.Error = "Vue requires @vue/typescript-plugin. Install it alongside the Vue server or configure its package directory"
				return status
			}
			status.Configuration.VuePluginPath = plugin
		}
	}
	status.Available = true
	return status
}
