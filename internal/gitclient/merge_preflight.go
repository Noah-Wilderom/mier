package gitclient

import (
	"context"
	"errors"
	"strings"
)

type MergePreflight struct {
	Head        string   `json:"head"`
	Source      string   `json:"source"`
	Clean       bool     `json:"clean"`
	FastForward bool     `json:"fastForward"`
	Conflicts   []string `json:"conflicts"`
}

func (c *Client) PreviewMerge(ctx context.Context, path string, request MergeBranchRequest) (MergePreflight, error) {
	result := MergePreflight{Conflicts: []string{}}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch || !state.HasHead {
			return errors.New("The destination branch changed. Preview the merge again")
		}
		if err = c.validateBranch(ctx, root, request.Branch); err != nil {
			return err
		}
		source, err := c.resolveRevision(ctx, root, "refs/heads/"+request.Branch)
		if err != nil {
			return err
		}
		if source != request.ExpectedSource {
			return errors.New("The source branch changed. Preview the merge again")
		}
		out, commandErr := c.executeInputResult(ctx, root, c.options.LocalTimeout, nil, nil, nil, "merge-tree", "--write-tree", "--name-only", "-z", state.HeadSHA, source)
		if commandErr != nil && !isExitCode(commandErr, 1) {
			return commandErr
		}
		fields := strings.Split(string(out), "\x00")
		if len(fields) < 2 || !objectID.MatchString(fields[0]) {
			return errors.New("Git returned an invalid merge preview; Git 2.38 or newer is required")
		}
		result.Head, result.Source, result.Clean = state.HeadSHA, source, commandErr == nil
		if !result.Clean {
			for _, file := range fields[1:] {
				if file == "" {
					break
				}
				result.Conflicts = append(result.Conflicts, file)
			}
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", state.HeadSHA, source)
		result.FastForward = err == nil
		if err != nil && !isExitCode(err, 1) {
			return err
		}
		return nil
	})
	return result, err
}
