package gitclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Worktree struct {
	Path       string `json:"path"`
	SHA        string `json:"sha"`
	Branch     string `json:"branch"`
	Token      string `json:"token"`
	Primary    bool   `json:"primary"`
	Current    bool   `json:"current"`
	Locked     bool   `json:"locked"`
	LockReason string `json:"lockReason"`
	Prunable   bool   `json:"prunable"`
	Changes    int    `json:"changes"`
	Error      string `json:"error"`
}
type WorktreeRequest struct {
	Action         string `json:"action"`
	Path           string `json:"path"`
	Mode           string `json:"mode"`
	Branch         string `json:"branch"`
	StartSHA       string `json:"startSha"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
	Reason         string `json:"reason"`
}

func (c *Client) worktrees(ctx context.Context, root string) ([]Worktree, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	items := []Worktree{}
	var item *Worktree
	for _, field := range strings.Split(string(out), "\x00") {
		if field == "" {
			item = nil
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		if key == "worktree" {
			items = append(items, Worktree{Path: value, Primary: len(items) == 0})
			item = &items[len(items)-1]
		}
		if item == nil {
			continue
		}
		switch key {
		case "HEAD":
			item.SHA = value
		case "branch":
			item.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "locked":
			item.Locked = true
			item.LockReason = value
		case "prunable":
			item.Prunable = true
		}
	}
	for i := range items {
		item := &items[i]
		canonical := filepath.Clean(item.Path)
		if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
			canonical = resolved
		}
		item.Current = canonical == root
		if !item.Prunable {
			state, err := c.status(ctx, item.Path)
			if err != nil {
				item.Error = err.Error()
			} else {
				item.Changes = len(state.Changes)
			}
		}
		item.Token = digest(item.Path, item.SHA, item.Branch, item.LockReason, item.Error, strconv.Itoa(item.Changes), strconv.FormatBool(item.Prunable), map[bool]string{true: "locked", false: "unlocked"}[item.Locked])
	}
	return items, nil
}
func (c *Client) Worktrees(ctx context.Context, path string) ([]Worktree, error) {
	var items []Worktree
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		items, err = c.worktrees(ctx, root)
		return err
	})
	return items, err
}
func (c *Client) MutateWorktree(ctx context.Context, path string, request WorktreeRequest) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		if request.Path == "" || strings.ContainsRune(request.Path, 0) || !filepath.IsAbs(request.Path) {
			return errors.New("Choose an absolute worktree directory")
		}
		if request.Action == "add" {
			state, err := c.status(ctx, root)
			if err != nil {
				return err
			}
			if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
				return errors.New("The source branch changed. Refresh before creating a worktree")
			}
			if !objectID.MatchString(request.StartSHA) {
				return errors.New("Choose a full starting commit ID")
			}
			if _, err = c.resolveRevision(ctx, root, request.StartSHA); err != nil {
				return err
			}
			target := filepath.Clean(request.Path)
			if _, err = os.Lstat(target); err == nil {
				return errors.New("Choose a new directory; existing directories are preserved")
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			canonicalTarget := target
			parent := filepath.Dir(target)
			for {
				resolved, err := filepath.EvalSymlinks(parent)
				if err == nil {
					relative, err := filepath.Rel(parent, target)
					if err != nil {
						return err
					}
					canonicalTarget = filepath.Join(resolved, relative)
					if inside(root, canonicalTarget) {
						return errors.New("Create worktrees outside the current repository directory")
					}
					break
				}
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				next := filepath.Dir(parent)
				if next == parent {
					return errors.New("Cannot resolve the worktree parent")
				}
				parent = next
			}
			items, err := c.worktrees(ctx, root)
			if err != nil {
				return err
			}
			for _, item := range items {
				registered := item.Path
				if resolved, e := filepath.EvalSymlinks(registered); e == nil {
					registered = resolved
				}
				if inside(registered, canonicalTarget) || inside(canonicalTarget, registered) {
					return errors.New("Worktrees cannot contain or replace another registered worktree")
				}
			}
			args := []string{"worktree", "add"}
			switch request.Mode {
			case "new":
				if err = c.validateBranch(ctx, root, request.Branch); err != nil {
					return err
				}
				args = append(args, "-b", request.Branch)
			case "existing":
				if err = c.validateBranch(ctx, root, request.Branch); err != nil {
					return err
				}
				sha, err := c.resolveRevision(ctx, root, "refs/heads/"+request.Branch)
				if err != nil {
					return err
				}
				if sha != request.StartSHA {
					return errors.New("The selected branch changed. Preview it again")
				}
			case "detached":
				args = append(args, "--detach")
			default:
				return errors.New("Choose a new branch, existing branch, or detached worktree")
			}
			args = append(args, "--", target)
			if request.Mode == "existing" {
				args = append(args, request.Branch)
			} else {
				args = append(args, request.StartSHA)
			}
			_, err = c.run(ctx, root, c.options.RemoteTimeout, args...)
			return err
		}
		items, err := c.worktrees(ctx, root)
		if err != nil {
			return err
		}
		var selected *Worktree
		requestedPath := filepath.Clean(request.Path)
		if resolved, resolveErr := filepath.EvalSymlinks(requestedPath); resolveErr == nil {
			requestedPath = resolved
		}
		for _, item := range items {
			itemPath := filepath.Clean(item.Path)
			if resolved, resolveErr := filepath.EvalSymlinks(itemPath); resolveErr == nil {
				itemPath = resolved
			}
			samePath := itemPath == requestedPath
			if !samePath {
				a, aErr := os.Stat(itemPath)
				b, bErr := os.Stat(requestedPath)
				samePath = aErr == nil && bErr == nil && os.SameFile(a, b)
			}
			if samePath {
				selected = &item
				break
			}
		}
		if selected == nil || request.Token == "" || selected.Token != request.Token {
			return errors.New("This worktree changed. Refresh the list before trying again")
		}
		if selected.Primary {
			return errors.New("The primary worktree cannot be locked or removed here")
		}
		switch request.Action {
		case "lock":
			if len(request.Reason) > 2000 || strings.ContainsRune(request.Reason, 0) {
				return errors.New("Use a shorter lock reason")
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "worktree", "lock", "--reason", request.Reason, "--", selected.Path)
		case "unlock":
			_, err = c.run(ctx, root, c.options.LocalTimeout, "worktree", "unlock", "--", selected.Path)
		case "remove":
			if selected.Current || selected.Locked || selected.Changes > 0 || selected.Error != "" {
				return errors.New("Open another worktree and commit or stash this worktree's changes, then unlock it before removal")
			}
			_, err = c.run(ctx, root, c.options.RemoteTimeout, "worktree", "remove", "--", selected.Path)
		default:
			return errors.New("Choose add, lock, unlock, or remove")
		}
		return err
	})
}
