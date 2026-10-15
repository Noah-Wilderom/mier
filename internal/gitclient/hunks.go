package gitclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type StageHunkRequest struct {
	Path          string `json:"path"`
	Staged        bool   `json:"staged"`
	Hunk          int    `json:"hunk"`
	ExpectedPatch string `json:"expectedPatch"`
	// Lines are zero-based rows within Hunk, including context rows. An omitted
	// list selects the whole hunk. Only changed rows may be selected.
	Lines []int `json:"lines,omitempty"`
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
		if strings.Contains(string(patch), "old mode ") {
			return errors.New("Stage permission changes as a whole file")
		}
		selected, err := selectHunk(string(patch), request.Hunk)
		if err != nil {
			return err
		}
		apply := []string{"apply", "--cached"}
		if len(request.Lines) > 0 {
			selected, err = selectPatchLines(selected, request.Lines, request.Staged)
			if err != nil {
				return err
			}
		} else if request.Staged {
			apply = append(apply, "--reverse")
		}
		if _, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(selected), append(apply, "--check")...); err != nil {
			return err
		}
		_, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, []byte(selected), apply...)
		return err
	})
}

// Build a patch from the source side, retaining unselected removals as context
// and omitting unselected additions. For unstaging the index is the source,
// so the patch is reversed before selection. The working file is never read
// from the caller and is never written by partial staging.
func selectPatchLines(patch string, rows []int, reverse bool) (string, error) {
	lines := strings.SplitAfter(patch, "\n")
	header, body := "", ""
	oldStart, oldCount, newCount, row, changes := 0, 0, 0, 0, 0
	selected := make(map[int]bool, len(rows))
	for _, n := range rows {
		if n < 0 || selected[n] {
			return "", errors.New("Choose distinct changed lines")
		}
		selected[n] = true
	}
	inHunk, emitted := false, false
	for _, line := range lines {
		if match := hunkHeader.FindStringSubmatch(line); len(match) == 3 {
			if inHunk {
				return "", errors.New("Select lines in one section at a time")
			}
			inHunk = true
			start := match[1]
			if reverse {
				start = match[2]
			}
			oldStart, _ = strconv.Atoi(start)
			continue
		}
		if !inHunk {
			header += line
			continue
		}
		if strings.HasPrefix(line, "\\ No newline") {
			if emitted {
				body += line
			}
			continue
		}
		if line == "" {
			continue
		}
		kind := line[0]
		if kind != '+' && kind != '-' && kind != ' ' {
			return "", errors.New("Unsupported patch line")
		}
		chosen := selected[row]
		if chosen && kind == ' ' {
			return "", errors.New("Select changed lines, not context")
		}
		delete(selected, row)
		row++
		if reverse {
			if kind == '+' {
				kind = '-'
			} else if kind == '-' {
				kind = '+'
			}
		}
		emitted = true
		switch {
		case kind == '+' && !chosen:
			emitted = false
		case kind == '-' && !chosen:
			body += " " + line[1:]
			oldCount++
			newCount++
		default:
			body += string(kind) + line[1:]
			if kind != '+' {
				oldCount++
			}
			if kind != '-' {
				newCount++
			}
			if kind != ' ' {
				changes++
			}
		}
	}
	if !inHunk || changes == 0 || len(selected) > 0 {
		return "", errors.New("The selected lines are no longer available")
	}
	newStart := oldStart
	if oldCount == 0 {
		newStart++
	}
	if newCount == 0 {
		newStart--
	}
	return header + fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount) + body, nil
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
