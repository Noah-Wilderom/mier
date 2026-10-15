package gitclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type StackBranch struct {
	Name         string `json:"name"`
	SHA          string `json:"sha"`
	Parent       string `json:"parent"`
	ParentSHA    string `json:"parentSha"`
	BaseSHA      string `json:"baseSha"`
	Current      bool   `json:"current"`
	NeedsRestack bool   `json:"needsRestack"`
	Error        string `json:"error"`
}
type BranchStacks struct {
	Branches []StackBranch `json:"branches"`
	Token    string        `json:"token"`
}
type StackBranchRequest struct {
	Action         string `json:"action"`
	Name           string `json:"name"`
	Parent         string `json:"parent"`
	BaseSHA        string `json:"baseSha"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
}
type StackPushRequest struct {
	Branch         string `json:"branch"`
	Remote         string `json:"remote"`
	ExpectedHead   string `json:"expectedHead"`
	ExpectedBranch string `json:"expectedBranch"`
	Token          string `json:"token"`
}
type StackPushPreview struct {
	Branch    string `json:"branch"`
	LocalSHA  string `json:"localSha"`
	RemoteSHA string `json:"remoteSha"`
	URL       string `json:"url"`
	Rewrites  bool   `json:"rewrites"`
	Token     string `json:"token"`
}

func (c *Client) branchStacks(ctx context.Context, root string) (BranchStacks, error) {
	result := BranchStacks{Branches: []StackBranch{}}
	branches, err := c.branches(ctx, root)
	if err != nil {
		return result, err
	}
	tips := map[string]string{}
	for _, branch := range branches {
		if !branch.Remote {
			tips[branch.Name] = branch.CommitSHA
		}
	}
	if len(tips) > 1000 {
		return result, errors.New("Branch stacks support up to 1,000 local branches")
	}
	for _, branch := range branches {
		if branch.Remote {
			continue
		}
		item := StackBranch{Name: branch.Name, SHA: branch.CommitSHA, Current: branch.Current}
		for key, dest := range map[string]*string{"mier-stack-parent": &item.Parent, "mier-stack-base": &item.BaseSHA} {
			out, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--get", "branch."+branch.Name+"."+key)
			if err == nil {
				*dest = strings.TrimSpace(string(out))
			} else if !isExitCode(err, 1) {
				return result, err
			}
		}
		if item.Parent != "" {
			item.ParentSHA = tips[item.Parent]
			if err = c.validateBranch(ctx, root, item.Parent); err != nil || item.ParentSHA == "" {
				item.Error = "Parent branch is missing or invalid"
			} else if !objectID.MatchString(item.BaseSHA) {
				item.Error = "Saved stack base is invalid; link this branch again"
			} else {
				item.NeedsRestack = item.ParentSHA != item.BaseSHA
				if item.NeedsRestack {
					if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", item.ParentSHA, item.SHA); err == nil {
						item.NeedsRestack = false
					}
				}
			}
		}
		result.Branches = append(result.Branches, item)
	}
	sort.Slice(result.Branches, func(i, j int) bool { return result.Branches[i].Name < result.Branches[j].Name })
	data, _ := json.Marshal(result.Branches)
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	result.Token = digest(root, state.HeadSHA, state.CurrentBranch, string(data))
	return result, nil
}
func (c *Client) BranchStacks(ctx context.Context, path string) (BranchStacks, error) {
	var result BranchStacks
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.branchStacks(ctx, root)
		return err
	})
	return result, err
}
func (c *Client) StackForkPoint(ctx context.Context, path, parent string) (string, error) {
	var result string
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if err := c.validateBranch(ctx, root, parent); err != nil {
			return err
		}
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if _, err = c.resolveRevision(ctx, root, "refs/heads/"+parent); err != nil {
			return err
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--fork-point", "refs/heads/"+parent, state.HeadSHA)
		if err != nil {
			out, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "refs/heads/"+parent, state.HeadSHA)
		}
		result = strings.TrimSpace(string(out))
		return err
	})
	return result, err
}
func (c *Client) MutateStackBranch(ctx context.Context, path string, request StackBranchRequest) error {
	return c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		stacks, err := c.branchStacks(ctx, root)
		if err != nil {
			return err
		}
		if request.Token == "" || stacks.Token != request.Token {
			return errors.New("The stack branches changed. Refresh before continuing")
		}
		state, err := c.requireIdleHead(ctx, root, request.ExpectedHead)
		if err != nil {
			return err
		}
		if state.CurrentBranch != request.ExpectedBranch || state.Detached {
			return errors.New("Check out the stack branch before changing its relationship")
		}
		branch, parent, base := state.CurrentBranch, request.Parent, request.BaseSHA
		if request.Action == "create" {
			if err = c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
				return err
			}
			branch = request.Name
			parent = state.CurrentBranch
			base = state.HeadSHA
			if err = c.validateBranch(ctx, root, branch); err != nil {
				return err
			}
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-overwrite-ignore", "--no-track", "--create", branch, base); err != nil {
				return err
			}
		} else if request.Action == "unlink" {
			for _, key := range []string{"mier-stack-parent", "mier-stack-base"} {
				if _, err = c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--unset-all", "branch."+branch+"."+key); err != nil && !isExitCode(err, 5) && !isExitCode(err, 1) {
					return err
				}
			}
			return nil
		} else if request.Action != "link" {
			return errors.New("Choose create, link, or unlink for a stack branch")
		}
		if err = c.validateBranch(ctx, root, parent); err != nil {
			return err
		}
		if branch == parent {
			return errors.New("A stack branch cannot be its own parent")
		}
		parentMap := map[string]string{}
		exists := false
		for _, item := range stacks.Branches {
			parentMap[item.Name] = item.Parent
			if item.Name == parent {
				exists = true
			}
		}
		if !exists {
			return errors.New("Choose an existing local parent branch")
		}
		seen := map[string]bool{branch: true}
		for next := parent; next != ""; next = parentMap[next] {
			if seen[next] {
				return errors.New("This relationship would create a branch-stack cycle")
			}
			seen[next] = true
		}
		if !objectID.MatchString(base) {
			return errors.New("Choose a full commit ID for the stack's original fork point")
		}
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", base, state.HeadSHA); err != nil {
			return errors.New("The fork point must be an ancestor of the child branch")
		}
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "branch."+branch+".mier-stack-parent", parent); err != nil {
			return err
		}
		_, err = c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "branch."+branch+".mier-stack-base", base)
		return err
	})
}

func (c *Client) validateStackRebase(ctx context.Context, root string, request RebaseRequest, plan *rebaseEditorPlan) error {
	if request.StackToken == "" {
		return nil
	}
	stacks, err := c.branchStacks(ctx, root)
	if err != nil {
		return err
	}
	if stacks.Token != request.StackToken {
		return errors.New("The branch stack changed. Preview the restack again")
	}
	for _, branch := range stacks.Branches {
		if !branch.Current {
			continue
		}
		if branch.Error != "" {
			return errors.New(branch.Error)
		}
		if branch.Parent == "" || branch.BaseSHA != request.Preview.Upstream || branch.ParentSHA != request.Preview.Onto {
			return errors.New("The restack plan must replay the saved child range onto its parent")
		}
		plan.StackBranch = branch.Name
		plan.StackParent = branch.Parent
		plan.StackOldBase = branch.BaseSHA
		plan.StackNewBase = branch.ParentSHA
		return nil
	}
	return errors.New("Check out the branch to restack")
}
func (c *Client) finishStackRebase(ctx context.Context, root string, plan rebaseEditorPlan) error {
	if plan.StackBranch == "" {
		return nil
	}
	for _, name := range []string{plan.StackBranch, plan.StackParent} {
		if err := c.validateBranch(ctx, root, name); err != nil {
			return err
		}
	}
	if !objectID.MatchString(plan.StackOldBase) || !objectID.MatchString(plan.StackNewBase) {
		return errors.New("Invalid saved restack metadata")
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return err
	}
	if state.CurrentBranch != plan.StackBranch {
		return errors.New("Rebase completed on another branch; relink the stack before restacking again")
	}
	for key, expected := range map[string]string{"mier-stack-parent": plan.StackParent, "mier-stack-base": plan.StackOldBase} {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--get", "branch."+plan.StackBranch+"."+key)
		if err != nil || strings.TrimSpace(string(out)) != expected {
			return errors.New("Rebase completed; the stack relationship changed during the rebase. Review and relink its fork point")
		}
	}
	if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", plan.StackNewBase, state.HeadSHA); err != nil {
		return errors.New("The completed rebase does not contain its parent; relink the stack")
	}
	_, err = c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "branch."+plan.StackBranch+".mier-stack-base", plan.StackNewBase)
	return err
}
func (c *Client) finishSavedStackRebase(ctx context.Context, root, planPath string) error {
	f, err := os.Open(planPath)
	if err != nil {
		return err
	}
	defer f.Close()
	var metadata struct{ StackBranch, StackParent, StackOldBase, StackNewBase string }
	if err = json.NewDecoder(io.LimitReader(f, 64*1024*1024)).Decode(&metadata); err != nil {
		return err
	}
	return c.finishStackRebase(ctx, root, rebaseEditorPlan{StackBranch: metadata.StackBranch, StackParent: metadata.StackParent, StackOldBase: metadata.StackOldBase, StackNewBase: metadata.StackNewBase})
}

func (c *Client) stackPushPreview(ctx context.Context, root string, request StackPushRequest) (StackPushPreview, error) {
	result := StackPushPreview{Branch: request.Branch}
	if err := c.validateBranch(ctx, root, request.Branch); err != nil {
		return result, err
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	if state.HeadSHA != request.ExpectedHead || state.CurrentBranch != request.ExpectedBranch {
		return result, errors.New("The checkout changed. Preview publishing again")
	}
	if op, err := c.operation(ctx, root, state); err != nil {
		return result, err
	} else if op != nil {
		return result, errors.New("Finish the current Git operation before publishing")
	}
	sha, err := c.resolveRevision(ctx, root, "refs/heads/"+request.Branch)
	if err != nil {
		return result, err
	}
	remote, err := c.selectRemote(ctx, root, request.Remote)
	if err != nil {
		return result, err
	}
	if err = validateRemoteURL(remote.pushSource); err != nil {
		return result, err
	}
	ref := "refs/heads/" + request.Branch
	out, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.pushSource, "ls-remote", "--refs", "--", remote.pushSource, ref)
	if err != nil {
		return result, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			if !objectID.MatchString(fields[0]) {
				return result, errors.New("The remote returned an invalid commit ID")
			}
			result.RemoteSHA = fields[0]
		}
	}
	result.LocalSHA = sha
	result.URL = redactCredentials(remote.pushSource)
	if result.RemoteSHA != "" && result.RemoteSHA != sha {
		_, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", result.RemoteSHA, sha)
		result.Rewrites = err != nil
	}
	result.Token = digest(root, request.Branch, remote.Name, remote.pushSource, state.HeadSHA, state.CurrentBranch, sha, result.RemoteSHA)
	return result, nil
}
func (c *Client) PreviewStackPush(ctx context.Context, path string, request StackPushRequest) (StackPushPreview, error) {
	var result StackPushPreview
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.stackPushPreview(ctx, root, request)
		return err
	})
	return result, err
}
func (c *Client) PushStackBranch(ctx context.Context, path string, request StackPushRequest) error {
	return c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		preview, err := c.stackPushPreview(ctx, root, request)
		if err != nil {
			return err
		}
		if request.Token == "" || request.Token != preview.Token {
			return errors.New("The local or remote branch changed. Preview publishing again")
		}
		remote, err := c.selectRemote(ctx, root, request.Remote)
		if err != nil {
			return err
		}
		if preview.RemoteSHA != "" {
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "cat-file", "-e", preview.RemoteSHA+"^{commit}"); err == nil {
				if err = c.checkpoint(ctx, root, preview.RemoteSHA, "stack-publish"); err != nil {
					return err
				}
			}
		}
		ref := "refs/heads/" + request.Branch
		_, err = c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.pushSource, "push", "--force-with-lease="+ref+":"+preview.RemoteSHA, "--", remote.pushSource, preview.LocalSHA+":"+ref)
		if err != nil {
			return fmt.Errorf("Publish stack branch: %w", err)
		}
		return nil
	})
}
