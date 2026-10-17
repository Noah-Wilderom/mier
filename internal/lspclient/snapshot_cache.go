package lspclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type cachedFile struct {
	Path, Hash string
	Link       bool
}
type snapshotRecord struct {
	Directory string
	Files     []cachedFile
}

func digestFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, dependencyReader{ctx, f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Dependencies are hashed by content, not just lockfile or mtime: local path
// packages and partially completed installations must invalidate the snapshot.
func dependencyDigest(ctx context.Context, root, kind string) (string, error) {
	hash := sha256.New()
	visited := 0
	var bytes int64
	if kind == "vendor" || kind == "node_modules" {
		for _, project := range projectDirectories(root) {
			source := filepath.Join(project, kind)
			resolved, err := filepath.EvalSymlinks(source)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", err
			}
			if !existingDirectory(resolved) || (!insideDirectory(root, resolved) && filepath.Base(resolved) != kind) {
				continue
			}
			err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				visited++
				if visited > 200000 {
					return fmt.Errorf("Installed dependencies exceed the 200,000-entry analysis limit")
				}
				relative, _ := filepath.Rel(resolved, path)
				if entry.IsDir() {
					if entry.Name() == ".git" || entry.Name() == ".cache" || entry.Name() == ".bin" {
						return filepath.SkipDir
					}
					return nil
				}
				prefix, _ := filepath.Rel(root, source)
				if entry.Type()&os.ModeSymlink != 0 {
					link, err := os.Readlink(path)
					if err != nil {
						return err
					}
					fmt.Fprintf(hash, "%s/%s:link:%s\x00", prefix, relative, link)
					return nil
				}
				if !dependencySource(relative, kind) {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				maximum := int64(maxDocumentBytes)
				if composerMetadata(relative, kind) {
					maximum = maxComposerMetadataBytes
				}
				if !info.Mode().IsRegular() || info.Size() > maximum {
					return nil
				}
				bytes += info.Size()
				if bytes > maxDependencyBytes {
					return fmt.Errorf("Installed %s source exceeds the 1 GiB analysis limit", kind)
				}
				digest, err := digestFile(ctx, path)
				if err != nil {
					return err
				}
				fmt.Fprintf(hash, "%s/%s:%s\x00", prefix, relative, digest)
				return nil
			})
			if err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func snapshotManifest(ctx context.Context, directory string) ([]cachedFile, error) {
	files := []cachedFile{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if len(files) > 220000 {
			return fmt.Errorf("Analysis snapshot exceeds its entry limit")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			files = append(files, cachedFile{relative, target, true})
			return nil
		}
		sum, err := digestFile(ctx, path)
		if err != nil {
			return err
		}
		files = append(files, cachedFile{relative, sum, false})
		return nil
	})
	return files, err
}
func validSnapshot(ctx context.Context, directory string, files []cachedFile) bool {
	if len(files) > 220000 {
		return false
	}
	for _, file := range files {
		relative, err := repositoryPath(filepath.ToSlash(file.Path))
		if err != nil {
			return false
		}
		path := filepath.Join(directory, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return false
		}
		if file.Link {
			if info.Mode()&os.ModeSymlink == 0 {
				return false
			}
			link, err := os.Readlink(path)
			if err != nil || link != file.Hash {
				return false
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return false
		}
		// Do not follow a modified parent link out of the private capture.
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !insideDirectory(directory, resolved) {
			return false
		}
		sum, err := digestFile(ctx, path)
		if err != nil || sum != file.Hash {
			return false
		}
	}
	return true
}
func (m *Manager) captureWorkspace(ctx context.Context, request CodePositionRequest, archive ArchiveResult, kind string) (*workspace, error) {
	// Local/index snapshots change frequently. Their temporary captures retain the
	// existing isolation and are never reused across application restarts.
	if request.Revision == "working" || request.Revision == "index" {
		return extractWorkspace(ctx, request.Root, m.cacheDirectory, archive, kind)
	}
	m.analysisPhase(request, "Checking cached source", false)
	dependencies, err := dependencyDigest(ctx, request.Root, kind)
	if err != nil {
		return nil, err
	}
	tarHash := sha256.Sum256(archive.Tar)
	key := sha256.Sum256([]byte(fmt.Sprintf("snapshot-v1\x00%s\x00%s\x00%s\x00%x\x00%s", request.Root, archive.Revision, kind, tarHash, dependencies)))
	cache := filepath.Join(m.options.CacheDirectory, "snapshots")
	if err = os.MkdirAll(cache, 0700); err != nil {
		return nil, err
	}
	name := hex.EncodeToString(key[:])
	parent := filepath.Join(cache, name)
	lock := flock.New(parent + ".lock")
	acquired, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !acquired {
		return extractWorkspace(ctx, request.Root, m.cacheDirectory, archive, kind)
	}
	retained := false
	defer func() {
		if !retained {
			_ = lock.Close()
		}
	}()
	if data, err := os.ReadFile(filepath.Join(parent, "manifest.json")); err == nil && len(data) <= 32*1024*1024 {
		var record snapshotRecord
		if json.Unmarshal(data, &record) == nil && record.Directory == filepath.Base(record.Directory) && strings.HasPrefix(record.Directory, "revision-") {
			directory, canonicalErr := filepath.EvalSymlinks(filepath.Join(parent, record.Directory))
			if canonicalErr == nil && validSnapshot(ctx, directory, record.Files) {
				w := &workspace{root: request.Root, revision: archive.Revision, directory: directory, sessions: map[string]*session{}}
				if err = w.addScope(directory, "", false); err != nil {
					return nil, err
				}
				for _, project := range projectDirectories(directory) {
					dest := filepath.Join(project, kind)
					if existingDirectory(dest) {
						relative, _ := filepath.Rel(directory, dest)
						if err = w.addScope(dest, filepath.ToSlash(relative), true); err != nil {
							w.close()
							return nil, err
						}
					}
				}
				w.persistent = true
				w.cacheRelease = func() { _ = lock.Close() }
				retained = true
				now := time.Now()
				_ = os.Chtimes(parent, now, now)
				m.analysisPhase(request, "Using cached source", true)
				return w, nil
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(parent)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	m.analysisPhase(request, "Capturing revision and dependencies", false)
	w, err := extractWorkspace(ctx, request.Root, parent, archive, kind)
	if err != nil {
		return nil, err
	}
	files, err := snapshotManifest(ctx, w.directory)
	if err != nil {
		w.close()
		return nil, err
	}
	// Detect an installation changing during capture; the next request can retry.
	after, err := dependencyDigest(ctx, request.Root, kind)
	if err != nil || after != dependencies {
		w.close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Installed dependencies changed during analysis preparation. Retry after installation finishes")
	}
	record, _ := json.Marshal(snapshotRecord{filepath.Base(w.directory), files})
	if err = os.WriteFile(filepath.Join(parent, "manifest.json"), record, 0600); err != nil {
		w.close()
		return nil, err
	}
	w.persistent = true
	w.cacheRelease = func() { _ = lock.Close() }
	retained = true
	pruneSnapshots(cache, parent)
	return w, nil
}
func pruneSnapshots(cache, current string) {
	entries, _ := os.ReadDir(cache)
	type candidate struct {
		path string
		time time.Time
	}
	var folders []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			folders = append(folders, candidate{filepath.Join(cache, entry.Name()), info.ModTime()})
		}
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].time.Before(folders[j].time) })
	count := len(folders)
	for _, folder := range folders {
		if count <= 4 {
			break
		}
		if folder.path == current {
			continue
		}
		lock := flock.New(folder.path + ".lock")
		if acquired, _ := lock.TryLock(); acquired {
			if os.RemoveAll(folder.path) == nil {
				count--
			}
		}
		_ = lock.Close()
	}
}
