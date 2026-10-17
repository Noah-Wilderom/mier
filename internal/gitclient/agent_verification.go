package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func (c *Client) AgentWorktreeStamp(ctx context.Context, guard AgentWorktree) (string, error) {
	var stamp string
	err := c.withRepository(ctx, guard.Path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		stamp, err = c.agentWorktreeStamp(ctx, root, guard)
		return err
	})
	return stamp, err
}
func (c *Client) agentWorktreeStamp(ctx context.Context, root string, guard AgentWorktree) (string, error) {
	if err := c.checkAgentWorktree(ctx, root, guard); err != nil {
		return "", err
	}
	out, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	paths := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	sort.Strings(paths)
	if len(paths) > 20000 {
		return "", errors.New("Verification supports up to 20,000 source files")
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00", guard.HeadSHA, guard.Branch)
	var total int64
	previous := ""
	buffer := make([]byte, 64*1024)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if path == "" || path == previous {
			continue
		}
		previous = path
		if _, err = agentPath(path); err != nil {
			return "", err
		}
		if err = agentParents(directory, path); err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00", path)
		info, err := directory.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprint(hash, "deleted\x00")
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := directory.Readlink(path)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(hash, "link:%s\x00", target)
			continue
		}
		if info.IsDir() {
			return "", errors.New("Review nested Git worktrees or submodules separately before running an approved check")
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("Verification cannot fingerprint a non-regular source file")
		}
		total += info.Size()
		if total > 1024*1024*1024 {
			return "", errors.New("Verification source exceeds its 1 GiB fingerprint limit")
		}
		fmt.Fprintf(hash, "%d:%d\x00", info.Mode().Perm(), info.Size())
		f, err := directory.Open(path)
		if err != nil {
			return "", err
		}
		written, err := io.CopyBuffer(hash, io.LimitReader(f, info.Size()+1), buffer)
		updated, statErr := f.Stat()
		f.Close()
		if err != nil {
			return "", err
		}
		if statErr != nil {
			return "", statErr
		}
		if written != info.Size() || updated.Size() != info.Size() || !updated.ModTime().Equal(info.ModTime()) {
			return "", errors.New("Source changed while preparing check approval")
		}
		fmt.Fprint(hash, "\x00")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
