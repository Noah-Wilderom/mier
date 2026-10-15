package gitclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Checkpoint struct {
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Label     string `json:"label"`
	CreatedAt string `json:"createdAt"`
}
type ReflogEntry struct {
	SHA      string `json:"sha"`
	Selector string `json:"selector"`
	Message  string `json:"message"`
	Actor    string `json:"actor"`
}
type ResetRequest struct {
	SHA            string `json:"sha"`
	Mode           string `json:"mode"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
}
type RecoverBranchRequest struct {
	SHA  string `json:"sha"`
	Name string `json:"name"`
}

func (c *Client) checkpoint(ctx context.Context, root, sha, label string) error {
	if !objectID.MatchString(sha) {
		return errors.New("A checkpoint needs an existing commit")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	ref := "refs/mier/checkpoints/" + time.Now().UTC().Format("20060102T150405.000000000") + "-" + label + "-" + hex.EncodeToString(nonce[:])
	_, err := c.run(ctx, root, c.options.LocalTimeout, "update-ref", "--create-reflog", "-m", "M.I.E.R.: before "+label, ref, sha, strings.Repeat("0", len(sha)))
	return err
}
func (c *Client) Checkpoints(ctx context.Context, path string) ([]Checkpoint, error) {
	items := []Checkpoint{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--sort=-refname", "--count=200", "--format=%(refname)%00%(objectname)", "refs/mier/checkpoints/")
		if err != nil {
			return err
		}
		for _, row := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if row == "" {
				continue
			}
			fields := strings.Split(row, "\x00")
			if len(fields) != 2 {
				return errors.New("Invalid checkpoint record")
			}
			name := strings.TrimPrefix(fields[0], "refs/mier/checkpoints/")
			parts := strings.Split(name, "-")
			label, created := name, ""
			if len(parts) >= 3 {
				label = strings.Join(parts[1:len(parts)-1], " ")
				if when, err := time.Parse("20060102T150405.000000000", parts[0]); err == nil {
					created = when.UTC().Format(time.RFC3339)
				}
			}
			items = append(items, Checkpoint{fields[0], fields[1], label, created})
		}
		return nil
	})
	return items, err
}
func (c *Client) Reflog(ctx context.Context, path string) ([]ReflogEntry, error) {
	items := []ReflogEntry{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !state.HasHead {
			return nil
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "reflog", "show", "-200", "-z", "--format=%H%x00%gd%x00%gs%x00%gn", "HEAD")
		if err != nil {
			return err
		}
		fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		if len(fields)%4 != 0 {
			return errors.New("Invalid reflog record")
		}
		for i := 0; i < len(fields); i += 4 {
			items = append(items, ReflogEntry{fields[i], fields[i+1], fields[i+2], fields[i+3]})
		}
		return nil
	})
	return items, err
}
func (c *Client) Reset(ctx context.Context, path string, request ResetRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if request.Mode != "soft" && request.Mode != "mixed" && request.Mode != "hard" {
			return errors.New("Choose soft, mixed, or hard reset")
		}
		if !objectID.MatchString(request.SHA) {
			return errors.New("Choose a full commit ID")
		}
		if err := c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		if _, err := c.resolveRevision(ctx, root, request.SHA); err != nil {
			return err
		}
		if request.Mode == "hard" {
			if err := c.protectResetPaths(ctx, root, request.ExpectedHead, request.SHA); err != nil {
				return err
			}
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "reset-"+request.Mode); err != nil {
			return err
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "reset", "--"+request.Mode, request.SHA, "--"); err != nil {
			return err
		}
		var err error
		result, err = c.operationResult(ctx, root)
		return err
	})
	return result, err
}

// status excludes ignored files. Check every incoming path and its parents so
// reset --hard cannot erase an ignored file/directory hidden by a clean status.
func (c *Client) protectResetPaths(ctx context.Context, root, head, target string) error {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "--diff-filter=ACMRT", "-z", head, target, "--")
	if err != nil {
		return err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, file := range strings.Split(string(out), "\x00") {
		if file == "" {
			continue
		}
		for part := file; part != "." && part != ""; part = filepath.ToSlash(filepath.Dir(part)) {
			info, err := dir.Lstat(part)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if info.IsDir() {
				if part != file {
					continue
				}
				ignored, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--", part)
				if err != nil {
					return err
				}
				if len(ignored) > 0 {
					return fmt.Errorf("Reset would replace ignored files in %s. Move them aside first", part)
				}
				continue
			}
			entries, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z", "--", part)
			if err != nil {
				return err
			}
			tracked := false
			for _, entry := range strings.Split(string(entries), "\x00") {
				if _, name, ok := strings.Cut(entry, "\t"); ok && name == part {
					tracked = true
				}
			}
			if !tracked {
				return fmt.Errorf("Reset would replace an untracked or ignored path: %s. Move it aside first", part)
			}
		}
	}
	return nil
}
func (c *Client) RecoverBranch(ctx context.Context, path string, request RecoverBranchRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !objectID.MatchString(request.SHA) {
			return errors.New("Choose an immutable commit ID")
		}
		if err := c.validateBranch(ctx, root, request.Name); err != nil {
			return err
		}
		if _, err := c.resolveRevision(ctx, root, request.SHA); err != nil {
			return err
		}
		_, err := c.run(ctx, root, c.options.LocalTimeout, "branch", "--", request.Name, request.SHA)
		return err
	})
}

type CherryPickManyRequest struct {
	SHAs           []string `json:"shas"`
	ExpectedHead   string   `json:"expectedHead"`
	ExpectedBranch string   `json:"expectedBranch"`
}

func (c *Client) CherryPickMany(ctx context.Context, path string, request CherryPickManyRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		if len(request.SHAs) < 1 || len(request.SHAs) > 100 {
			return errors.New("Choose between 1 and 100 commits")
		}
		if err := c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, sha := range request.SHAs {
			if !objectID.MatchString(sha) || seen[sha] {
				return errors.New("Choose distinct full commit IDs")
			}
			seen[sha] = true
			out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--parents", "-n", "1", sha, "--")
			if err != nil {
				return err
			}
			if len(strings.Fields(string(out))) > 2 {
				return errors.New("Cherry-pick merge commits individually so you can choose their mainline parent")
			}
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "cherry-pick"); err != nil {
			return err
		}
		_, commandErr := c.execute(ctx, root, c.options.RemoteTimeout, []string{"GIT_EDITOR=true"}, nil, append([]string{"cherry-pick", "-x"}, request.SHAs...)...)
		var err error
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if commandErr != nil && result.Operation == nil {
			return commandErr
		}
		return nil
	})
	return result, err
}
func (c *Client) Revert(ctx context.Context, path string, request CherryPickRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !objectID.MatchString(request.SHA) {
			return errors.New("Choose a full commit ID")
		}
		if err := c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--parents", "-n", "1", request.SHA, "--")
		if err != nil {
			return err
		}
		parents := len(strings.Fields(string(out))) - 1
		if parents > 1 && (request.Mainline < 1 || request.Mainline > parents) {
			return errors.New("Choose a mainline parent for this merge commit")
		}
		if parents <= 1 && request.Mainline != 0 {
			return errors.New("Mainline applies only to a merge commit")
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "revert"); err != nil {
			return err
		}
		args := []string{"revert", "--no-edit"}
		if request.Mainline > 0 {
			args = append(args, "--mainline", fmt.Sprint(request.Mainline))
		}
		args = append(args, request.SHA)
		_, commandErr := c.execute(ctx, root, c.options.LocalTimeout, []string{"GIT_EDITOR=true"}, nil, args...)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if commandErr != nil && result.Operation == nil {
			return commandErr
		}
		return nil
	})
	return result, err
}
