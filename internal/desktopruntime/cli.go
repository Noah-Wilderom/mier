package desktopruntime

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CLIEnvironment supplies Node to npm-installed CLIs using /usr/bin/env node.
// Native executables keep their original environment. PATH is never changed
// globally, so concurrent repository commands retain the user's environment.
func CLIEnvironment(ctx context.Context, binary string) []string {
	env := os.Environ()
	file, err := os.Open(binary)
	if err != nil {
		return env
	}
	defer file.Close()
	header, _ := io.ReadAll(io.LimitReader(file, 512))
	line, _, _ := strings.Cut(string(header), "\n")
	if !strings.HasPrefix(line, "#!") {
		return env
	}
	for _, word := range strings.Fields(strings.TrimPrefix(line, "#!")) {
		if name := filepath.Base(word); name == "node" || name == "node.exe" {
			if node, err := FindNode(ctx, "", false); err == nil {
				return WithPath(env, filepath.Dir(node.Executable))
			}
			break
		}
	}
	return env
}
