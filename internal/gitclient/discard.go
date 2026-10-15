package gitclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DiscardPreview struct {
	Path      string      `json:"path"`
	Token     string      `json:"token"`
	Diff      ChangedFile `json:"diff"`
	Untracked bool        `json:"untracked"`
}
type DiscardRequest struct {
	Path  string `json:"path"`
	Token string `json:"token"`
	Hunk  *int   `json:"hunk,omitempty"`
	Lines []int  `json:"lines,omitempty"`
}
type DiscardBackup struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	CreatedAt  string `json:"createdAt"`
	Exists     bool   `json:"exists"`
	Mode       uint32 `json:"mode"`
	Content    []byte `json:"-"`
	AfterToken string `json:"afterToken"`
}

// Tokens include file bytes, type, executable bit and the index entry. A status
// or diff alone cannot detect a changed binary file or a changed symlink target.
func (c *Client) workingVersion(ctx context.Context, root, file string) (DiscardBackup, string, error) {
	version := DiscardBackup{Path: file}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return version, "", err
	}
	defer dir.Close()
	info, err := dir.Lstat(file)
	if err == nil {
		version.Exists = true
		version.Mode = uint32(info.Mode())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, e := dir.Readlink(file)
			err = e
			version.Content = []byte(target)
		case info.Mode().IsRegular():
			var f *os.File
			f, err = dir.Open(file)
			if err == nil {
				version.Content, err = io.ReadAll(io.LimitReader(f, 64*1024*1024+1))
				f.Close()
				if len(version.Content) > 64*1024*1024 {
					return version, "", errors.New("Files larger than 64 MiB must be discarded in an external Git client")
				}
			}
		default:
			return version, "", errors.New("Submodules, directories, and special files must be handled separately")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return version, "", err
	}
	index, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z", "--", file)
	if err != nil {
		return version, "", err
	}
	return version, digest(file, fmt.Sprint(version.Exists), fmt.Sprint(version.Mode), string(version.Content), string(index)), nil
}
func (c *Client) discardPreview(ctx context.Context, root, file string) (DiscardPreview, error) {
	file, err := validateFile(root, file)
	if err != nil {
		return DiscardPreview{}, err
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return DiscardPreview{}, err
	}
	if op, err := c.operation(ctx, root, state); err != nil {
		return DiscardPreview{}, err
	} else if op != nil {
		return DiscardPreview{}, errors.New("Finish or abort the current Git operation before discarding")
	}
	var found *FileStatus
	for _, change := range state.Changes {
		if change.Path == file {
			found = &change
			break
		}
	}
	if found == nil || !found.Unstaged || found.Conflict || found.OriginalPath != "" || (found.Submodule != "" && found.Submodule != "N...") {
		return DiscardPreview{}, errors.New("Choose an unstaged file without conflicts, renames, or submodule changes")
	}
	_, token, err := c.workingVersion(ctx, root, file)
	if err != nil {
		return DiscardPreview{}, err
	}
	diff, err := c.diff(ctx, root, file, false)
	return DiscardPreview{Path: file, Token: token, Diff: diff, Untracked: found.Untracked}, err
}
func (c *Client) PreviewDiscard(ctx context.Context, path, file string) (DiscardPreview, error) {
	var preview DiscardPreview
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		preview, err = c.discardPreview(ctx, root, file)
		return err
	})
	return preview, err
}
func (c *Client) backupDirectory(ctx context.Context, root string) (string, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "mier", "discard")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
func (c *Client) Discard(ctx context.Context, path string, request DiscardRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		preview, err := c.discardPreview(ctx, root, request.Path)
		if err != nil {
			return err
		}
		if request.Token == "" || preview.Token != request.Token {
			return errors.New("This file or its index changed. Preview the discard again")
		}
		patch := ""
		if request.Hunk != nil {
			if preview.Untracked || preview.Diff.Binary || preview.Diff.Truncated || preview.Diff.Status != "modified" || strings.Contains(preview.Diff.RawPatch, "old mode ") {
				return errors.New("Select the whole file for this discard")
			}
			patch, err = selectHunk(preview.Diff.RawPatch, *request.Hunk)
			if err != nil {
				return err
			}
			if len(request.Lines) > 0 {
				patch, err = selectPatchLines(patch, request.Lines, true)
				if err != nil {
					return err
				}
			} else {
				// Reverse all changed rows using the same source-side patch builder.
				rows := []int{}
				row := 0
				inHunk := false
				for _, line := range strings.Split(patch, "\n") {
					if strings.HasPrefix(line, "@@ ") {
						inHunk = true
						continue
					}
					if inHunk && line != "" && !strings.HasPrefix(line, "\\") {
						if line[0] == '+' || line[0] == '-' {
							rows = append(rows, row)
						}
						row++
					}
				}
				patch, err = selectPatchLines(patch, rows, true)
				if err != nil {
					return err
				}
			}
			if _, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(patch), "apply", "--check"); err != nil {
				return err
			}
		}
		backup, token, err := c.workingVersion(ctx, root, preview.Path)
		if err != nil {
			return err
		}
		if token != request.Token {
			return errors.New("File changed while preparing the discard")
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		backup.ID = hex.EncodeToString(nonce[:])
		backup.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		dir, err := c.backupDirectory(ctx, root)
		if err != nil {
			return err
		}
		name := filepath.Join(dir, backup.ID+".json")
		if err = os.WriteFile(filepath.Join(dir, backup.ID+".data"), backup.Content, 0600); err != nil {
			return fmt.Errorf("Save discard recovery content: %w", err)
		}
		data, err := json.Marshal(backup)
		if err != nil {
			return err
		}
		if err = os.WriteFile(name, data, 0600); err != nil {
			return fmt.Errorf("Save discard recovery copy: %w", err)
		}
		if request.Hunk != nil {
			_, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(patch), "apply")
		} else if preview.Untracked {
			var d *os.Root
			d, err = os.OpenRoot(root)
			if err == nil {
				err = d.Remove(preview.Path)
				d.Close()
			}
		} else {
			_, err = c.run(ctx, root, c.options.LocalTimeout, "restore", "--worktree", "--", preview.Path)
		}
		_, after, readErr := c.workingVersion(ctx, root, preview.Path)
		if readErr == nil {
			backup.AfterToken = after
			data, _ = json.Marshal(backup)
			if saveErr := os.WriteFile(name, data, 0600); saveErr != nil && err == nil {
				err = fmt.Errorf("Discard completed; recovery metadata could not be updated: %w", saveErr)
			}
		}
		return err
	})
}
func (c *Client) DiscardBackups(ctx context.Context, path string) ([]DiscardBackup, error) {
	items := []DiscardBackup{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		dir, err := c.backupDirectory(ctx, root)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return err
			}
			var item DiscardBackup
			if err = json.Unmarshal(data, &item); err != nil {
				return err
			}
			item.Content = nil
			items = append(items, item)
		}
		return nil
	})
	return items, err
}
func (c *Client) RestoreDiscard(ctx context.Context, path, id string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if len(id) != 32 {
			return errors.New("Choose a recovery entry")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return err
		}
		dir, err := c.backupDirectory(ctx, root)
		if err != nil {
			return err
		}
		name := filepath.Join(dir, id+".json")
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		var backup DiscardBackup
		if err = json.Unmarshal(data, &backup); err != nil {
			return err
		}
		backup.Content, err = os.ReadFile(filepath.Join(dir, id+".data"))
		if err != nil {
			return err
		}
		file, err := validateFile(root, backup.Path)
		if err != nil {
			return err
		}
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if op, err := c.operation(ctx, root, state); err != nil {
			return err
		} else if op != nil {
			return errors.New("Finish the current Git operation before restoring")
		}
		_, token, err := c.workingVersion(ctx, root, file)
		if err != nil {
			return err
		}
		if backup.AfterToken == "" || token != backup.AfterToken {
			return errors.New("The file or index changed since discarding. Preserve those changes before restoring this recovery copy")
		}
		d, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer d.Close()
		if backup.Exists {
			if err = d.MkdirAll(filepath.Dir(file), 0755); err != nil {
				return err
			}
		}
		if !backup.Exists {
			err = d.Remove(file)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else if os.FileMode(backup.Mode)&os.ModeSymlink != 0 {
			if e := d.Remove(file); e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
			err = d.Symlink(string(backup.Content), file)
		} else {
			if info, e := d.Lstat(file); e == nil && info.Mode()&os.ModeSymlink != 0 {
				if e = d.Remove(file); e != nil {
					return e
				}
			}
			err = writeConflictResult(root, file, backup.Content, os.FileMode(backup.Mode).Perm())
		}
		if err != nil {
			return err
		}
		if err = os.Remove(name); err != nil {
			return err
		}
		return os.Remove(filepath.Join(dir, id+".data"))
	})
}

func (c *Client) IgnoreFile(ctx context.Context, path, file string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		file, err := validateFile(root, file)
		if err != nil {
			return err
		}
		if strings.ContainsAny(file, "\r\n") {
			return errors.New("Files with line breaks in their names need a manually edited ignore rule")
		}
		tracked, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "-z", "--", file)
		if err != nil {
			return err
		}
		if len(tracked) > 0 {
			return errors.New("This file is tracked. Ignoring does not stop tracking; remove it from the index explicitly first")
		}
		rule := "/"
		for _, ch := range file {
			if strings.ContainsRune("\\*?[]!# ", ch) {
				rule += "\\"
			}
			rule += string(ch)
		}
		dir, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer dir.Close()
		if info, e := dir.Lstat(".gitignore"); e == nil && !info.Mode().IsRegular() {
			return errors.New(".gitignore must be a regular file")
		}
		data, err := dir.ReadFile(".gitignore")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(data) > 1024*1024 {
			return errors.New(".gitignore is too large to edit automatically")
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == rule {
				return nil
			}
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		data = append(data, []byte(rule+"\n")...)
		return writeConflictResult(root, ".gitignore", data, 0644)
	})
}
