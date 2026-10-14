package gitclient

import (
	"context"
	"errors"
	"strings"
)

type StageHunkRequest struct {
	Path          string `json:"path"`
	Staged        bool   `json:"staged"`
	Hunk          int    `json:"hunk"`
	ExpectedPatch string `json:"expectedPatch"`
}

// StageHunk changes only the index. The fresh Git patch must match the preview;
// arbitrary caller-supplied patch content is never passed to Git.
func (c *Client) StageHunk(ctx context.Context, path string, request StageHunkRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		file, err := validateFile(root, request.Path)
		if err != nil {
			return err
		}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		eligible := false
		for _, change := range status.Changes {
			if change.Path != file {
				continue
			}
			code := change.WorktreeStatus
			if request.Staged {
				code = change.IndexStatus
			}
			eligible = code == "M" && !change.Conflict && change.OriginalPath == "" && change.Submodule == "N..."
		}
		if !eligible {
			return errors.New("Partial staging is available for modified text files. Stage additions, deletions, renames, and conflicts as whole files")
		}
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "-U5"}
		if request.Staged {
			args = append(args, "--cached")
		}
		args = append(args, "--", file)
		patch, err := c.run(ctx, root, c.options.LocalTimeout, args...)
		if err != nil {
			return err
		}
		if request.ExpectedPatch == "" || string(patch) != request.ExpectedPatch {
			return errors.New("This diff changed. Refresh it before staging a section")
		}
		selected, err := selectHunk(string(patch), request.Hunk)
		if err != nil {
			return err
		}
		apply := []string{"apply", "--cached"}
		if request.Staged {
			apply = append(apply, "--reverse")
		}
		if _, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(selected), append(apply, "--check")...); err != nil {
			return err
		}
		_, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(selected), apply...)
		return err
	})
}

func selectHunk(patch string, selected int) (string, error) {
	if selected < 0 || strings.Contains(patch, "Binary files ") || strings.Contains(patch, "GIT binary patch") {
		return "", errors.New("Choose a text diff section")
	}
	lines := strings.SplitAfter(patch, "\n")
	header := ""
	var hunks []string
	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			hunks = append(hunks, line)
		} else if len(hunks) == 0 {
			header += line
		} else {
			hunks[len(hunks)-1] += line
		}
	}
	if selected >= len(hunks) {
		return "", errors.New("This diff section is no longer available")
	}
	return header + hunks[selected], nil
}
