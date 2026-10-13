package lspclient

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Dependency metadata and source are frozen with the reviewed workspace. Linking
// an installed directory wholesale would let monorepo aliases escape to dirty
// project source, and Composer's generated baseDir would resolve the wrong tree.
func dependencySource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".php", ".phtml", ".inc", ".stub", ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".vue", ".json", ".map", ".html", ".css", ".scss", ".sass", ".less", ".yaml", ".yml", ".neon":
		return true
	}
	return false
}

func (w *workspace) captureDependencies(ctx context.Context, source, destination, label, kind string) error {
	confined, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer confined.Close()
	if err = os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	visited := 0
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > 200000 {
			return errors.New("Installed dependencies exceed the 200,000-entry analysis limit")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".cache" || entry.Name() == ".bin" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0700)
		}
		if _, err = os.Lstat(target); err == nil {
			return nil // Selected tracked files take precedence over installed copies.
		} else if !os.IsNotExist(err) {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			var rebased string
			if insideDirectory(w.root, resolved) {
				relative, _ := filepath.Rel(w.root, resolved)
				rebased = filepath.Join(w.directory, relative)
			} else if insideDirectory(source, resolved) {
				relative, _ := filepath.Rel(source, resolved)
				rebased = filepath.Join(destination, relative)
			} else {
				return nil // A dependency cannot grant arbitrary outside source.
			}
			return os.Symlink(rebased, target)
		}
		if !dependencySource(relative) {
			return nil
		}
		file, err := confined.Open(relative)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
			file.Close()
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
		file.Close()
		if err != nil {
			return err
		}
		if len(data) > maxDocumentBytes {
			return errors.New("Installed dependency source changed while it was captured")
		}
		w.dependencyBytes += int64(len(data))
		if w.dependencyBytes > 128*1024*1024 {
			return errors.New("Installed dependency source exceeds the 128 MiB analysis limit")
		}
		if kind == "vendor" && (relative == "autoload.php" || strings.HasPrefix(filepath.ToSlash(relative), "composer/")) {
			value := string(data)
			value = strings.ReplaceAll(value, strings.ReplaceAll(w.root, "\\", "\\\\"), strings.ReplaceAll(w.directory, "\\", "\\\\"))
			value = strings.ReplaceAll(value, filepath.ToSlash(w.root), filepath.ToSlash(w.directory))
			data = []byte(value)
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		return err
	}
	if err = w.addScope(source, label, true); err != nil {
		return err
	}
	return w.addScope(destination, label, true)
}
