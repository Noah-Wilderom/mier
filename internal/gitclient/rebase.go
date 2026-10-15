package gitclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RebasePreviewRequest struct {
	Onto           string `json:"onto"`
	Upstream       string `json:"upstream"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
}
type RebasePreview struct {
	Token    string   `json:"token"`
	Onto     string   `json:"onto"`
	Upstream string   `json:"upstream"`
	Head     string   `json:"head"`
	Branch   string   `json:"branch"`
	Commits  []Commit `json:"commits"`
}
type RebaseStep struct {
	SHA     string `json:"sha"`
	Action  string `json:"action"`
	Message string `json:"message"`
}
type RebaseRequest struct {
	Preview    RebasePreviewRequest `json:"preview"`
	Token      string               `json:"token"`
	Steps      []RebaseStep         `json:"steps"`
	StackToken string               `json:"stackToken,omitempty"`
}
type rebaseEditorPlan struct {
	GitDir       string
	Todo         string
	Messages     map[string]string
	Actions      map[string]string
	StackBranch  string
	StackParent  string
	StackOldBase string
	StackNewBase string
}

func (c *Client) rebasePreview(ctx context.Context, root string, request RebasePreviewRequest) (RebasePreview, error) {
	result := RebasePreview{Commits: []Commit{}}
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch || !state.HasHead || state.Detached {
		return result, errors.New("The checked-out branch changed. Check out a branch and preview again")
	}
	if !objectID.MatchString(request.Onto) {
		return result, errors.New("Choose a full destination commit ID")
	}
	onto, err := c.resolveRevision(ctx, root, request.Onto)
	if err != nil {
		return result, err
	}
	upstream := request.Upstream
	if upstream == "" {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", onto, state.HeadSHA)
		if err != nil {
			return result, err
		}
		upstream = strings.TrimSpace(string(out))
	}
	if !objectID.MatchString(upstream) {
		return result, errors.New("Choose a full base commit ID")
	}
	if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", upstream, state.HeadSHA); err != nil {
		return result, errors.New("The base must be an ancestor of the current branch")
	}
	out, err := c.run(ctx, root, c.options.LocalTimeout, "log", "-101", "--reverse", "--topo-order", "-z", "--no-show-signature", commitFormat, upstream+".."+state.HeadSHA, "--")
	if err != nil {
		return result, err
	}
	commits, err := parseCommits(out)
	if err != nil {
		return result, err
	}
	if len(commits) > 100 {
		return result, errors.New("Choose a nearer base; a rebase plan supports at most 100 commits")
	}
	ids := []string{}
	for _, commit := range commits {
		if len(commit.Parents) > 1 {
			return result, errors.New("This range contains merge commits. Use an external Git client to preserve its merge topology")
		}
		ids = append(ids, commit.SHA)
	}
	result = RebasePreview{Token: digest(root, state.HeadSHA, state.CurrentBranch, onto, upstream, strings.Join(ids, ",")), Onto: onto, Upstream: upstream, Head: state.HeadSHA, Branch: state.CurrentBranch, Commits: commits}
	return result, nil
}
func (c *Client) PreviewRebase(ctx context.Context, path string, request RebasePreviewRequest) (RebasePreview, error) {
	var result RebasePreview
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.rebasePreview(ctx, root, request)
		return err
	})
	return result, err
}

func buildRebasePlan(preview RebasePreview, steps []RebaseStep) (rebaseEditorPlan, error) {
	plan := rebaseEditorPlan{Messages: map[string]string{}, Actions: map[string]string{}}
	if len(steps) != len(preview.Commits) || len(steps) == 0 {
		return plan, errors.New("A plan must include every previewed commit exactly once")
	}
	allowed := map[string]Commit{}
	for _, commit := range preview.Commits {
		allowed[commit.SHA] = commit
	}
	kept := 0
	for _, step := range steps {
		commit, ok := allowed[step.SHA]
		if !ok {
			return plan, errors.New("The plan includes an unknown or duplicate commit")
		}
		delete(allowed, step.SHA)
		switch step.Action {
		case "pick", "reword", "edit", "squash", "fixup", "drop":
		default:
			return plan, errors.New("Choose pick, reword, edit, squash, fixup, or drop")
		}
		message := strings.TrimSpace(step.Message)
		if len(message) > 100*1024 || strings.ContainsRune(message, 0) {
			return plan, errors.New("Invalid rebase commit message")
		}
		if step.Action == "reword" && message == "" {
			return plan, errors.New("Reworded commits need a message")
		}
		if message == "" {
			message = strings.TrimSpace(commit.Subject + "\n\n" + commit.Body)
		}
		if step.Action != "drop" {
			if kept == 0 && (step.Action == "squash" || step.Action == "fixup") {
				return plan, errors.New("The first kept commit cannot squash or fix up a previous commit")
			}
			kept++
		}
		// Git receives only validated actions and immutable object IDs. Messages
		// go through a private file, never through shell or todo exec commands.
		plan.Todo += step.Action + " " + step.SHA + "\n"
		plan.Actions[step.SHA] = step.Action
		if step.Action == "reword" {
			plan.Messages[step.SHA] = message
		}
	}
	if kept == 0 {
		return plan, errors.New("Keep at least one commit in the plan")
	}
	return plan, nil
}

func gitEditorCommand(planPath, mode string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	quote := func(value string) string {
		return "'" + strings.ReplaceAll(filepath.ToSlash(value), "'", "'\\''") + "'"
	}
	return quote(executable) + " --mier-git-editor " + quote(planPath) + " " + quote(mode), nil
}
func rebaseEditorEnvironment(planPath string) ([]string, error) {
	sequence, err := gitEditorCommand(planPath, "todo")
	if err != nil {
		return nil, err
	}
	message, err := gitEditorCommand(planPath, "message")
	if err != nil {
		return nil, err
	}
	return []string{"GIT_SEQUENCE_EDITOR=" + sequence, "GIT_EDITOR=" + message}, nil
}
func (c *Client) Rebase(ctx context.Context, path string, request RebaseRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		if err := c.requireCleanBranch(ctx, root, request.Preview.ExpectedHead, request.Preview.ExpectedBranch); err != nil {
			return err
		}
		preview, err := c.rebasePreview(ctx, root, request.Preview)
		if err != nil {
			return err
		}
		if request.Token == "" || preview.Token != request.Token {
			return errors.New("The rebase preview changed. Preview again")
		}
		plan, err := buildRebasePlan(preview, request.Steps)
		if len(preview.Commits) == 0 && len(request.Steps) == 0 && request.StackToken != "" {
			plan = rebaseEditorPlan{Todo: "noop\n", Messages: map[string]string{}, Actions: map[string]string{}}
			err = nil
		}
		if err != nil {
			return err
		}
		if err = c.validateStackRebase(ctx, root, request, &plan); err != nil {
			return err
		}
		if err = c.protectResetPaths(ctx, root, preview.Head, preview.Onto); err != nil {
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
		if err != nil {
			return err
		}
		plan.GitDir = strings.TrimSpace(string(out))
		dir := filepath.Join(plan.GitDir, "mier", "rebase-plans")
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		planPath := filepath.Join(dir, hex.EncodeToString(nonce[:])+".json")
		data, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		if err = os.WriteFile(planPath, data, 0600); err != nil {
			return err
		}
		keepPlan := false
		defer func() {
			if !keepPlan {
				_ = os.Remove(planPath)
			}
		}()
		env, err := rebaseEditorEnvironment(planPath)
		if err != nil {
			return err
		}
		if err = c.checkpoint(ctx, root, preview.Head, "rebase"); err != nil {
			return err
		}
		_, commandErr := c.execute(ctx, root, c.options.RemoteTimeout, env, nil, "-c", "rebase.autoStash=false", "-c", "rebase.abbreviateCommands=false", "rebase", "--interactive", "--keep-empty", "--empty=keep", "--reapply-cherry-picks", "--onto", preview.Onto, preview.Upstream)
		result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if result.Operation != nil && result.Operation.Kind == "rebase" {
			// The marker lives in this Git sequencer's directory. A later external
			// rebase cannot accidentally inherit our saved messages.
			if err = os.WriteFile(filepath.Join(plan.GitDir, "rebase-merge", "mier-plan"), []byte(planPath), 0600); err != nil {
				return fmt.Errorf("Rebase paused; could not save its message plan: %w", err)
			}
			keepPlan = true
		} else if commandErr != nil {
			return commandErr
		} else if err = c.finishStackRebase(ctx, root, plan); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

// HandleGitEditor must run before desktop startup. It is also used by the test
// executable to exercise actual Git reword/squash/continue behavior cross-platform.
func HandleGitEditor(args []string) (bool, error) {
	if len(args) < 2 || args[1] != "--mier-git-editor" {
		return false, nil
	}
	if len(args) != 5 {
		return true, errors.New("Invalid Git editor arguments")
	}
	planFile, err := os.Open(args[2])
	if err != nil {
		return true, err
	}
	defer planFile.Close()
	var plan rebaseEditorPlan
	if err = json.NewDecoder(planFile).Decode(&plan); err != nil {
		return true, err
	}
	gitDir, err := filepath.EvalSymlinks(plan.GitDir)
	if err != nil {
		return true, err
	}
	target, err := filepath.Abs(args[4])
	if err != nil {
		return true, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return true, err
	}
	if !inside(gitDir, parent) {
		return true, errors.New("Git editor target is outside its Git directory")
	}
	content := ""
	switch args[3] {
	case "todo":
		if filepath.Base(target) != "git-rebase-todo" {
			return true, errors.New("Invalid todo target")
		}
		content = plan.Todo
	case "message":
		if filepath.Base(target) != "COMMIT_EDITMSG" {
			return true, errors.New("Invalid commit message target")
		}
		done, err := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "done"))
		if err != nil {
			return true, err
		}
		lines := strings.Split(strings.TrimSpace(string(done)), "\n")
		if len(lines) == 0 {
			return true, errors.New("Missing rebase step")
		}
		fields := strings.Fields(lines[len(lines)-1])
		if len(fields) < 2 {
			return true, errors.New("Invalid rebase step")
		}
		if action, ok := plan.Actions[fields[1]]; !ok || action != fields[0] {
			return true, errors.New("This editor step is not in the approved plan")
		} else if action != "reword" {
			return true, nil
		} // Keep Git's prepared combination or conflict-resolution message.
		var ok bool
		content, ok = plan.Messages[fields[1]]
		if !ok {
			return true, errors.New("Commit message was not in the approved rebase plan")
		}
	default:
		return true, errors.New("Invalid Git editor mode")
	}
	if info, err := os.Lstat(target); err != nil {
		return true, err
	} else if !info.Mode().IsRegular() {
		return true, errors.New("Git editor target must be a regular file")
	}
	return true, os.WriteFile(target, []byte(content+"\n"), 0600)
}

func (c *Client) rebaseEnvironment(ctx context.Context, root string) ([]string, string, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, "", err
	}
	gitDir := strings.TrimSpace(string(out))
	marker := filepath.Join(gitDir, "rebase-merge", "mier-plan")
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	planPath := string(data)
	if !inside(filepath.Join(gitDir, "mier", "rebase-plans"), planPath) {
		return nil, "", errors.New("Invalid saved rebase plan")
	}
	env, err := rebaseEditorEnvironment(planPath)
	return env, planPath, err
}
