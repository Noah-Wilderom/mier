package lspclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxDependencyBytes int64 = 1024 * 1024 * 1024
const maxComposerMetadataBytes int64 = 32 * 1024 * 1024

func dependencyKind(language string) string {
	if language == "php" {
		return "vendor"
	}
	switch serverLanguage(language) {
	case "typescript", "vue", "astro", "svelte", "mdx", "html", "css", "marko", "coffeescript", "twig":
		return "node_modules"
	}
	return "source-only"
}

var composerPathLiteral = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)

// Composer can retain the path spelling used during installation, including
// macOS /var aliases and Windows short paths. Resolve literal absolute paths
// before rebasing so those aliases cannot point analysis at the dirty checkout.
func rebaseComposerMetadata(data []byte, root, destination string) []byte {
	return composerPathLiteral.ReplaceAllFunc(data, func(literal []byte) []byte {
		quote := string(literal[:1])
		value := string(literal[1 : len(literal)-1])
		value = strings.ReplaceAll(value, "\\"+quote, quote)
		value = strings.ReplaceAll(value, "\\\\", "\\")
		path := filepath.FromSlash(value)
		if !filepath.IsAbs(path) {
			return literal
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !insideDirectory(root, resolved) {
			return literal
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil {
			return literal
		}
		rebased := filepath.ToSlash(filepath.Join(destination, relative))
		rebased = strings.ReplaceAll(rebased, "\\", "\\\\")
		rebased = strings.ReplaceAll(rebased, quote, "\\"+quote)
		return []byte(quote + rebased + quote)
	})
}

// Dependency metadata and source are frozen with the reviewed workspace. Linking
// an installed directory wholesale would let monorepo aliases escape to dirty
// project source, and Composer's generated baseDir would resolve the wrong tree.
func dependencySource(path, kind string) bool {
	if kind == "vendor" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".php", ".phtml", ".inc", ".stub", ".json", ".yaml", ".yml", ".neon":
			return true
		}
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".php", ".phtml", ".inc", ".stub", ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".vue", ".astro", ".svelte", ".marko", ".coffee", ".json", ".map", ".html", ".css", ".scss", ".sass", ".less", ".yaml", ".yml", ".neon":
		return true
	}
	return false
}

func composerMetadata(path, kind string) bool {
	return kind == "vendor" && (path == "autoload.php" || strings.HasPrefix(filepath.ToSlash(path), "composer/"))
}

type dependencyReader struct {
	ctx context.Context
	io.Reader
}

func (r dependencyReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(buffer)
}

func copyDependency(ctx context.Context, source *os.File, destination string, info os.FileInfo, maximum int64, buffer []byte) (int64, error) {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	// Reuse one bounded buffer rather than allocating a full source file for
	// each dependency. Wrapping the writer also keeps cancellation checks in
	// the reader instead of bypassing them through os.File.ReadFrom.
	written, err := io.CopyBuffer(struct{ io.Writer }{output}, dependencyReader{ctx, io.LimitReader(source, maximum+1)}, buffer)
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		updated, statErr := source.Stat()
		if statErr != nil {
			err = statErr
		} else if written != info.Size() || updated.Size() != info.Size() || !updated.ModTime().Equal(info.ModTime()) {
			err = errors.New("Installed dependency source changed while it was captured")
		}
	}
	if err != nil {
		_ = os.Remove(destination)
	}
	return written, err
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
	buffer := make([]byte, 64*1024)
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
		if !dependencySource(relative, kind) {
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
		metadata := composerMetadata(relative, kind)
		maximum := int64(maxDocumentBytes)
		if metadata {
			maximum = maxComposerMetadataBytes
		}
		if !info.Mode().IsRegular() || info.Size() > maximum {
			file.Close()
			return nil
		}
		defer file.Close()
		if info.Size() > maxDependencyBytes-w.dependencyBytes {
			return fmt.Errorf("Installed %s source exceeds the 1 GiB analysis limit", kind)
		}
		var captured int64
		if metadata {
			data, err := io.ReadAll(dependencyReader{ctx, io.LimitReader(file, maximum+1)})
			if err != nil {
				return err
			}
			updated, err := file.Stat()
			if err != nil {
				return err
			}
			if int64(len(data)) != info.Size() || updated.Size() != info.Size() || !updated.ModTime().Equal(info.ModTime()) {
				return errors.New("Installed dependency source changed while it was captured")
			}
			data = rebaseComposerMetadata(data, w.root, w.directory)
			captured = int64(len(data))
			if captured > maxDependencyBytes-w.dependencyBytes {
				return fmt.Errorf("Installed %s source exceeds the 1 GiB analysis limit", kind)
			}
			if err = os.WriteFile(target, data, 0600); err != nil {
				return err
			}
		} else {
			captured, err = copyDependency(ctx, file, target, info, maximum, buffer)
			if err != nil {
				return err
			}
		}
		w.dependencyBytes += captured
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if err = w.addScope(source, label, true); err != nil {
		return err
	}
	return w.addScope(destination, label, true)
}
