package gitclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func (c *Client) Diff(ctx context.Context, path, selectedPath string, staged bool) (ChangedFile, error) {
	var result ChangedFile
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		selected, err := validateFile(root, selectedPath)
		if err != nil {
			return err
		}
		result = ChangedFile{Path: selected, Status: "modified", Lines: []DiffLine{}}
		status, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		paths := []string{selected}
		conflicted := false
		for _, change := range status.Changes {
			if change.Path != selected {
				continue
			}
			code := change.WorktreeStatus
			conflicted = change.Conflict
			if staged {
				code = change.IndexStatus
			}
			result.Status = diffStatus(code)
			if change.OriginalPath != "" {
				result.PreviousPath = change.OriginalPath
				if staged && change.IndexStatus == "R" {
					previous, err := validateFile(root, change.OriginalPath)
					if err != nil {
						return err
					}
					paths = append(paths, previous)
				}
			}
			if change.Untracked && !staged {
				result.Status = "added"
				return c.untrackedDiff(ctx, root, &result)
			}
			break
		}
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--find-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "-U5"}
		if conflicted {
			args = append(args, "--ours")
		} else if staged {
			args = append(args, "--cached")
		}
		args = append(args, "--")
		output, err := c.run(ctx, root, c.options.LocalTimeout, append(args, paths...)...)
		if err != nil {
			return err
		}
		result.RawPatch = string(output)
		return parsePatch(&result)
	})
	return result, err
}

func diffStatus(code string) string {
	switch code {
	case "A", "?":
		return "added"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	default:
		return "modified"
	}
}

var hunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

func number(value int) *int { return &value }

func parsePatch(result *ChangedFile) error {
	oldLine, newLine := 0, 0
	hunk := -1
	inHunk := false
	for _, raw := range strings.Split(result.RawPatch, "\n") {
		if strings.HasPrefix(raw, "diff --git ") {
			inHunk = false
			continue
		}
		if strings.HasPrefix(raw, "Binary files ") || raw == "GIT binary patch" {
			result.Binary = true
			continue
		}
		if match := hunkHeader.FindStringSubmatch(raw); len(match) == 3 {
			hunk++
			var err error
			oldLine, err = strconv.Atoi(match[1])
			if err != nil {
				return errors.New("Invalid old line number in Git diff")
			}
			newLine, err = strconv.Atoi(match[2])
			if err != nil {
				return errors.New("Invalid new line number in Git diff")
			}
			inHunk = true
			continue
		}
		if !inHunk || raw == "" || strings.HasPrefix(raw, "\\") {
			continue
		}
		line := DiffLine{Text: raw[1:], Hunk: number(hunk)}
		switch raw[0] {
		case ' ':
			line.Kind, line.OldLine, line.NewLine = "context", number(oldLine), number(newLine)
			oldLine++
			newLine++
		case '+':
			line.Kind, line.NewLine = "add", number(newLine)
			newLine++
			result.Additions++
		case '-':
			line.Kind, line.OldLine = "remove", number(oldLine)
			oldLine++
			result.Deletions++
		default:
			return errors.New("Git returned an unsupported diff line")
		}
		result.Lines = append(result.Lines, line)
	}
	return nil
}

func (c *Client) untrackedDiff(ctx context.Context, root string, result *ChangedFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Lstat(result.Path)
	if err != nil {
		return fmt.Errorf("read untracked file: %w", err)
	}
	mode := "100644"
	var content []byte
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := directory.Readlink(result.Path)
		if err != nil {
			return fmt.Errorf("read untracked symlink: %w", err)
		}
		content, mode = []byte(target), "120000"
	} else {
		if !info.Mode().IsRegular() {
			return errors.New("Special files cannot be displayed as text")
		}
		if info.Mode()&0111 != 0 {
			mode = "100755"
		}
		file, err := directory.Open(result.Path)
		if err != nil {
			return fmt.Errorf("read untracked file: %w", err)
		}
		defer file.Close()
		content, err = io.ReadAll(io.LimitReader(file, c.options.MaxFileBytes+1))
		if err != nil {
			return fmt.Errorf("read untracked file: %w", err)
		}
		if int64(len(content)) > c.options.MaxFileBytes {
			result.Truncated = true
			result.Binary = bytes.IndexByte(content[:min(len(content), 8192)], 0) >= 0
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	header := "diff --git " + strconv.Quote("a/"+result.Path) + " " + strconv.Quote("b/"+result.Path) + "\nnew file mode " + mode + "\n"
	if bytes.IndexByte(content, 0) >= 0 {
		result.Binary = true
		result.RawPatch = header + "Binary files /dev/null and " + strconv.Quote("b/"+result.Path) + " differ\n"
		return nil
	}
	if len(content) == 0 {
		result.RawPatch = header
		return nil
	}
	text := strings.TrimSuffix(string(content), "\n")
	lines := strings.Split(text, "\n")
	var patch strings.Builder
	patch.WriteString(header)
	fmt.Fprintf(&patch, "--- /dev/null\n+++ %s\n@@ -0,0 +1,%d @@\n", strconv.Quote("b/"+result.Path), len(lines))
	for i, line := range lines {
		result.Lines = append(result.Lines, DiffLine{Kind: "add", NewLine: number(i + 1), Text: line})
		patch.WriteString("+" + line + "\n")
	}
	if content[len(content)-1] != '\n' {
		patch.WriteString("\\ No newline at end of file\n")
	}
	result.Additions = len(lines)
	result.RawPatch = patch.String()
	return nil
}
