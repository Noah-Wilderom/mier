package gitclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type AgentWorktree struct {
	Path            string `json:"path"`
	Branch          string `json:"branch"`
	HeadSHA         string `json:"headSha"`
	CommonDirectory string `json:"-"`
}
type AgentFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
	Exists  bool   `json:"exists"`
}
type AgentFileQuery struct {
	Prefix string `json:"prefix"`
	Cursor string `json:"cursor"`
}
type AgentFilePage struct {
	Paths      []string `json:"paths"`
	NextCursor string   `json:"nextCursor"`
	Total      int      `json:"total"`
}

func (c *Client) commonDirectory(ctx context.Context, root string) (string, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(strings.TrimSpace(string(out))))
}
func sameDirectory(first, second string) bool {
	a, e := os.Stat(first)
	if e != nil {
		return false
	}
	b, e := os.Stat(second)
	return e == nil && os.SameFile(a, b)
}

// Pin only a registered secondary worktree at the exact new branch and commit.
// The guard lives in application-owned task state, never in model arguments.
func (c *Client) PinAgentWorktree(ctx context.Context, source, target, branch, head string) (AgentWorktree, error) {
	var result AgentWorktree
	err := c.withRepository(ctx, source, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !strings.HasPrefix(branch, "mier/agent/") || !objectID.MatchString(head) {
			return errors.New("Choose an application-managed agent branch")
		}
		items, err := c.worktrees(ctx, root)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !sameDirectory(item.Path, target) {
				continue
			}
			if item.Primary || item.Branch != branch || item.SHA != head {
				return errors.New("The agent worktree identity changed")
			}
			path, err := filepath.EvalSymlinks(item.Path)
			if err != nil {
				return err
			}
			common, err := c.commonDirectory(ctx, root)
			if err != nil {
				return err
			}
			result = AgentWorktree{Path: path, Branch: branch, HeadSHA: head, CommonDirectory: common}
			return nil
		}
		return errors.New("The agent directory is not a registered worktree")
	})
	return result, err
}
func (c *Client) checkAgentWorktree(ctx context.Context, root string, guard AgentWorktree) error {
	if guard.Path == "" || guard.CommonDirectory == "" || !strings.HasPrefix(guard.Branch, "mier/agent/") || !sameDirectory(root, guard.Path) {
		return errors.New("This task no longer owns the worktree directory")
	}
	common, err := c.commonDirectory(ctx, root)
	if err != nil {
		return err
	}
	if !sameDirectory(common, guard.CommonDirectory) {
		return errors.New("The agent worktree belongs to a different repository")
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return err
	}
	if state.HeadSHA != guard.HeadSHA || state.CurrentBranch != guard.Branch {
		return errors.New("The agent branch or commit changed. Stop the task and review its work")
	}
	op, err := c.operation(ctx, root, state)
	if err != nil {
		return err
	}
	if op != nil {
		return errors.New("Finish the Git operation in this worktree before allowing agent edits")
	}
	return nil
}
func agentPath(value string) (string, error) {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("Choose a relative source path inside the agent worktree")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", errors.New("Agent tools cannot access Git metadata or ambiguous paths")
		}
	}
	return value, nil
}
func agentParents(directory *os.Root, path string) error {
	parts := strings.Split(path, "/")
	for i := 1; i <= len(parts); i++ {
		info, err := directory.Lstat(strings.Join(parts[:i], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Agent file tools do not follow symlinks")
		}
		if i < len(parts) && !info.IsDir() {
			return errors.New("A parent source path is not a directory")
		}
	}
	return nil
}
func readAgentFile(directory *os.Root, path string) (AgentFile, os.FileMode, error) {
	result := AgentFile{Path: path}
	if err := agentParents(directory, path); err != nil {
		return result, 0, err
	}
	file, err := directory.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, 0600, nil
	}
	if err != nil {
		return result, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, 0, err
	}
	if !info.Mode().IsRegular() {
		return result, 0, errors.New("Agent tools support regular source files")
	}
	data, err := io.ReadAll(io.LimitReader(file, 256*1024+1))
	if err != nil {
		return result, 0, err
	}
	if len(data) > 256*1024 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return result, 0, errors.New("This source file is binary or exceeds 256 KiB")
	}
	result.Content = string(data)
	result.Hash = digest(path, result.Content)
	result.Exists = true
	return result, info.Mode().Perm(), nil
}
func (c *Client) agentSourceAllowed(ctx context.Context, root, path string) error {
	if _, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--error-unmatch", "--", path); err == nil {
		return nil
	} else if !isExitCode(err, 1) {
		return err
	}
	// check-ignore accepts file names, not Git pathspec magic. Its parser rejects
	// the global literal-pathspec marker; disable that marker for this read only.
	_, err := c.run(ctx, root, c.options.LocalTimeout, "--no-literal-pathspecs", "check-ignore", "--quiet", "--no-index", "--", path)
	if err == nil {
		return errors.New("Agent tools do not access ignored files")
	}
	if !isExitCode(err, 1) {
		return err
	}
	return nil
}
func (c *Client) AgentFiles(ctx context.Context, guard AgentWorktree, query AgentFileQuery) (AgentFilePage, error) {
	result := AgentFilePage{Paths: []string{}}
	err := c.withRepository(ctx, guard.Path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.checkAgentWorktree(ctx, root, guard); err != nil {
			return err
		}
		args := []string{"ls-files", "--cached", "--others", "--exclude-standard", "-z"}
		if query.Prefix != "" {
			prefix, err := agentPath(strings.TrimSuffix(query.Prefix, "/"))
			if err != nil {
				return err
			}
			args = append(args, "--", prefix)
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, args...)
		if err != nil {
			return err
		}
		paths := []string{}
		key := digest(guard.Path, guard.Branch, guard.HeadSHA, query.Prefix, string(out))
		offset := 0
		if query.Cursor != "" {
			data, err := base64.RawURLEncoding.DecodeString(query.Cursor)
			var cursor historyCursor
			if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Key != key || cursor.Offset < 0 {
				return errors.New("The worktree catalog changed; start a fresh listing")
			}
			offset = cursor.Offset
		}
		for _, path := range strings.Split(string(out), "\x00") {
			if path == "" {
				continue
			}
			if _, err := agentPath(path); err != nil {
				continue
			}
			paths = append(paths, path)
		}
		if offset > len(paths) {
			return errors.New("Invalid worktree catalog cursor")
		}
		result.Total = len(paths)
		end := min(len(paths), offset+200)
		result.Paths = append(result.Paths, paths[offset:end]...)
		if end < len(paths) {
			data, _ := json.Marshal(historyCursor{Offset: end, Key: key})
			result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		}
		return nil
	})
	return result, err
}
func (c *Client) AgentRead(ctx context.Context, guard AgentWorktree, path string) (AgentFile, error) {
	var result AgentFile
	err := c.withRepository(ctx, guard.Path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.checkAgentWorktree(ctx, root, guard); err != nil {
			return err
		}
		selected, err := agentPath(path)
		if err != nil {
			return err
		}
		if err = c.agentSourceAllowed(ctx, root, selected); err != nil {
			return err
		}
		directory, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer directory.Close()
		result, _, err = readAgentFile(directory, selected)
		return err
	})
	return result, err
}
func (c *Client) AgentWrite(ctx context.Context, guard AgentWorktree, path, expectedHash, content string) (AgentFile, error) {
	var result AgentFile
	err := c.withRepository(ctx, guard.Path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.checkAgentWorktree(ctx, root, guard); err != nil {
			return err
		}
		selected, err := agentPath(path)
		if err != nil {
			return err
		}
		if !utf8.ValidString(content) || strings.ContainsRune(content, 0) || len(content) > 256*1024 {
			return errors.New("Use UTF-8 source contents within 256 KiB")
		}
		if err = c.agentSourceAllowed(ctx, root, selected); err != nil {
			return err
		}
		directory, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer directory.Close()
		before, mode, err := readAgentFile(directory, selected)
		if err != nil {
			return err
		}
		if before.Hash != expectedHash {
			return errors.New("This file changed after the agent read it. Read the current file before editing")
		}
		parent := filepath.ToSlash(filepath.Dir(selected))
		if parent != "." {
			if err = directory.MkdirAll(parent, 0700); err != nil {
				return err
			}
		}
		if err = agentParents(directory, selected); err != nil {
			return err
		}
		var random [16]byte
		if _, err = rand.Read(random[:]); err != nil {
			return err
		}
		temporary := filepath.ToSlash(filepath.Join(parent, ".mier-agent-"+hex.EncodeToString(random[:])+".tmp"))
		file, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer directory.Remove(temporary)
		if _, err = file.WriteString(content); err != nil {
			file.Close()
			return err
		}
		if err = file.Chmod(mode); err != nil {
			file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		current, _, err := readAgentFile(directory, selected)
		if err != nil {
			return err
		}
		if current.Hash != expectedHash {
			return errors.New("This file changed during the agent edit; the existing file was preserved")
		}
		if err = c.checkAgentWorktree(ctx, root, guard); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = directory.Rename(temporary, selected); err != nil {
			return err
		}
		result, _, err = readAgentFile(directory, selected)
		return err
	})
	return result, err
}
