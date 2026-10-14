package gitclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

type ConflictVersion struct {
	Exists  bool   `json:"exists"`
	SHA     string `json:"sha"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
}

type ConflictFile struct {
	Path          string          `json:"path"`
	Token         string          `json:"token"`
	Base          ConflictVersion `json:"base"`
	Current       ConflictVersion `json:"current"`
	Incoming      ConflictVersion `json:"incoming"`
	Working       string          `json:"working"`
	Editable      bool            `json:"editable"`
	Reason        string          `json:"reason"`
	CurrentLabel  string          `json:"currentLabel"`
	IncomingLabel string          `json:"incomingLabel"`
}

type ResolveConflictRequest struct {
	Path    string `json:"path"`
	Token   string `json:"token"`
	Choice  string `json:"choice"` // result, current, incoming, delete
	Content string `json:"content"`
}

func (c *Client) Conflict(ctx context.Context, path, file string) (ConflictFile, error) {
	var result ConflictFile
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.conflict(ctx, root, file)
		return err
	})
	return result, err
}

func (c *Client) conflict(ctx context.Context, root, file string) (ConflictFile, error) {
	selected, err := validateFile(root, file)
	if err != nil {
		return ConflictFile{}, err
	}
	out, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--unmerged", "-z", "--", selected)
	if err != nil {
		return ConflictFile{}, err
	}
	if len(out) == 0 {
		return ConflictFile{}, errors.New("This file is no longer conflicted. Refresh local changes")
	}
	result := ConflictFile{Path: selected, Editable: true, CurrentLabel: "Current version", IncomingLabel: "Incoming version"}
	stages := []*ConflictVersion{nil, &result.Base, &result.Current, &result.Incoming}
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || name != selected || len(fields) != 3 || len(fields[2]) != 1 || fields[2][0] < '1' || fields[2][0] > '3' || !objectID.MatchString(fields[1]) {
			return ConflictFile{}, errors.New("Git returned invalid conflict stages")
		}
		version := stages[int(fields[2][0]-'0')]
		version.Exists, version.Mode, version.SHA = true, fields[0], fields[1]
		if version.Mode != "100644" && version.Mode != "100755" {
			result.Editable, result.Reason = false, "This conflict changes a symbolic link or submodule. Choose a complete version or resolve it in your editor."
		}
		if version.Mode == "160000" {
			continue
		}
		size, err := c.run(ctx, root, c.options.LocalTimeout, "cat-file", "-s", version.SHA)
		if err != nil {
			return ConflictFile{}, err
		}
		var bytes int64
		if _, err := fmt.Sscan(string(size), &bytes); err != nil {
			return ConflictFile{}, err
		}
		if bytes > c.options.MaxFileBytes {
			version.Binary = true
			result.Editable, result.Reason = false, "This file exceeds the text editor size limit. Choose a complete version."
			continue
		}
		data, err := c.run(ctx, root, c.options.LocalTimeout, "cat-file", "blob", version.SHA)
		if err != nil {
			return ConflictFile{}, err
		}
		version.Binary = strings.IndexByte(string(data), 0) >= 0 || !utf8.Valid(data)
		if version.Binary {
			result.Editable, result.Reason = false, "Binary content cannot be merged as text. Choose a complete version."
		} else {
			version.Content = string(data)
		}
	}
	working, mode, err := c.conflictWorking(root, selected)
	if err != nil {
		return ConflictFile{}, err
	}
	if strings.IndexByte(working, 0) >= 0 || !utf8.ValidString(working) || mode&os.ModeSymlink != 0 {
		result.Editable = false
		if result.Reason == "" {
			result.Reason = "This working file cannot be edited as text. Choose a complete version."
		}
	} else {
		result.Working = working
	}
	status, err := c.status(ctx, root)
	if err != nil {
		return ConflictFile{}, err
	}
	op, err := c.operation(ctx, root, status)
	if err != nil {
		return ConflictFile{}, err
	}
	opToken := status.HeadSHA
	if op != nil {
		result.CurrentLabel, result.IncomingLabel, opToken = op.CurrentLabel, op.SourceLabel, op.Token
	}
	result.Token = digest(root, selected, string(out), working, fmt.Sprint(mode), opToken)
	return result, nil
}

func (c *Client) conflictWorking(root, file string) (string, os.FileMode, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return "", 0, err
	}
	defer directory.Close()
	info, err := directory.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := directory.Readlink(file)
		return target, info.Mode(), err
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New("Only regular conflict files can be resolved here")
	}
	f, err := directory.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	// Hash large working files without sending their bytes through RPC.
	if info.Size() > c.options.MaxFileBytes {
		if info.Size() > 64*1024*1024 {
			return "", 0, errors.New("This conflict exceeds the 64 MiB resolution limit. Resolve it in an external editor")
		}
		h := sha256.New()
		if _, err := io.Copy(h, io.LimitReader(f, 64*1024*1024+1)); err != nil {
			return "", 0, err
		}
		return "\x00large:" + hex.EncodeToString(h.Sum(nil)), info.Mode(), nil
	}
	data, err := io.ReadAll(io.LimitReader(f, c.options.MaxFileBytes+1))
	return string(data), info.Mode(), err
}

// ResolveConflict compares the exact index stages and on-disk version before
// writing. An editor or another Git client cannot be silently overwritten.
func (c *Client) ResolveConflict(ctx context.Context, path string, request ResolveConflictRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		file, err := c.conflict(ctx, root, request.Path)
		if err != nil {
			return err
		}
		if request.Token == "" || file.Token != request.Token {
			return errors.New("This conflict changed on disk. Reload it before saving; your edited result is still in the editor")
		}
		if request.Choice != "result" && request.Choice != "current" && request.Choice != "incoming" && request.Choice != "delete" {
			return errors.New("Choose a conflict resolution")
		}
		if request.Choice == "result" {
			if !file.Editable {
				return errors.New(file.Reason)
			}
			if int64(len(request.Content)) > c.options.MaxFileBytes || !utf8.ValidString(request.Content) || strings.IndexByte(request.Content, 0) >= 0 {
				return errors.New("The result must be valid text within the editor size limit")
			}
			if hasConflictMarkers(request.Content) {
				return errors.New("Resolve all conflict markers before saving")
			}
			mode := os.FileMode(0644)
			if file.Current.Mode == "100755" || (!file.Current.Exists && file.Incoming.Mode == "100755") {
				mode = 0755
			}
			if err := writeConflictResult(root, file.Path, []byte(request.Content), mode); err != nil {
				return err
			}
		} else {
			version, stage := file.Current, "2"
			if request.Choice == "incoming" {
				version, stage = file.Incoming, "3"
			}
			if request.Choice == "delete" || !version.Exists {
				directory, err := os.OpenRoot(root)
				if err != nil {
					return err
				}
				err = directory.Remove(file.Path)
				directory.Close()
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			} else {
				if version.Mode == "160000" {
					return errors.New("Resolve submodule commits in a Git editor before staging")
				}
				if _, err := c.run(ctx, root, c.options.LocalTimeout, "checkout-index", "--force", "--stage="+stage, "--", file.Path); err != nil {
					return err
				}
			}
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "add", "--", file.Path); err != nil {
			return fmt.Errorf("The result was saved, but could not be staged. Refresh before retrying: %w", err)
		}
		return nil
	})
}

func writeConflictResult(root, file string, content []byte, mode os.FileMode) error {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	if info, err := directory.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return errors.New("The result path must be a regular file")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(file), ".mier-merge-"+hex.EncodeToString(nonce[:]))
	f, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer directory.Remove(temporary)
	_, writeErr := f.Write(content)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return directory.Rename(temporary, file)
}

var conflictMarker = regexp.MustCompile(`(?m)^(?:<{7,}|={7,}|>{7,}|\|{7,})(?:[ \t\r].*)?$`)

func hasConflictMarkers(content string) bool { return conflictMarker.MatchString(content) }
