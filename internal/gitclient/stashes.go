package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Stash struct {
	SHA       string `json:"sha"`
	Ref       string `json:"ref"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}
type SaveStashRequest struct {
	Message          string `json:"message"`
	Mode             string `json:"mode"`
	IncludeUntracked bool   `json:"includeUntracked"`
	ExpectedHead     string `json:"expectedHead"`
}
type StashRequest struct {
	SHA          string `json:"sha"`
	Action       string `json:"action"`
	RestoreIndex bool   `json:"restoreIndex"`
	ExpectedHead string `json:"expectedHead"`
}

func (c *Client) stashes(ctx context.Context, root string) ([]Stash, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "stash", "list", "-z", "--format=%H%x00%gd%x00%gs%x00%cI")
	if err != nil {
		return nil, err
	}
	items := []Stash{}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		return items, nil
	}
	if len(fields)%4 != 0 {
		return nil, errors.New("Git returned an invalid stash list")
	}
	for i := 0; i < len(fields); i += 4 {
		items = append(items, Stash{fields[i], fields[i+1], fields[i+2], fields[i+3]})
	}
	return items, nil
}
func (c *Client) Stashes(ctx context.Context, path string) ([]Stash, error) {
	var items []Stash
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		items, err = c.stashes(ctx, root)
		return err
	})
	return items, err
}
func (c *Client) findStash(ctx context.Context, root, sha string) (Stash, error) {
	if !objectID.MatchString(sha) {
		return Stash{}, errors.New("Choose a stash from the list")
	}
	items, err := c.stashes(ctx, root)
	if err != nil {
		return Stash{}, err
	}
	for _, item := range items {
		if item.SHA == sha {
			return item, nil
		}
	}
	return Stash{}, errors.New("This stash no longer exists. Refresh the list")
}
func (c *Client) StashDiff(ctx context.Context, path, sha string) (string, error) {
	var patch string
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if _, err := c.findStash(ctx, root, sha); err != nil {
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "stash", "show", "--include-untracked", "--no-ext-diff", "--no-textconv", "--no-color", "--patch", sha)
		patch = string(out)
		return err
	})
	return patch, err
}
func (c *Client) requireIdleHead(ctx context.Context, root, head string) (RepositorySnapshot, error) {
	state, err := c.status(ctx, root)
	if err != nil {
		return state, err
	}
	if !objectID.MatchString(head) || state.HeadSHA != head {
		return state, errors.New("HEAD changed. Refresh before trying again")
	}
	op, err := c.operation(ctx, root, state)
	if err != nil {
		return state, err
	}
	if op != nil {
		return state, fmt.Errorf("Finish or abort the current %s first", op.Kind)
	}
	for _, file := range state.Changes {
		if file.Conflict {
			return state, errors.New("Resolve conflicts first")
		}
	}
	return state, nil
}
func (c *Client) SaveStash(ctx context.Context, path string, request SaveStashRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		state, err := c.requireIdleHead(ctx, root, request.ExpectedHead)
		if err != nil {
			return err
		}
		if len(state.Changes) == 0 {
			return errors.New("There are no local changes to stash")
		}
		if len(request.Message) > 10000 || strings.ContainsRune(request.Message, 0) {
			return errors.New("Choose a shorter stash message")
		}
		// Git stash invokes clean internally. Inherited literal-pathspec mode
		// prevents its generated cleanup pathspecs from matching untracked files.
		// This command accepts no caller pathspecs, so clear that mode here only.
		args := []string{"--no-literal-pathspecs", "stash", "push", "--message", request.Message}
		switch request.Mode {
		case "all":
		case "keep-index":
			args = append(args, "--keep-index")
		case "staged":
			if request.IncludeUntracked {
				return errors.New("Staged-only stashes cannot include untracked files")
			}
			args = append(args, "--staged")
		default:
			return errors.New("Choose a stash mode")
		}
		if request.IncludeUntracked {
			args = append(args, "--include-untracked")
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, args...)
		return err
	})
}
func (c *Client) MutateStash(ctx context.Context, path string, request StashRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if _, err := c.requireIdleHead(ctx, root, request.ExpectedHead); err != nil {
			return err
		}
		item, err := c.findStash(ctx, root, request.SHA)
		if err != nil {
			return err
		}
		switch request.Action {
		case "apply", "pop":
			args := []string{"--no-literal-pathspecs", "stash", "apply"}
			if request.RestoreIndex {
				args = append(args, "--index")
			}
			args = append(args, item.SHA)
			if _, err := c.run(ctx, root, c.options.LocalTimeout, args...); err != nil {
				return fmt.Errorf("Stash kept. Inspect Local changes before retrying: %w", err)
			}
			if request.Action == "apply" {
				return nil
			}
			// Resolve the reflog position again after applying. Never pop a shifted
			// stash slot or drop anything when application has conflicts.
			item, err = c.findStash(ctx, root, request.SHA)
			if err != nil {
				return fmt.Errorf("Stash applied; could not remove its list entry: %w", err)
			}
		case "drop":
		default:
			return errors.New("Choose apply, pop, or drop")
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "stash", "drop", item.Ref)
		return err
	})
}

type AmendRequest struct {
	Message      string `json:"message"`
	ExpectedHead string `json:"expectedHead"`
}

func (c *Client) Amend(ctx context.Context, path string, request AmendRequest) (string, error) {
	var sha string
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if _, err := c.requireIdleHead(ctx, root, request.ExpectedHead); err != nil {
			return err
		}
		message := strings.TrimSpace(request.Message)
		if message == "" || len(message) > 100*1024 || strings.ContainsRune(message, 0) {
			return errors.New("Enter a commit message of at most 100 KiB")
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "amend"); err != nil {
			return err
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "commit", "--amend", "-m", message); err != nil {
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "HEAD")
		sha = strings.TrimSpace(string(out))
		return err
	})
	return sha, err
}
