package gitclient

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type AssistantCaptureRequest struct {
	Kind         string `json:"kind"`
	Base         string `json:"base"`
	Head         string `json:"head"`
	ExpectedHead string `json:"expectedHead"`
	Path         string `json:"path"`
}
type AssistantFile struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Status       string `json:"status"`
	Patch        string `json:"patch"`
}
type AssistantCapture struct {
	Root           string                  `json:"-"`
	Request        AssistantCaptureRequest `json:"-"`
	Kind           string                  `json:"kind"`
	Repository     string                  `json:"repository"`
	Branch         string                  `json:"branch"`
	HeadSHA        string                  `json:"headSha"`
	BaseSHA        string                  `json:"baseSha"`
	MergeBaseSHA   string                  `json:"mergeBaseSha"`
	Token          string                  `json:"token"`
	Files          []AssistantFile         `json:"files"`
	RecentSubjects []string                `json:"recentSubjects"`
	Warnings       []string                `json:"warnings"`
	Conflict       *ConflictFile           `json:"conflict,omitempty"`
	IndexBlobs     map[string]string       `json:"-"`
}

func (c *Client) AssistantCapture(ctx context.Context, path string, request AssistantCaptureRequest) (AssistantCapture, error) {
	var captured AssistantCapture
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		captured, err = c.captureAssistant(ctx, root, request)
		return err
	})
	return captured, err
}
func (c *Client) assistantIndex(ctx context.Context, root string) (string, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv", "--no-renames")
	return string(out), err
}
func (c *Client) captureAssistant(ctx context.Context, root string, request AssistantCaptureRequest) (AssistantCapture, error) {
	result := AssistantCapture{Root: root, Request: request, Kind: request.Kind, Repository: filepath.Base(root), Files: []AssistantFile{}, RecentSubjects: []string{}, Warnings: []string{}, IndexBlobs: map[string]string{}}
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	result.Branch = state.CurrentBranch
	result.HeadSHA = state.HeadSHA
	op, err := c.operation(ctx, root, state)
	if err != nil {
		return result, err
	}
	if (request.Kind == "commit" || request.Kind == "code") && op != nil {
		return result, errors.New("Finish the current Git operation before starting this AI action")
	}
	if request.Kind == "commit" || request.Kind == "conflict" || request.Kind == "code" {
		if request.ExpectedHead != state.HeadSHA {
			return result, errors.New("The current commit changed. Refresh before starting this AI action")
		}
	}
	if state.HasHead {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "log", "-10", "--format=%s", state.HeadSHA, "--")
		if err != nil {
			return result, err
		}
		result.RecentSubjects = strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	var files []ChangedFile
	switch request.Kind {
	case "commit":
		before, err := c.assistantIndex(ctx, root)
		if err != nil {
			return result, err
		}
		staged := []string{}
		for _, file := range state.Changes {
			if file.Conflict {
				return result, errors.New("Resolve conflicts before drafting a commit message")
			}
			if file.Staged {
				staged = append(staged, file.Path)
			}
		}
		if len(staged) == 0 {
			return result, errors.New("Stage changes before asking for a commit draft")
		}
		if len(staged) > 64 {
			return result, errors.New("This commit exceeds the 64-file AI context limit. Draft its message manually or split it into smaller commits")
		}
		for _, path := range staged {
			file, err := c.diff(ctx, root, path, true)
			if err != nil {
				return result, err
			}
			files = append(files, file)
		}
		args := append([]string{"ls-files", "--stage", "-z", "--"}, staged...)
		out, err := c.run(ctx, root, c.options.LocalTimeout, args...)
		if err != nil {
			return result, err
		}
		for _, record := range strings.Split(string(out), "\x00") {
			header, path, ok := strings.Cut(record, "\t")
			fields := strings.Fields(header)
			if !ok {
				continue
			}
			if len(fields) != 3 || fields[2] != "0" || !objectID.MatchString(fields[1]) {
				return result, errors.New("The staged source changed or has unresolved entries")
			}
			result.IndexBlobs[path] = fields[1]
		}
		after, err := c.assistantIndex(ctx, root)
		if err != nil {
			return result, err
		}
		if before != after {
			return result, errors.New("Staged changes moved while preparing the AI context. Retry")
		}
		result.BaseSHA = state.HeadSHA
		result.MergeBaseSHA = state.HeadSHA
		result.Token = digest(root, request.Kind, state.CurrentBranch, state.HeadSHA, before)
	case "pull_request", "explain":
		if request.Base == "" || request.Head == "" {
			return result, errors.New("Choose base and source branches for this AI action")
		}
		base, err := c.resolveRevision(ctx, root, request.Base)
		if err != nil {
			return result, err
		}
		head, err := c.resolveRevision(ctx, root, request.Head)
		if err != nil {
			return result, err
		}
		if request.ExpectedHead != "" && request.ExpectedHead != head {
			return result, errors.New("The selected source commit changed. Refresh its preview")
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", base, head)
		if err != nil {
			return result, err
		}
		mergeBase := strings.TrimSpace(string(out))
		if !objectID.MatchString(mergeBase) {
			return result, errors.New("The branches have no usable common ancestor")
		}
		files, err = c.diffRevisions(ctx, root, []string{mergeBase, head})
		if err != nil {
			return result, err
		}
		result.HeadSHA = head
		result.BaseSHA = base
		result.MergeBaseSHA = mergeBase
		out, err = c.run(ctx, root, c.options.LocalTimeout, "log", "-40", "--format=%s", base+".."+head, "--")
		if err != nil {
			return result, err
		}
		result.RecentSubjects = strings.Split(strings.TrimSpace(string(out)), "\n")
		result.Token = digest(root, request.Kind, request.Base, request.Head, base, head)
	case "conflict":
		file, err := c.conflict(ctx, root, request.Path)
		if err != nil {
			return result, err
		}
		if !file.Editable {
			return result, errors.New("This conflict needs a complete-version choice rather than an AI text proposal")
		}
		if len(file.Base.Content)+len(file.Current.Content)+len(file.Incoming.Content)+len(file.Working) > 384*1024 {
			return result, errors.New("This conflict exceeds the AI text context limit")
		}
		result.Conflict = &file
		result.Token = file.Token
	case "code":
		if !state.HasHead {
			return result, errors.New("Create an initial commit before starting an agent worktree")
		}
		result.Token = digest(root, request.Kind, state.CurrentBranch, state.HeadSHA)
	default:
		return result, errors.New("Choose a supported AI action")
	}
	bytes := 0
	for _, file := range files {
		if len(result.Files) >= 64 || bytes+len(file.RawPatch) > 384*1024 {
			return result, errors.New("This comparison exceeds the 64-file or 384 KiB AI context limit")
		}
		result.Files = append(result.Files, AssistantFile{Path: file.Path, PreviousPath: file.PreviousPath, Status: file.Status, Patch: file.RawPatch})
		bytes += len(file.RawPatch)
		if file.Binary || file.Truncated || file.RawPatch == "" {
			result.Warnings = append(result.Warnings, "No complete text patch for "+file.Path)
		}
	}
	if request.Kind != "conflict" && request.Kind != "code" && len(result.Files) == 0 {
		return result, errors.New("There are no changes in the selected comparison")
	}
	latest, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	if request.Kind == "commit" && latest.HeadSHA != state.HeadSHA {
		return result, errors.New("The current commit changed while preparing the draft")
	}
	return result, nil
}

func (c *Client) ValidateAssistantCapture(ctx context.Context, captured AssistantCapture) error {
	next, err := c.AssistantCapture(ctx, captured.Root, captured.Request)
	if err != nil {
		return err
	}
	if next.Token != captured.Token {
		return errors.New("These changes moved after the AI task started. Generate a fresh proposal")
	}
	return nil
}
func (c *Client) AssistantSource(ctx context.Context, captured AssistantCapture, path, side string) (RevisionFile, error) {
	var result RevisionFile
	err := c.withRepository(ctx, captured.Root, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		selected, err := validateFile(root, path)
		if err != nil {
			return err
		}
		if side != "old" && side != "new" {
			return errors.New("Choose old or new source")
		}
		if captured.Kind == "commit" && side == "new" {
			if blob := captured.IndexBlobs[selected]; blob != "" {
				size, err := c.run(ctx, root, c.options.LocalTimeout, "cat-file", "-s", blob)
				if err != nil {
					return err
				}
				length, err := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
				if err != nil || length < 0 || length > c.options.MaxFileBytes {
					return errors.New("The captured file exceeds its source read limit")
				}
				out, err := c.run(ctx, root, c.options.LocalTimeout, "cat-file", "blob", blob)
				if err != nil {
					return err
				}
				if !utf8.Valid(out) || strings.ContainsRune(string(out), 0) {
					return errors.New("This captured source file is binary")
				}
				result = RevisionFile{Path: selected, SHA: blob, Content: string(out)}
				return nil
			}
			for _, file := range captured.Files {
				if file.Path == selected && file.Status == "removed" {
					return errors.New("This file is deleted in the staged snapshot")
				}
			}
		}
		revision := captured.HeadSHA
		if side == "old" {
			revision = captured.MergeBaseSHA
			for _, file := range captured.Files {
				if file.Path == selected && file.PreviousPath != "" {
					selected = file.PreviousPath
					break
				}
			}
		}
		if revision == "" {
			return errors.New("This side has no committed source")
		}
		result, err = c.revisionFile(ctx, root, revision, selected)
		if err == nil && (result.Binary || !utf8.ValidString(result.Content)) {
			return errors.New("This source file is binary")
		}
		return err
	})
	return result, err
}
