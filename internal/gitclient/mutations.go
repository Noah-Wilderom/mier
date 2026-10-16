package gitclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (c *Client) Stage(ctx context.Context, path string, files []string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		paths, err := validateFiles(root, files)
		if err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		for _, change := range status.Changes {
			if !change.Conflict {
				continue
			}
			for _, path := range paths {
				if path != change.Path {
					continue
				}
				working, _, err := c.conflictWorking(root, path)
				if err != nil {
					return err
				}
				if hasConflictMarkers(working) {
					return errors.New("Resolve this file's conflict markers before staging")
				}
			}
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, append([]string{"add", "--"}, paths...)...)
		return err
	})
}

func (c *Client) Unstage(ctx context.Context, path string, files []string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		paths, err := validateFiles(root, files)
		if err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		selected := make(map[string]bool, len(paths))
		for _, path := range paths {
			selected[path] = true
		}
		for _, change := range status.Changes {
			if selected[change.Path] && change.IndexStatus == "R" && change.OriginalPath != "" {
				original, err := validateFile(root, change.OriginalPath)
				if err != nil {
					return err
				}
				selected[original] = true
			}
		}
		paths = paths[:0]
		for path := range selected {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		command := []string{"restore", "--staged", "--"}
		if !status.HasHead {
			command = []string{"rm", "--cached", "--force", "--ignore-unmatch", "--"}
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, append(command, paths...)...)
		return err
	})
}

func (c *Client) Commit(ctx context.Context, path, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" || strings.IndexByte(message, 0) >= 0 {
		return "", errors.New("Enter a commit message")
	}
	if len(message) > 100*1024 {
		return "", errors.New("Commit messages cannot exceed 100 KiB")
	}
	var sha string
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		op, err := c.operation(ctx, root, status)
		if err != nil {
			return err
		}
		if op != nil {
			return errors.New("Use Continue to finish the current Git operation")
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "commit", "-m", message); err != nil {
			return err
		}
		output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return err
		}
		sha = strings.TrimSpace(string(output))
		return nil
	})
	return sha, err
}

func (c *Client) CreateBranch(ctx context.Context, path, branch string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		_, err := c.run(ctx, root, c.options.LocalTimeout, "branch", "--", branch)
		return err
	})
}

// CreateAndCheckoutBranch creates and selects a branch in one locked operation.
// On a new repository, naming HEAD preserves the index and working files.
func (c *Client) CreateAndCheckoutBranch(ctx context.Context, path, branch string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !status.HasHead {
			if _, err := c.run(ctx, root, c.options.LocalTimeout, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
				return fmt.Errorf("branch %q already exists", branch)
			} else if !isExitCode(err, 1) {
				return err
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "symbolic-ref", "HEAD", "refs/heads/"+branch)
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--create", branch, "--no-track", "HEAD")
		return err
	})
}

// DeleteBranch removes only a local branch whose commits are reachable from
// the checked-out HEAD. Git also rejects branches used by another worktree.
func (c *Client) DeleteBranch(ctx context.Context, path, branch string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if branch == status.CurrentBranch {
			return errors.New("Switch to another branch before deleting this one")
		}
		if !status.HasHead {
			return errors.New("Create a commit before deleting branches")
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", "refs/heads/"+branch, "HEAD"); err != nil {
			if isExitCode(err, 1) {
				return errors.New("This branch has commits that are not merged into the current branch; its work has been preserved")
			}
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "branch", "--delete", "--", branch)
		return err
	})
}

func (c *Client) RenameBranch(ctx context.Context, path, branch, name string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		if err := c.validateBranch(ctx, root, name); err != nil {
			return err
		}
		_, err := c.run(ctx, root, c.options.LocalTimeout, "branch", "--move", "--", branch, name)
		return err
	})
}

func (c *Client) Checkout(ctx context.Context, path, branch string) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		_, err := c.run(ctx, root, c.options.LocalTimeout, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		if err == nil {
			_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--", branch)
			return err
		}
		if !isExitCode(err, 1) {
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "show-ref", "--verify", "--quiet", "refs/remotes/"+branch)
		if err != nil {
			return fmt.Errorf("branch %q is not available locally: %w", branch, err)
		}
		remotes, err := c.remotes(ctx, root)
		if err != nil {
			return err
		}
		sort.Slice(remotes, func(i, j int) bool { return len(remotes[i].Name) > len(remotes[j].Name) })
		for _, remote := range remotes {
			if !strings.HasPrefix(branch, remote.Name+"/") {
				continue
			}
			local := strings.TrimPrefix(branch, remote.Name+"/")
			if err := c.validateBranch(ctx, root, local); err != nil {
				return err
			}
			_, exists := c.run(ctx, root, c.options.LocalTimeout, "show-ref", "--verify", "--quiet", "refs/heads/"+local)
			if exists == nil {
				return fmt.Errorf("local branch %q already exists; select that branch directly", local)
			}
			if !isExitCode(exists, 1) {
				return exists
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--track", "--create", local, "--", "refs/remotes/"+branch)
			return err
		}
		return errors.New("The remote branch does not have a configured remote")
	})
}

type selectedRemote struct {
	Remote
	fetchSource string
	pushSource  string
}

func (c *Client) selectRemote(ctx context.Context, root, name string) (selectedRemote, error) {
	if strings.HasPrefix(name, "-") || strings.IndexByte(name, 0) >= 0 {
		return selectedRemote{}, errors.New("Choose a valid configured remote")
	}
	remotes, err := c.remotes(ctx, root)
	if err != nil {
		return selectedRemote{}, err
	}
	if name == "" {
		for _, remote := range remotes {
			if remote.Name == "origin" {
				name = remote.Name
				break
			}
		}
		if name == "" && len(remotes) == 1 {
			name = remotes[0].Name
		}
		if name == "" {
			return selectedRemote{}, errors.New("Choose a remote for this operation")
		}
	}
	for _, remote := range remotes {
		if remote.Name != name {
			continue
		}
		result := selectedRemote{Remote: remote}
		read := func(key string) (string, error) {
			output, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--null", "--get-all", key)
			if isExitCode(err, 1) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			return strings.SplitN(string(output), "\x00", 2)[0], nil
		}
		result.fetchSource, err = read("remote." + name + ".url")
		if err != nil {
			return selectedRemote{}, err
		}
		result.pushSource, err = read("remote." + name + ".pushurl")
		if err != nil {
			return selectedRemote{}, err
		}
		if result.pushSource == "" {
			result.pushSource = result.fetchSource
		}
		if expanded, expandErr := c.run(ctx, root, c.options.LocalTimeout, "remote", "get-url", "--", name); expandErr == nil {
			result.fetchSource = strings.TrimSpace(string(expanded))
		}
		if expanded, expandErr := c.run(ctx, root, c.options.LocalTimeout, "remote", "get-url", "--push", "--", name); expandErr == nil {
			result.pushSource = strings.TrimSpace(string(expanded))
		}
		return result, nil
	}
	return selectedRemote{}, fmt.Errorf("remote %q is not configured in this repository", name)
}
func validateRemoteURL(raw string) error {
	if raw == "" || strings.HasPrefix(raw, "-") || strings.IndexByte(raw, 0) >= 0 {
		return errors.New("Enter a valid Git remote URL")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return errors.New("Enter a valid Git remote URL")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("Git repository URLs cannot contain a query or fragment")
		}
		if strings.EqualFold(parsed.Scheme, "http") && parsed.User != nil {
			return errors.New("Use HTTPS when credentials are supplied in a repository URL")
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ssh", "git":
			if parsed.Hostname() == "" || strings.HasPrefix(parsed.Hostname(), "-") {
				return errors.New("Enter a valid remote host")
			}
		case "file":
		default:
			return errors.New("This Git transport is not supported")
		}
		return nil
	}
	if match := scpRemote.FindStringSubmatch(raw); len(match) == 3 && !filepath.IsAbs(raw) {
		// A second colon after the transport name requests a remote helper;
		// colons inside a bracketed IPv6 host or a local path do not.
		if strings.HasPrefix(match[2], ":") {
			return errors.New("External Git helper transports are not supported")
		}
		if strings.HasPrefix(match[1], "-") {
			return errors.New("Enter a valid remote host")
		}
	}
	return nil
}

func (c *Client) Fetch(ctx context.Context, path, remote string) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		selected, err := c.selectRemote(ctx, root, remote)
		if err != nil {
			return err
		}
		if err := validateRemoteURL(selected.fetchSource); err != nil {
			return err
		}
		_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, selected.fetchSource, "fetch", "--prune", "--", selected.Name)
		return err
	})
}

func (c *Client) Pull(ctx context.Context, path, remote string) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		selected, err := c.selectRemote(ctx, root, remote)
		if err != nil {
			return err
		}
		if err := validateRemoteURL(selected.fetchSource); err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !status.HasHead || status.Detached {
			return errors.New("Check out a branch with an upstream before pulling")
		}
		output, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+status.CurrentBranch)
		if err != nil {
			return err
		}
		upstream := strings.SplitN(strings.TrimSuffix(string(output), "\n"), "\x00", 2)
		if len(upstream) != 2 || upstream[0] == "" || upstream[1] == "" {
			return errors.New("This branch does not have an upstream; push it or configure tracking first")
		}
		if upstream[0] != selected.Name {
			return fmt.Errorf("this branch tracks %s; pull from that remote", upstream[0])
		}
		_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, selected.fetchSource, "pull", "--ff-only", "--", selected.Name, upstream[1])
		return err
	})
}

func (c *Client) Push(ctx context.Context, path, remote string) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		selected, err := c.selectRemote(ctx, root, remote)
		if err != nil {
			return err
		}
		if err := validateRemoteURL(selected.pushSource); err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !status.HasHead || status.Detached {
			return errors.New("Check out a branch before pushing")
		}
		if err := c.validateBranch(ctx, root, status.CurrentBranch); err != nil {
			return err
		}
		destination := "refs/heads/" + status.CurrentBranch
		output, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+status.CurrentBranch)
		if err != nil {
			return err
		}
		upstream := strings.SplitN(strings.TrimSuffix(string(output), "\n"), "\x00", 2)
		if len(upstream) == 2 && upstream[0] == selected.Name && strings.HasPrefix(upstream[1], "refs/heads/") {
			destination = upstream[1]
		}
		_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, selected.pushSource, "push", "--porcelain", "--set-upstream", "--", selected.Name, "HEAD:"+destination)
		return err
	})
}

func (c *Client) Clone(ctx context.Context, remoteURL, directory string) (RepositorySnapshot, error) {
	if err := validateRemoteURL(remoteURL); err != nil {
		return RepositorySnapshot{}, err
	}
	commandURL := remoteURL
	if parsed, err := url.Parse(remoteURL); err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.User = nil
		commandURL = parsed.String()
	}
	if directory == "" || strings.IndexByte(directory, 0) >= 0 {
		return RepositorySnapshot{}, errors.New("Choose a destination directory")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	if info, err := os.Stat(absolute); err == nil {
		if !info.IsDir() {
			return RepositorySnapshot{}, errors.New("The clone destination must be a directory")
		}
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return RepositorySnapshot{}, err
		}
		if len(entries) > 0 {
			return RepositorySnapshot{}, errors.New("The clone destination must be empty")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RepositorySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.CloneTimeout)
	defer cancel()
	release, err := c.acquire(ctx, absolute)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	_, err = c.authenticatedRun(ctx, "", c.options.CloneTimeout, remoteURL, "clone", "--", commandURL, absolute)
	release()
	if err != nil {
		return RepositorySnapshot{}, err
	}
	return c.Snapshot(ctx, absolute)
}
