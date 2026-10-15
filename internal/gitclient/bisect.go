package gitclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type BisectRequest struct {
	Action         string `json:"action"`
	Good           string `json:"good"`
	Bad            string `json:"bad"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
}

var bisectResultLine = regexp.MustCompile(`(?m)^# first (?:'[^'\r\n]+'|[^\s]+) commit: \[([a-f0-9]{40}|[a-f0-9]{64})\]`)

func bisectResult(log string) string {
	match := bisectResultLine.FindStringSubmatch(log)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func (c *Client) bisect(ctx context.Context, root string, request BisectRequest) (OperationResult, error) {
	state, err := c.status(ctx, root)
	if err != nil {
		return OperationResult{}, err
	}
	op, err := c.operation(ctx, root, state)
	if err != nil {
		return OperationResult{}, err
	}
	if request.Action == "start" {
		if err = c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return OperationResult{}, err
		}
		if !objectID.MatchString(request.Good) || !objectID.MatchString(request.Bad) || request.Good == request.Bad {
			return OperationResult{}, errors.New("Choose different full good and bad commit IDs")
		}
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", request.Good, request.Bad); err != nil {
			return OperationResult{}, errors.New("The known good commit must be an ancestor of the bad commit")
		}
		if err = c.checkpoint(ctx, root, state.HeadSHA, "bisect-start"); err != nil {
			return OperationResult{}, err
		}
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "bisect", "start", "--no-checkout", request.Bad, request.Good, "--"); err != nil {
			return OperationResult{}, err
		}
	} else {
		if op == nil || op.Kind != "bisect" || op.Token != request.Token || request.Token == "" {
			return OperationResult{}, errors.New("The bisect session changed. Refresh before marking a result")
		}
		if len(state.Changes) > 0 {
			return OperationResult{}, errors.New("Commit, stash, or discard test edits before moving to another bisect commit")
		}
		if request.Action == "reset" {
			return c.resetBisect(ctx, root, state)
		}
		if request.Action != "good" && request.Action != "bad" && request.Action != "skip" && request.Action != "checkout" {
			return OperationResult{}, errors.New("Choose good, bad, skip, check out, or end bisect")
		}
		if request.Action != "checkout" {
			if op.BisectDone {
				return OperationResult{}, errors.New("Bisection has finished. Review its log and end the session")
			}
			if op.SourceSHA != state.HeadSHA {
				return OperationResult{}, errors.New("Check out the proposed bisect commit before marking it")
			}
			// Adopt safe, explicit checkout for sessions begun in another Git client too.
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "update-ref", "BISECT_HEAD", op.SourceSHA); err != nil {
				return OperationResult{}, err
			}
			_, err = c.executeInputResult(ctx, root, c.options.LocalTimeout, nil, nil, nil, "bisect", request.Action, op.SourceSHA)
			if err != nil && !isExitCode(err, 2) {
				return OperationResult{}, err
			}
		}
	}
	state, err = c.status(ctx, root)
	if err != nil {
		return OperationResult{}, err
	}
	next, err := c.operation(ctx, root, state)
	if err != nil {
		return OperationResult{}, err
	}
	if next != nil && next.BisectResult != "" {
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "update-ref", "BISECT_HEAD", next.BisectResult); err != nil {
			return OperationResult{}, err
		}
	}
	candidate, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "BISECT_HEAD^{commit}")
	if err != nil {
		return OperationResult{}, err
	}
	sha := strings.TrimSpace(string(candidate))
	if !objectID.MatchString(sha) {
		return OperationResult{}, errors.New("Git did not return a valid bisect commit")
	}
	if _, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--detach", "--no-overwrite-ignore", sha); err != nil {
		return OperationResult{}, err
	}
	return c.operationResult(ctx, root)
}
func (c *Client) resetBisect(ctx context.Context, root string, state RepositorySnapshot) (OperationResult, error) {
	if len(state.Changes) > 0 {
		return OperationResult{}, errors.New("Preserve test edits before ending bisect")
	}
	out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return OperationResult{}, err
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "BISECT_START"))
	if err != nil {
		return OperationResult{}, err
	}
	original := strings.TrimSpace(string(data))
	args := []string{"switch", "--no-overwrite-ignore"}
	if objectID.MatchString(original) {
		args = append(args, "--detach", original)
	} else {
		if err = c.validateBranch(ctx, root, original); err != nil {
			return OperationResult{}, err
		}
		args = append(args, "--no-guess", "--", original)
	}
	if _, err = c.run(ctx, root, c.options.LocalTimeout, args...); err != nil {
		return OperationResult{}, err
	}
	if _, err = c.run(ctx, root, c.options.LocalTimeout, "bisect", "reset", "HEAD"); err != nil {
		return OperationResult{}, err
	}
	return c.operationResult(ctx, root)
}
func (c *Client) Bisect(ctx context.Context, path string, request BisectRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.bisect(ctx, root, request)
		return err
	})
	return result, err
}
