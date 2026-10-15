package gitclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type ExportPatchRequest struct {
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
}
type ApplyPatchRequest struct {
	Patch          string `json:"patch"`
	Mode           string `json:"mode"`
	Reverse        bool   `json:"reverse"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
}
type PatchPreview struct {
	Token string   `json:"token"`
	Stat  string   `json:"stat"`
	Paths []string `json:"paths"`
}

var binaryPatchSize = regexp.MustCompile(`(?m)^(?:literal|delta) ([0-9]+)$`)

func (c *Client) ExportPatch(ctx context.Context, path string, request ExportPatchRequest) (string, error) {
	var patch string
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--src-prefix=a/", "--dst-prefix=b/"}
		switch request.Mode {
		case "working":
		case "staged":
			args = append(args, "--cached")
		case "commit":
			if !objectID.MatchString(request.SHA) {
				return errors.New("Choose a full commit ID")
			}
			if _, err := c.resolveRevision(ctx, root, request.SHA); err != nil {
				return err
			}
			parents, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--parents", "-n", "1", request.SHA, "--")
			if err != nil {
				return err
			}
			if len(strings.Fields(string(parents))) > 2 {
				return errors.New("Choose a non-merge commit for mailbox export; Git format-patch omits merge commits")
			}
			args = []string{"format-patch", "-1", "--stdout", "--binary", "--full-index", "--no-signature", request.SHA}
		default:
			return errors.New("Choose working, staged, or commit patch export")
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, append(args, "--")...)
		if err != nil {
			return err
		}
		if len(out) > 8*1024*1024 {
			return errors.New("This patch exceeds the 8 MiB export limit")
		}
		patch = string(out)
		return nil
	})
	return patch, err
}
func (c *Client) previewPatch(ctx context.Context, root string, request ApplyPatchRequest) (PatchPreview, error) {
	result := PatchPreview{Paths: []string{}}
	if request.Patch == "" || len(request.Patch) > 8*1024*1024 || strings.ContainsRune(request.Patch, 0) {
		return result, errors.New("Choose a text or Git binary patch of at most 8 MiB")
	}
	if request.Mode != "working" && request.Mode != "index" && request.Mode != "commits" {
		return result, errors.New("Choose working files, working files and index, or mailbox commits")
	}
	if request.Mode == "commits" && request.Reverse {
		return result, errors.New("Use Revert commit to undo mailbox commits")
	}
	if request.Mode == "commits" && !strings.HasPrefix(request.Patch, "From ") {
		return result, errors.New("Mailbox mode needs a format-patch file. Use working files mode for a plain diff")
	}
	var total int64
	for _, match := range binaryPatchSize.FindAllStringSubmatch(request.Patch, -1) {
		size, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || size > 64*1024*1024 {
			return result, errors.New("Binary patch entries cannot exceed 64 MiB")
		}
		total += size
		if total > 256*1024*1024 {
			return result, errors.New("This binary patch expands beyond 256 MiB")
		}
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
		return result, errors.New("The checked-out branch changed. Preview the patch again")
	}
	if op, err := c.operation(ctx, root, state); err != nil {
		return result, err
	} else if op != nil {
		return result, errors.New("Finish or abort the current Git operation before applying a patch")
	}
	for _, file := range state.Changes {
		if file.Conflict {
			return result, errors.New("Resolve existing conflicts before importing a patch")
		}
	}
	if request.Mode == "commits" {
		if err = c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return result, err
		}
	}
	input := []byte(request.Patch)
	args := []string{"apply", "--numstat", "-z"}
	if request.Reverse {
		args = append(args, "--reverse")
	}
	out, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, input, args...)
	if err != nil {
		return result, err
	}
	parts := []string{root, state.HeadSHA, state.CurrentBranch, request.Mode, strconv.FormatBool(request.Reverse), request.Patch}
	seen := map[string]bool{}
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		if records[i] == "" {
			continue
		}
		fields := strings.SplitN(records[i], "\t", 3)
		if len(fields) != 3 {
			return result, errors.New("Invalid patch file list")
		}
		files := []string{fields[2]}
		if fields[2] == "" {
			if i+2 >= len(records) {
				return result, errors.New("Incomplete patch rename")
			}
			files = []string{records[i+1], records[i+2]}
			i += 2
		}
		for _, file := range files {
			file, err = validateFile(root, file)
			if err != nil {
				return result, err
			}
			if seen[file] {
				continue
			}
			seen[file] = true
			if len(seen) > 2000 {
				return result, errors.New("A patch may affect at most 2000 files")
			}
			_, token, err := c.workingVersion(ctx, root, file)
			if err != nil {
				return result, err
			}
			parts = append(parts, token)
			result.Paths = append(result.Paths, file)
		}
	}
	if len(result.Paths) == 0 {
		return result, errors.New("This patch does not contain file changes")
	}
	index, err := c.run(ctx, root, c.options.LocalTimeout, "ls-files", "--stage", "-z")
	if err != nil {
		return result, err
	}
	parts = append(parts, string(index))
	statArgs := []string{"apply", "--stat"}
	if request.Reverse {
		statArgs = append(statArgs, "--reverse")
	}
	stat, err := c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, input, statArgs...)
	if err != nil {
		return result, err
	}
	result.Stat = string(stat)
	if request.Mode != "commits" {
		args = []string{"apply", "--check"}
		if request.Mode == "index" {
			args = append(args, "--index")
		}
		if request.Reverse {
			args = append(args, "--reverse")
		}
		if _, err = c.executeInput(ctx, root, c.options.LocalTimeout, nil, nil, input, args...); err != nil {
			return result, err
		}
	}
	result.Token = digest(parts...)
	return result, nil
}
func (c *Client) PreviewPatch(ctx context.Context, path string, request ApplyPatchRequest) (PatchPreview, error) {
	var result PatchPreview
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.previewPatch(ctx, root, request)
		return err
	})
	return result, err
}
func (c *Client) ApplyPatch(ctx context.Context, path string, request ApplyPatchRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		preview, err := c.previewPatch(ctx, root, request)
		if err != nil {
			return err
		}
		if request.Token == "" || preview.Token != request.Token {
			return errors.New("The patch, index, or affected files changed. Preview the patch again")
		}
		args := []string{"apply"}
		if request.Mode == "index" {
			args = append(args, "--index")
		}
		if request.Reverse {
			args = append(args, "--reverse")
		}
		if request.Mode == "commits" {
			if err = c.checkpoint(ctx, root, request.ExpectedHead, "apply-mailbox"); err != nil {
				return err
			}
			args = []string{"am", "--3way"}
		}
		_, commandErr := c.executeInput(ctx, root, c.options.RemoteTimeout, []string{"GIT_EDITOR=true"}, nil, []byte(request.Patch), args...)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if commandErr != nil && result.Operation == nil {
			return fmt.Errorf("Apply patch: %w", commandErr)
		}
		return nil
	})
	return result, err
}
