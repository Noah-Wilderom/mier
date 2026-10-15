package gitclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type LFSPattern struct {
	Pattern  string `json:"pattern"`
	Source   string `json:"source"`
	Lockable bool   `json:"lockable"`
}
type LFSFile struct {
	Name       string `json:"name"`
	OID        string `json:"oid"`
	Size       int64  `json:"size"`
	Downloaded bool   `json:"downloaded"`
}
type LFSState struct {
	Installed   bool         `json:"installed"`
	Version     string       `json:"version"`
	Initialized bool         `json:"initialized"`
	Patterns    []LFSPattern `json:"patterns"`
	Files       []LFSFile    `json:"files"`
	Summary     string       `json:"summary"`
	Token       string       `json:"token"`
}
type LFSRequest struct {
	Action         string `json:"action"`
	Pattern        string `json:"pattern"`
	Filename       bool   `json:"filename"`
	Lockable       bool   `json:"lockable"`
	Remote         string `json:"remote"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
}
type LFSLock struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Owner struct {
		Name string `json:"name"`
	} `json:"owner"`
	LockedAt string `json:"locked_at"`
}

func (c *Client) lfs(ctx context.Context, root string) (LFSState, error) {
	result := LFSState{Patterns: []LFSPattern{}, Files: []LFSFile{}}
	version, err := c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "version")
	if err != nil {
		return result, nil
	}
	result.Installed = true
	result.Version = strings.TrimSpace(string(version))
	filter, _ := c.run(ctx, root, c.options.LocalTimeout, "config", "--get", "filter.lfs.process")
	result.Initialized = strings.TrimSpace(string(filter)) != ""
	patterns, err := c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "track", "--json")
	var catalog struct {
		Patterns []LFSPattern `json:"patterns"`
	}
	if err == nil && json.Unmarshal(patterns, &catalog) == nil {
		result.Patterns = catalog.Patterns
	} else {
		patterns, err = c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "track")
		if err != nil {
			return result, err
		}
		result.Summary = "Tracked patterns:\n" + string(patterns)
	}
	files, err := c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "ls-files", "--json")
	var listing struct {
		Files []LFSFile `json:"files"`
	}
	if err == nil && json.Unmarshal(files, &listing) == nil {
		result.Files = listing.Files
	} else {
		files, err = c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "ls-files", "--long")
		if err != nil {
			return result, err
		}
		result.Summary += "\nLFS files:\n" + string(files)
	}
	status, err := c.run(ctx, root, c.options.LocalTimeout, "-c", "alias.lfs=", "lfs", "status")
	if err != nil {
		return result, err
	}
	result.Summary += "\n" + string(status)
	_, attrs, err := c.workingVersion(ctx, root, ".gitattributes")
	if err != nil {
		return result, err
	}
	head, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	result.Token = digest(root, head.HeadSHA, head.CurrentBranch, attrs, string(filter), string(patterns), string(files), string(status))
	if result.Patterns == nil {
		result.Patterns = []LFSPattern{}
	}
	if result.Files == nil {
		result.Files = []LFSFile{}
	}
	return result, nil
}
func (c *Client) LFS(ctx context.Context, path string) (LFSState, error) {
	var result LFSState
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.lfs(ctx, root)
		return err
	})
	return result, err
}
func (c *Client) MutateLFS(ctx context.Context, path string, request LFSRequest) (string, error) {
	var output string
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		if _, err := c.requireIdleHead(ctx, root, request.ExpectedHead); err != nil {
			return err
		}
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if state.CurrentBranch != request.ExpectedBranch {
			return errors.New("The checked-out branch changed. Refresh Git LFS")
		}
		current, err := c.lfs(ctx, root)
		if err != nil {
			return err
		}
		if !current.Installed {
			return errors.New("Install Git LFS from git-lfs.com, then refresh this page")
		}
		if current.Token != request.Token || request.Token == "" {
			return errors.New("Git LFS or repository files changed. Refresh before continuing")
		}
		args := []string{"-c", "alias.lfs=", "lfs"}
		network := false
		switch request.Action {
		case "install":
			args = append(args, "install", "--local")
		case "track", "untrack":
			if request.Pattern == "" || strings.ContainsAny(request.Pattern, "\x00\r\n") || strings.HasPrefix(request.Pattern, "-") || len(request.Pattern) > 4096 {
				return errors.New("Choose a nonempty pattern without line breaks or leading options")
			}
			dir, err := os.OpenRoot(root)
			if err != nil {
				return err
			}
			defer dir.Close()
			if info, err := dir.Lstat(".gitattributes"); err == nil && !info.Mode().IsRegular() {
				return errors.New(".gitattributes must be a regular file")
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			args = append(args, request.Action)
			if request.Action == "track" {
				if request.Filename {
					if _, err = validateFile(root, request.Pattern); err != nil {
						return err
					}
					args = append(args, "--filename")
				}
				if request.Lockable {
					args = append(args, "--lockable")
				}
			}
			args = append(args, request.Pattern)
		case "checkout":
			args = append(args, "checkout")
		case "pull", "fetch":
			args = append(args, request.Action)
			network = true
		case "lock", "unlock":
			if _, err = validateFile(root, request.Pattern); err != nil {
				return err
			}
			if strings.HasPrefix(request.Pattern, "-") {
				return errors.New("Choose a file path without a leading option")
			}
			args = append(args, request.Action, "--remote="+request.Remote, request.Pattern)
			network = true
		default:
			return errors.New("Unknown Git LFS action")
		}
		var out []byte
		if network {
			remote, err := c.selectRemote(ctx, root, request.Remote)
			if err != nil {
				return err
			}
			if err = validateRemoteURL(remote.fetchSource); err != nil {
				return err
			}
			if request.Action == "pull" || request.Action == "fetch" {
				args = append(args, remote.Name)
			}
			out, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.fetchSource, args...)
			if err != nil {
				return err
			}
		} else {
			out, err = c.run(ctx, root, c.options.LocalTimeout, args...)
			if err != nil {
				return err
			}
		}
		output = strings.TrimSpace(string(out))
		return nil
	})
	return output, err
}
func (c *Client) LFSLocks(ctx context.Context, path, remoteName string) ([]LFSLock, error) {
	items := []LFSLock{}
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		remote, err := c.selectRemote(ctx, root, remoteName)
		if err != nil {
			return err
		}
		if err = validateRemoteURL(remote.fetchSource); err != nil {
			return err
		}
		out, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.fetchSource, "-c", "alias.lfs=", "lfs", "locks", "--json", "--remote="+remote.Name)
		if err != nil {
			return err
		}
		var result struct {
			Locks []LFSLock `json:"locks"`
		}
		if err = json.Unmarshal(out, &result); err != nil {
			return err
		}
		if result.Locks != nil {
			items = result.Locks
		}
		return nil
	})
	return items, err
}
