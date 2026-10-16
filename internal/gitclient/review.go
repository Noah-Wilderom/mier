package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (c *Client) CheckoutPullRequest(ctx context.Context, path string, number int, remoteName ...string) error {
	if number <= 0 || number > 2147483647 {
		return errors.New("Choose a valid pull request number")
	}
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		name := "origin"
		if len(remoteName) > 0 {
			name = remoteName[0]
		}
		remote, err := c.selectRemote(ctx, root, name)
		if err != nil {
			return err
		}
		if err := validateRemoteURL(remote.fetchSource); err != nil {
			return err
		}
		source, err := c.pullRequestSource(ctx, root, remote, number)
		if err != nil {
			return err
		}
		ref := source.HeadRef
		if ref == "" {
			ref = source.HeadSHA
		}
		if ref == "" {
			return errors.New("This provider did not return a source branch or commit")
		}
		expected := ""
		if len(remoteName) > 1 {
			expected = remoteName[1]
		}
		if expected != "" && (!objectID.MatchString(expected) || source.HeadSHA != "" && source.HeadSHA != expected) {
			return errors.New("The pull request changed. Refresh before checking it out")
		}
		if _, fetchErr := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, source.HeadURL, "fetch", "--no-tags", "--", source.HeadURL, ref); fetchErr != nil {
			fallback := source.HeadSHA
			if fallback == "" {
				fallback = expected
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fallback == "" || fallback == ref {
				return fetchErr
			}
			if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, source.HeadURL, "fetch", "--no-tags", "--", source.HeadURL, fallback); err != nil {
				return err
			}
		}
		output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
		if err != nil {
			return err
		}
		fetched := strings.TrimSpace(string(output))
		if expected != "" && fetched != expected {
			return errors.New("The source branch changed while fetching. Refresh the pull request before checking it out")
		}
		branch := "pr-" + strconv.Itoa(number)
		existing, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err == nil {
			if strings.TrimSpace(string(existing)) != fetched {
				return fmt.Errorf("branch %s already exists at a different commit; its work has been preserved", branch)
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--no-overwrite-ignore", "--", branch)
			return err
		}
		if !isExitCode(err, 1) {
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-guess", "--no-overwrite-ignore", "--create", branch, "--", fetched)
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
		files, err = c.diffRevisions(ctx, root, []string{baseSHA + "..." + headSHA})
		return err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// CommitDiff compares a commit to its first parent. Root commits compare with
// the empty tree, and merge commits show what the merge brought to that parent.
// No checkout, index, or object database writes are required.
func (c *Client) CommitDiff(ctx context.Context, path, revision string) ([]ChangedFile, error) {
	var files []ChangedFile
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		sha, err := c.resolveRevision(ctx, root, revision)
		if err != nil {
			return err
		}
		output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--parents", "-n", "1", sha, "--")
		if err != nil {
			return err
		}
		parents := strings.Fields(string(output))
		var base string
		if len(parents) > 1 {
			base = parents[1]
		} else {
			empty, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte{}, "hash-object", "-t", "tree", "--stdin")
			if err != nil {
				return err
			}
			base = strings.TrimSpace(string(empty))
		}
		files, err = c.diffRevisions(ctx, root, []string{base, sha})
		return err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (c *Client) diffRevisions(ctx context.Context, root string, comparison []string) ([]ChangedFile, error) {
	files := []ChangedFile{}
	args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--name-status", "-z"}, comparison...)
	output, err := c.run(ctx, root, c.options.LocalTimeout, append(args, "--")...)
	if err != nil {
		return nil, err
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
			return nil, errors.New("Git returned an incomplete comparison record")
		}
		changed := ChangedFile{Status: diffStatus(code[:1]), Lines: []DiffLine{}}
		paths := []string{}
		if strings.HasPrefix(code, "R") || strings.HasPrefix(code, "C") {
			if i+1 >= len(records) {
				return nil, errors.New("Git returned an incomplete comparison rename")
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
			return nil, errors.New("The comparison exceeds 2000 changed files")
		}
		args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "-U5"}, comparison...)
		args = append(args, "--")
		patch, err := c.run(ctx, root, c.options.LocalTimeout, append(args, paths...)...)
		if err != nil {
			return nil, err
		}
		changed.RawPatch = string(patch)
		if err := parsePatch(&changed); err != nil {
			return nil, err
		}
		files = append(files, changed)
	}
	return files, nil
}
