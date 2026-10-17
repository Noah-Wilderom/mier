package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ConflictForecastRequest struct {
	Target         string `json:"target"`
	ExpectedTarget string `json:"expectedTarget"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Rebase         bool   `json:"rebase"`
}
type RebasePreviewStep struct {
	Commit    string   `json:"commit"`
	Clean     bool     `json:"clean"`
	Conflicts []string `json:"conflicts"`
}
type ConflictForecast struct {
	Head           string              `json:"head"`
	Target         string              `json:"target"`
	Base           string              `json:"base"`
	Clean          bool                `json:"clean"`
	Conflicts      []string            `json:"conflicts"`
	Overlap        []string            `json:"overlap"`
	Rebase         []RebasePreviewStep `json:"rebase"`
	RebaseComplete bool                `json:"rebaseComplete"`
	Note           string              `json:"note"`
}

func (c *Client) forecastTree(ctx context.Context, root, left, right, base string) (string, []string, error) {
	args := []string{"merge-tree", "--write-tree", "--name-only", "-z"}
	if base != "" {
		args = append(args, "--merge-base="+base)
	}
	args = append(args, left, right)
	out, err := c.executeInputResult(ctx, root, c.options.LocalTimeout, nil, nil, nil, args...)
	if err != nil && !isExitCode(err, 1) {
		return "", nil, err
	}
	fields := strings.Split(string(out), "\x00")
	if len(fields) < 2 || !objectID.MatchString(fields[0]) {
		return "", nil, errors.New("Conflict forecasts require Git 2.38 or newer")
	}
	files := []string{}
	if err != nil {
		for _, path := range fields[1:] {
			if path == "" {
				break
			}
			files = append(files, path)
		}
	}
	return fields[0], files, nil
}

// ForecastConflict writes only disposable Git objects. No worktree, branch,
// index, hooks, or remotes are changed; installed hooks are never invoked.
func (c *Client) ForecastConflict(ctx context.Context, path string, request ConflictForecastRequest) (ConflictForecast, error) {
	result := ConflictForecast{Overlap: []string{}, Conflicts: []string{}, Rebase: []RebasePreviewStep{}}
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !state.HasHead || state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
			return errors.New("The current branch changed. Refresh the conflict forecast")
		}
		target, err := c.resolveRevision(ctx, root, request.Target)
		if err != nil {
			return err
		}
		if target != request.ExpectedTarget {
			return errors.New("The target branch changed. Refresh the conflict forecast")
		}
		base, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", state.HeadSHA, target)
		if err != nil {
			return err
		}
		result.Head = state.HeadSHA
		result.Target = target
		result.Base = strings.TrimSpace(string(base))
		if !objectID.MatchString(result.Base) {
			return errors.New("Git returned an invalid common ancestor")
		}
		own, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "-z", result.Base, state.HeadSHA, "--")
		if err != nil {
			return err
		}
		theirs, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "-z", result.Base, target, "--")
		if err != nil {
			return err
		}
		changed := map[string]bool{}
		for _, file := range strings.Split(string(own), "\x00") {
			if file != "" {
				changed[file] = true
			}
		}
		for _, file := range strings.Split(string(theirs), "\x00") {
			if changed[file] {
				result.Overlap = append(result.Overlap, file)
			}
		}
		_, result.Conflicts, err = c.forecastTree(ctx, root, state.HeadSHA, target, "")
		if err != nil {
			return err
		}
		result.Clean = len(result.Conflicts) == 0
		if request.Rebase {
			out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--reverse", "--topo-order", "--parents", target+".."+state.HeadSHA, "--")
			if err != nil {
				return err
			}
			commits := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(commits) > 50 {
				return errors.New("Rebase previews are limited to 50 commits. Select a closer target")
			}
			cursor := target
			result.RebaseComplete = true
			for _, line := range commits {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				if len(fields) > 2 {
					result.Note = "Preview skips merge commits, matching rebase without preserving merges."
					continue
				}
				if len(fields) != 2 {
					return errors.New("Cannot preview rebasing a root commit")
				}
				tree, conflicts, err := c.forecastTree(ctx, root, cursor, fields[0], fields[1])
				if err != nil {
					return err
				}
				result.Rebase = append(result.Rebase, RebasePreviewStep{fields[0], len(conflicts) == 0, conflicts})
				if len(conflicts) > 0 {
					result.RebaseComplete = false
					break
				}
				// An unattached synthetic commit lets the next step use the actual tree
				// produced by its predecessor without starting a real rebase.
				commit, err := c.run(ctx, root, c.options.LocalTimeout, "-c", "user.name=M.I.E.R. preview", "-c", "user.email=preview@mier.invalid", "-c", "commit.gpgsign=false", "commit-tree", tree, "-p", cursor, "-m", "Temporary rebase preview")
				if err != nil {
					return fmt.Errorf("Simulate rebase step: %w", err)
				}
				cursor = strings.TrimSpace(string(commit))
				if !objectID.MatchString(cursor) {
					return errors.New("Git returned an invalid simulated commit")
				}
			}
		}
		fresh, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		latest, err := c.resolveRevision(ctx, root, request.Target)
		if err != nil {
			return err
		}
		if fresh.HeadSHA != result.Head || fresh.CurrentBranch != request.ExpectedBranch || latest != target {
			return errors.New("Branches changed during the forecast. Refresh to check the current commits")
		}
		return nil
	})
	return result, err
}
