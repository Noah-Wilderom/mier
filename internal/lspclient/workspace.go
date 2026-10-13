package lspclient

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

const maxDocumentBytes = 4 * 1024 * 1024

type fileScope struct {
	directory, label string
	external         bool
	root             *os.Root
}
type workspace struct {
	root, revision, directory string
	scopeMu                   sync.RWMutex
	scopes                    []fileScope
	sessions                  map[string]*session
	leases                    int
	dependencyBytes           int64
}

func insideDirectory(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func repositoryPath(path string) (string, error) {
	if path == "" || strings.ContainsAny(path, "\x00\\") || filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", errors.New("Choose a repository-relative source file")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("Source file is outside the review workspace")
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if strings.EqualFold(part, ".git") {
			return "", errors.New("Git metadata is not a source file")
		}
	}
	return clean, nil
}
func fileURI(path string) string {
	slash := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}
func pathFromURI(value string) (string, error) {
	uri, err := url.Parse(value)
	if err != nil || uri.Scheme != "file" || (uri.Host != "" && uri.Host != "localhost") || uri.User != nil || uri.RawQuery != "" || uri.Fragment != "" || strings.ContainsRune(uri.Path, 0) {
		return "", errors.New("Definition is not a local file URI")
	}
	path := filepath.FromSlash(uri.Path)
	if runtime.GOOS == "windows" && len(path) > 2 && path[0] == filepath.Separator && path[2] == ':' {
		path = path[1:]
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("Definition URI is not absolute")
	}
	return filepath.Clean(path), nil
}
func extractWorkspace(ctx context.Context, root, cache string, archive ArchiveResult) (*workspace, error) {
	// Temporary directories can have aliases (/var on macOS and 8.3 paths on
	// Windows). Every scope must use the same canonical spelling as read(), or
	// valid source files and monorepo dependency links appear to be outside it.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(cache, "revision-"+archive.Revision[:7]+"-")
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	directory = canonical
	confined, err := os.OpenRoot(directory)
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	success := false
	var captured *workspace
	defer func() {
		if !success {
			if captured != nil {
				captured.close()
			} else {
				confined.Close()
				_ = os.RemoveAll(directory)
			}
		}
	}()
	reader := tar.NewReader(bytes.NewReader(archive.Tar))
	count := 0
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Read commit archive: %w", err)
		}
		count++
		if count > 20000 {
			return nil, errors.New("Selected commit exceeds the 20,000-file analysis limit")
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name, err := repositoryPath(strings.TrimSuffix(header.Name, "/"))
		if err != nil {
			return nil, fmt.Errorf("Unsafe commit archive path: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = confined.MkdirAll(name, 0700); err != nil {
				return nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || total > 64*1024*1024 {
				return nil, errors.New("Selected commit exceeds the 64 MiB analysis limit")
			}
			if header.Size > maxDocumentBytes {
				continue
			}
			if err = confined.MkdirAll(filepath.Dir(name), 0700); err != nil {
				return nil, err
			}
			file, err := confined.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return nil, err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
		case tar.TypeSymlink, tar.TypeLink: // Tracked links never grant access to another revision or external files.
		default:
			return nil, errors.New("Commit archive contains an unsupported file type")
		}
	}
	w := &workspace{root: root, revision: archive.Revision, directory: directory, scopes: []fileScope{{directory: directory, label: "", root: confined}}, sessions: map[string]*session{}}
	captured = w
	for _, project := range projectDirectories(directory) {
		relative, _ := filepath.Rel(directory, project)
		for _, kind := range []string{"node_modules", "vendor"} {
			source := filepath.Join(root, relative, kind)
			resolved, err := filepath.EvalSymlinks(source)
			if err != nil || !existingDirectory(resolved) {
				continue
			}
			if !insideDirectory(root, resolved) && filepath.Base(resolved) != kind {
				continue
			}
			destination := filepath.Join(project, kind)
			if err = w.captureDependencies(ctx, resolved, destination, filepath.ToSlash(filepath.Join(relative, kind)), kind); err != nil {
				return nil, fmt.Errorf("Capture %s source: %w", kind, err)
			}
		}
	}
	success = true
	return w, nil
}
func (w *workspace) addScope(directory, label string, external bool) error {
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	w.scopeMu.Lock()
	defer w.scopeMu.Unlock()
	for _, scope := range w.scopes {
		if scope.directory == resolved {
			return nil
		}
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return err
	}
	w.scopes = append(w.scopes, fileScope{directory: resolved, label: label, external: external, root: root})
	return nil
}
func (w *workspace) read(path string) (text, label string, external bool, err error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", false, err
	}
	w.scopeMu.RLock()
	defer w.scopeMu.RUnlock()
	var selected *fileScope
	for index := range w.scopes {
		scope := &w.scopes[index]
		if insideDirectory(scope.directory, resolved) && (selected == nil || len(scope.directory) > len(selected.directory)) {
			selected = scope
		}
	}
	if selected != nil {
		scope := *selected
		relative, _ := filepath.Rel(scope.directory, resolved)
		file, err := scope.root.Open(relative)
		if err != nil {
			return "", "", false, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return "", "", false, err
		}
		if !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
			file.Close()
			return "", "", false, errors.New("Definition source is not a supported text file (maximum 4 MiB)")
		}
		data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
		file.Close()
		if err != nil {
			return "", "", false, err
		}
		if len(data) > maxDocumentBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return "", "", false, errors.New("Definition source is not UTF-8 text")
		}
		label = filepath.ToSlash(relative)
		if scope.label != "" {
			label = strings.TrimSuffix(scope.label, "/") + "/" + label
		}
		return string(data), label, scope.external, nil
	}
	return "", "", false, errors.New("Definition is outside the selected commit and known dependency directories")
}
func (w *workspace) close() {
	for _, session := range w.sessions {
		_ = session.close()
	}
	w.scopeMu.Lock()
	for _, scope := range w.scopes {
		_ = scope.root.Close()
	}
	w.scopes = nil
	w.scopeMu.Unlock()
	_ = os.RemoveAll(w.directory)
}
func validatePosition(text string, request CodePositionRequest) error {
	if request.Line < 0 || request.Character < 0 {
		return errors.New("Code position must use nonnegative UTF-16 coordinates")
	}
	lines := strings.Split(text, "\n")
	if request.Line >= len(lines) {
		return errors.New("Code line is outside the selected revision")
	}
	line := strings.TrimSuffix(lines[request.Line], "\r")
	if request.LineText != nil && strings.TrimSuffix(*request.LineText, "\r") != line {
		return errors.New("The displayed code line does not match the selected revision. Refresh the diff before inspecting this symbol")
	}
	units := 0
	boundary := request.Character == 0
	for _, character := range line {
		units += utf16.RuneLen(character)
		if units == request.Character {
			boundary = true
		}
	}
	if request.Character > units || !boundary {
		return errors.New("Code character is outside the line or splits a UTF-16 surrogate pair")
	}
	return nil
}
