package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Operation describes Git's actual sequencer state, including operations begun
// in another Git client. Token guards against continuing a different operation.
type Operation struct {
	Kind          string `json:"kind"`
	Token         string `json:"token"`
	SourceSHA     string `json:"sourceSha"`
	SourceLabel   string `json:"sourceLabel"`
	CurrentLabel  string `json:"currentLabel"`
	Conflicts     int    `json:"conflicts"`
	Message       string `json:"message"`
	CanSkip       bool   `json:"canSkip"`
	Empty         bool   `json:"empty"`
	PausedForEdit bool   `json:"pausedForEdit,omitempty"`
}

type OperationResult struct {
	HeadSHA   string     `json:"headSha"`
	Operation *Operation `json:"operation,omitempty"`
}

type CherryPickRequest struct {
	SHA            string `json:"sha"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Mainline       int    `json:"mainline"`
}

type MergeBranchRequest struct {
	Branch         string `json:"branch"`
	ExpectedSource string `json:"expectedSource"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
}

var objectID = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Client) operation(ctx context.Context, root string, status RepositorySnapshot) (*Operation, error) {
	output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	gitDir := strings.TrimSpace(string(output))
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(gitDir, name))
		if err != nil || len(data) > 128*1024 {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(gitDir, name)); return err == nil }
	op := &Operation{CurrentLabel: status.CurrentBranch}
	identity := ""
	switch {
	case exists("rebase-merge") || exists("rebase-apply"):
		if exists("rebase-apply/applying") {
			op.Kind = "am"
			op.CanSkip = true
			op.SourceLabel = "Mailbox patch " + read("rebase-apply/next")
			op.Message = read("rebase-apply/final-commit")
		} else {
			op.Kind = "rebase"
			op.CanSkip = true
		}
		dir := "rebase-merge/"
		if !exists("rebase-merge") {
			dir = "rebase-apply/"
		}
		op.SourceSHA = read("REBASE_HEAD")
		op.SourceLabel = "Replayed commit " + shortSHA(op.SourceSHA)
		op.CurrentLabel = "Rebased result (onto " + shortSHA(read(dir+"onto")) + ")"
		identity = read(dir+"orig-head") + "\x00" + read(dir+"onto") + "\x00" + read(dir+"head-name")
		if op.Kind == "am" {
			op.CurrentLabel = status.CurrentBranch
			identity += "\x00" + read(dir+"next") + "\x00" + read(dir+"patch") + "\x00" + read("ORIG_HEAD")
		}
		if op.Kind == "rebase" {
			done := strings.Fields(read(dir + "done"))
			for i := len(done) - 1; i >= 0; i-- {
				if objectID.MatchString(done[i]) && i > 0 {
					op.PausedForEdit = (done[i-1] == "edit" || done[i-1] == "e") && exists(dir+"stopped-sha")
					break
				}
			}
		}
	case exists("CHERRY_PICK_HEAD") || (exists("sequencer/todo") && strings.HasPrefix(read("sequencer/todo"), "pick ")):
		op.Kind, op.CanSkip = "cherry-pick", true
		op.SourceSHA = read("CHERRY_PICK_HEAD")
		identity = read("sequencer/head") + "\x00" + read("sequencer/todo")
	case exists("REVERT_HEAD"):
		op.Kind, op.CanSkip, op.SourceSHA = "revert", true, read("REVERT_HEAD")
	case exists("MERGE_HEAD"):
		op.Kind = "merge"
		identity = read("MERGE_HEAD")
		if fields := strings.Fields(identity); len(fields) > 0 {
			op.SourceSHA = fields[0]
		}
	case exists("BISECT_START"):
		op.Kind = "bisect"
		op.SourceSHA = read("BISECT_HEAD")
		if !objectID.MatchString(op.SourceSHA) {
			op.SourceSHA = status.HeadSHA
		}
		op.SourceLabel = "Test commit " + shortSHA(op.SourceSHA)
		op.Message = read("BISECT_LOG")
		op.CurrentLabel = read("BISECT_START")
		identity = op.Message + "\x00" + op.CurrentLabel
	default:
		return nil, nil
	}
	if op.SourceLabel == "" {
		op.SourceLabel = shortSHA(op.SourceSHA)
		if objectID.MatchString(op.SourceSHA) {
			subject, err := c.run(ctx, root, c.options.LocalTimeout, "show", "-s", "--format=%s", op.SourceSHA, "--")
			if err == nil {
				op.SourceLabel += " · " + strings.TrimSpace(string(subject))
			}
		}
	}
	if op.Kind == "revert" {
		op.SourceLabel = "Reverted commit " + op.SourceLabel
	}
	if op.Message == "" {
		op.Message = read("MERGE_MSG")
	}
	for _, file := range status.Changes {
		if file.Conflict {
			op.Conflicts++
		}
	}
	if op.Conflicts > 0 {
		op.PausedForEdit = false
	}
	op.Empty = (op.Kind == "cherry-pick" || op.Kind == "am") && len(status.Changes) == 0
	op.Token = digest(root, op.Kind, status.HeadSHA, op.SourceSHA, identity)
	return op, nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (c *Client) operationResult(ctx context.Context, root string) (OperationResult, error) {
	status, err := c.status(ctx, root)
	if err != nil {
		return OperationResult{}, err
	}
	op, err := c.operation(ctx, root, status)
	return OperationResult{HeadSHA: status.HeadSHA, Operation: op}, err
}

func (c *Client) requireCleanBranch(ctx context.Context, root, head, branch string) error {
	status, err := c.status(ctx, root)
	if err != nil {
		return err
	}
	if !objectID.MatchString(head) || status.HeadSHA != head || status.CurrentBranch != branch {
		return errors.New("The checked-out branch changed. Refresh the repository and try again")
	}
	if status.Detached {
		return errors.New("Check out a local branch first")
	}
	op, err := c.operation(ctx, root, status)
	if err != nil {
		return err
	}
	if op != nil {
		return fmt.Errorf("Finish or abort the current %s first", op.Kind)
	}
	if len(status.Changes) > 0 {
		return errors.New("Commit or stash your local changes before starting this operation")
	}
	return nil
}

// CherryPick applies one immutable commit to the currently checked-out branch.
// A conflict or an empty pick is a paused operation, not an opaque RPC failure.
func (c *Client) CherryPick(ctx context.Context, path string, request CherryPickRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !objectID.MatchString(request.SHA) {
			return errors.New("Choose a full commit SHA from repository history")
		}
		if err := c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		parents, err := c.run(ctx, root, c.options.LocalTimeout, "rev-list", "--parents", "-n", "1", request.SHA, "--")
		if err != nil {
			return err
		}
		count := len(strings.Fields(string(parents))) - 1
		if count > 1 && (request.Mainline < 1 || request.Mainline > count) {
			return errors.New("Choose the parent to use as the mainline for this merge commit")
		}
		if count <= 1 && request.Mainline != 0 {
			return errors.New("A mainline parent is only used for merge commits")
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "cherry-pick"); err != nil {
			return err
		}
		args := []string{"cherry-pick", "-x"}
		if count > 1 {
			args = append(args, "--mainline", fmt.Sprint(request.Mainline))
		}
		args = append(args, request.SHA)
		_, commandErr := c.execute(ctx, root, c.options.LocalTimeout, []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}, nil, args...)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if commandErr != nil && result.Operation == nil {
			return commandErr
		}
		return nil
	})
	return result, err
}

func (c *Client) MergeBranch(ctx context.Context, path string, request MergeBranchRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		if err := c.validateBranch(ctx, root, request.Branch); err != nil {
			return err
		}
		sha, err := c.resolveRevision(ctx, root, "refs/heads/"+request.Branch)
		if err != nil {
			return err
		}
		if sha != request.ExpectedSource {
			return errors.New("The source branch changed. Refresh and preview it again")
		}
		if err := c.checkpoint(ctx, root, request.ExpectedHead, "merge"); err != nil {
			return err
		}
		_, commandErr := c.execute(ctx, root, c.options.LocalTimeout, []string{"GIT_EDITOR=true"}, nil, "merge", "--no-edit", "--no-overwrite-ignore", "--", sha)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if commandErr != nil && result.Operation == nil {
			return commandErr
		}
		return nil
	})
	return result, err
}

func (c *Client) ControlOperation(ctx context.Context, path, token, action string) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		op := result.Operation
		if op == nil || token == "" || token != op.Token {
			return errors.New("The Git operation changed. Refresh before continuing or aborting")
		}
		if op.Kind == "bisect" {
			if action != "abort" {
				return errors.New("Mark the bisect commit as good, bad, or skipped")
			}
			state, err := c.status(ctx, root)
			if err != nil {
				return err
			}
			result, err = c.resetBisect(ctx, root, state)
			return err
		}
		if action != "continue" && action != "abort" && action != "skip" {
			return errors.New("Choose continue, abort, or skip")
		}
		if action == "skip" && !op.CanSkip {
			return errors.New("This Git operation cannot be skipped")
		}
		if action == "continue" {
			if op.Empty {
				return errors.New("This commit has no remaining changes. Skip it or abort the operation")
			}
			if op.Conflicts > 0 {
				return errors.New("Resolve and stage every conflicted file before continuing")
			}
			patch, err := c.run(ctx, root, c.options.LocalTimeout, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--no-color", "-U0")
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(patch), "\n") {
				if strings.HasPrefix(line, "+") && hasConflictMarkers(line[1:]) {
					return errors.New("The staged result still contains conflict markers")
				}
			}
		}
		args := []string{op.Kind, "--" + action}
		if op.Kind == "merge" && action == "continue" {
			args = []string{"commit", "--no-edit"}
		}
		env := []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}
		planPath := ""
		if op.Kind == "rebase" {
			var envErr error
			env, planPath, envErr = c.rebaseEnvironment(ctx, root)
			if envErr != nil {
				if action != "abort" {
					return envErr
				}
				env = []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}
			}
		}
		_, commandErr := c.execute(ctx, root, c.options.RemoteTimeout, env, nil, args...)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if op.Kind == "rebase" && result.Operation == nil && planPath != "" {
			_ = os.Remove(planPath)
		}
		// A new conflict in a sequenced operation is returned for immediate review.
		if commandErr != nil && !(action == "continue" && result.Operation != nil && result.Operation.Conflicts > 0 && result.Operation.Token != token) {
			return commandErr
		}
		return nil
	})
	return result, err
}

func (c *Client) BranchCommits(ctx context.Context, path, branch, expectedTip string) ([]Commit, error) {
	var commits []Commit
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, branch); err != nil {
			return err
		}
		sha, err := c.resolveRevision(ctx, root, "refs/heads/"+branch)
		if err != nil {
			return err
		}
		if sha != expectedTip {
			return errors.New("This branch changed. Refresh before selecting a commit")
		}
		commits, err = c.historyFor(ctx, root, []string{sha})
		return err
	})
	return commits, err
}
