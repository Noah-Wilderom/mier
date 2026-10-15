package gitclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Submodule struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	URL         string `json:"url"`
	RecordedSHA string `json:"recordedSha"`
	CurrentSHA  string `json:"currentSha"`
	Branch      string `json:"branch"`
	Initialized bool   `json:"initialized"`
	Changes     int    `json:"changes"`
	Token       string `json:"token"`
	Error       string `json:"error"`
}
type SubmoduleRequest struct {
	Action         string `json:"action"`
	Path           string `json:"path"`
	URL            string `json:"url"`
	Token          string `json:"token"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
}

func validateSubmodulePath(root, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("Choose a repository-relative submodule path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." {
		return "", errors.New("Choose a subdirectory for the submodule")
	}
	if _, err := validateFile(root, clean+"/.mier-directory-check"); err != nil {
		return "", err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if info, err := dir.Lstat(clean); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Submodule directories cannot be symbolic links")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return clean, nil
}
func (c *Client) submodules(ctx context.Context, root string) ([]Submodule, error) {
	items := []Submodule{}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Lstat(".gitmodules")
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(".gitmodules must be a regular file")
	}
	f, err := dir.Open(".gitmodules")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, errors.New(".gitmodules exceeds the 1 MiB configuration limit")
	}
	out, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, data, "config", "--null", "--file", "-", "--get-regexp", `^submodule\..*\.path$`)
	if isExitCode(err, 1) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		key, path, ok := strings.Cut(record, "\n")
		if !ok {
			return nil, errors.New("Invalid submodule path record")
		}
		if len(items) >= 200 {
			return nil, errors.New("The submodule catalog exceeds 200 entries")
		}
		path, err = validateSubmodulePath(root, path)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "submodule."), ".path")
		url, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, data, "config", "--file", "-", "--get", "submodule."+name+".url")
		if err != nil && !isExitCode(err, 1) {
			return nil, err
		}
		item := Submodule{Name: name, Path: path, URL: redactCredentials(strings.TrimSpace(string(url)))}
		index, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z", "--", path)
		if err != nil {
			return nil, err
		}
		for _, entry := range strings.Split(string(index), "\x00") {
			meta, file, ok := strings.Cut(entry, "\t")
			if ok && file == path {
				fields := strings.Fields(meta)
				if len(fields) == 3 && fields[0] == "160000" && fields[2] == "0" {
					item.RecordedSHA = fields[1]
				}
			}
		}
		if item.RecordedSHA == "" {
			item.Error = "Not recorded as a submodule in the index"
		}
		if _, err := dir.Lstat(path + "/.git"); err == nil {
			item.Initialized = true
			state, err := c.status(ctx, filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				item.Error = err.Error()
			} else {
				item.CurrentSHA = state.HeadSHA
				item.Branch = state.CurrentBranch
				item.Changes = len(state.Changes)
			}
		}
		effective, configErr := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--get", "submodule."+name+".url")
		if configErr != nil && !isExitCode(configErr, 1) {
			return nil, configErr
		}
		item.Token = digest(root, string(data), string(effective), item.Name, item.Path, item.RecordedSHA, item.CurrentSHA, item.Branch, strconv.Itoa(item.Changes), item.Error)
		items = append(items, item)
	}
	return items, nil
}
func (c *Client) Submodules(ctx context.Context, path string) ([]Submodule, error) {
	var items []Submodule
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		items, err = c.submodules(ctx, root)
		return err
	})
	return items, err
}
func (c *Client) MutateSubmodule(ctx context.Context, path string, request SubmoduleRequest) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
			return errors.New("The checked-out branch changed. Refresh submodules first")
		}
		if op, err := c.operation(ctx, root, state); err != nil {
			return err
		} else if op != nil {
			return errors.New("Finish or abort the current Git operation first")
		}
		for _, file := range state.Changes {
			if file.Conflict {
				return errors.New("Resolve existing conflicts before managing submodules")
			}
		}
		file, err := validateSubmodulePath(root, request.Path)
		if err != nil {
			return err
		}
		if request.Action == "add" {
			if err = validateRemoteURL(request.URL); err != nil {
				return err
			}
			if _, err = os.Lstat(filepath.Join(root, filepath.FromSlash(file))); err == nil {
				return errors.New("Choose a new submodule directory; existing files are preserved")
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			for _, change := range state.Changes {
				if change.Path == ".gitmodules" && change.Unstaged {
					return errors.New("Stage or preserve the existing .gitmodules edits before adding a submodule")
				}
			}
			_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, request.URL, "submodule", "add", "--", request.URL, file)
			return err
		}
		items, err := c.submodules(ctx, root)
		if err != nil {
			return err
		}
		var selected *Submodule
		for _, item := range items {
			if item.Path == file {
				selected = &item
				break
			}
		}
		if selected == nil || request.Token == "" || selected.Token != request.Token {
			return errors.New("This submodule changed. Refresh its preview")
		}
		if selected.Error != "" {
			return errors.New(selected.Error)
		}
		switch request.Action {
		case "sync":
			_, err = c.run(ctx, root, c.options.LocalTimeout, "submodule", "sync", "--", file)
		case "set-url":
			if err = validateRemoteURL(request.URL); err != nil {
				return err
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "submodule", "set-url", "--", file, request.URL)
		case "deinit":
			if selected.Changes > 0 {
				return errors.New("Commit or stash changes inside this submodule before deinitializing it")
			}
			if selected.Initialized {
				if _, err = c.requireIdleHead(ctx, filepath.Join(root, filepath.FromSlash(file)), selected.CurrentSHA); err != nil {
					return err
				}
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "submodule", "deinit", "--", file)
		case "update":
			if selected.Changes > 0 {
				return errors.New("Commit or stash changes inside this submodule before updating it")
			}
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "submodule", "init", "--", file); err != nil {
				return err
			}
			url, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--get", "submodule."+selected.Name+".url")
			if err != nil {
				return err
			}
			source := strings.TrimSpace(string(url))
			if err = validateRemoteURL(source); err != nil {
				return err
			}
			args := []string{"submodule", "update", "--init", "--checkout"}
			if selected.Initialized {
				child := filepath.Join(root, filepath.FromSlash(file))
				if _, err = c.requireIdleHead(ctx, child, selected.CurrentSHA); err != nil {
					return err
				}
				if _, err = c.run(ctx, child, c.options.LocalTimeout, "cat-file", "-e", selected.RecordedSHA+"^{commit}"); err != nil {
					remote, err := c.selectRemote(ctx, child, "origin")
					if err != nil {
						return err
					}
					if err = validateRemoteURL(remote.fetchSource); err != nil {
						return err
					}
					if _, err = c.authenticatedRun(ctx, child, c.options.RemoteTimeout, remote.fetchSource, "fetch", "--no-tags", "--", remote.Name); err != nil {
						return err
					}
					if _, err = c.run(ctx, child, c.options.LocalTimeout, "cat-file", "-e", selected.RecordedSHA+"^{commit}"); err != nil {
						if _, err = c.authenticatedRun(ctx, child, c.options.RemoteTimeout, remote.fetchSource, "fetch", "--no-tags", "--", remote.Name, selected.RecordedSHA); err != nil {
							return err
						}
					}
				}
				if err = c.protectResetPaths(ctx, child, selected.CurrentSHA, selected.RecordedSHA); err != nil {
					return err
				}
				if selected.CurrentSHA != selected.RecordedSHA {
					if err = c.checkpoint(ctx, child, selected.CurrentSHA, "submodule-update"); err != nil {
						return err
					}
				}
				args = append(args, "--no-fetch")
			}
			_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, source, append(args, "--", file)...)
		default:
			return fmt.Errorf("Unknown submodule action %q", request.Action)
		}
		return err
	})
}
