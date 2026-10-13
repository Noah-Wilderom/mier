package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (c *Client) CheckoutPullRequest(ctx context.Context, path string, number int) error {
	if number <= 0 || number > 2147483647 {
		return errors.New("Choose a valid pull request number")
	}
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		remote, err := c.selectRemote(ctx, root, "origin")
		if err != nil {
			return err
		}
		if err := validateRemoteURL(remote.fetchSource); err != nil {
			return err
		}
		ref := "refs/pull/" + strconv.Itoa(number) + "/head"
		if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.fetchSource, "fetch", "--no-tags", "--", remote.Name, ref); err != nil {
			return err
		}
		output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
		if err != nil {
			return err
		}
		fetched := strings.TrimSpace(string(output))
		branch := "pr-" + strconv.Itoa(number)
		existing, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err == nil {
			if strings.TrimSpace(string(existing)) != fetched {
				return fmt.Errorf("branch %s already exists at a different commit; its work has been preserved", branch)
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--", branch)
			return err
		}
		if !isExitCode(err, 1) {
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--create", branch, "--", fetched)
		return err
	})
}

func (c *Client) resolveRevision(ctx context.Context, root, revision string) (string, error) {
	if revision != "HEAD" {
		if err := c.validateBranch(ctx, root, revision); err != nil {
			return "", err
		}
	}
	output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// Compare uses the merge base, matching a GitHub pull request's file comparison.
func (c *Client) Compare(ctx context.Context, path, base, head string) ([]ChangedFile, error) {
	files := []ChangedFile{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		baseSHA, err := c.resolveRevision(ctx, root, base)
		if err != nil {
			return err
		}
		headSHA, err := c.resolveRevision(ctx, root, head)
		if err != nil {
			return err
		}
		comparison := baseSHA + "..." + headSHA
		output, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", "-z", comparison, "--")
		if err != nil {
			return err
		}
		records := strings.Split(string(output), "\x00")
		for i := 0; i < len(records); {
			if records[i] == "" {
				i++
				continue
			}
			code := records[i]
			i++
			if i >= len(records) {
				return errors.New("Git returned an incomplete comparison record")
			}
			changed := ChangedFile{Status: diffStatus(code[:1]), Lines: []DiffLine{}}
			paths := []string{}
			if strings.HasPrefix(code, "R") || strings.HasPrefix(code, "C") {
				if i+1 >= len(records) {
					return errors.New("Git returned an incomplete comparison rename")
				}
				changed.PreviousPath, changed.Path = records[i], records[i+1]
				paths = []string{changed.Path, changed.PreviousPath}
				i += 2
			} else {
				changed.Path = records[i]
				paths = []string{changed.Path}
				i++
			}
			if len(files) >= 2000 {
				return errors.New("The branch comparison exceeds 2000 changed files")
			}
			args := []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "-U5", comparison, "--"}
			patch, err := c.run(ctx, root, c.options.LocalTimeout, append(args, paths...)...)
			if err != nil {
				return err
			}
			changed.RawPatch = string(patch)
			if err := parsePatch(&changed); err != nil {
				return err
			}
			files = append(files, changed)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
