package gitclient

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type FlowConfig struct {
	Main          string `json:"main"`
	Develop       string `json:"develop"`
	FeaturePrefix string `json:"featurePrefix"`
	ReleasePrefix string `json:"releasePrefix"`
	HotfixPrefix  string `json:"hotfixPrefix"`
}
type FlowSession struct {
	Kind         string     `json:"kind"`
	Source       string     `json:"source"`
	SourceSHA    string     `json:"sourceSha"`
	Config       FlowConfig `json:"config"`
	MainSHA      string     `json:"mainSha"`
	DevelopSHA   string     `json:"developSha"`
	Tag          string     `json:"tag"`
	Message      string     `json:"message"`
	DeleteBranch bool       `json:"deleteBranch"`
	Stage        int        `json:"stage"`
	MergeTarget  string     `json:"mergeTarget"`
	MergeBefore  string     `json:"mergeBefore"`
	MergeSource  string     `json:"mergeSource"`
}
type FlowState struct {
	Config     FlowConfig   `json:"config"`
	Configured bool         `json:"configured"`
	Kind       string       `json:"kind"`
	Session    *FlowSession `json:"session,omitempty"`
	Token      string       `json:"token"`
}
type FlowRequest struct {
	Action         string     `json:"action"`
	Config         FlowConfig `json:"config"`
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	Tag            string     `json:"tag"`
	Message        string     `json:"message"`
	DeleteBranch   bool       `json:"deleteBranch"`
	ExpectedHead   string     `json:"expectedHead"`
	ExpectedBranch string     `json:"expectedBranch"`
	Token          string     `json:"token"`
}

func (c *Client) flowStorage(ctx context.Context, root string) (*os.Root, error) {
	out, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(strings.TrimSpace(string(out)))
}
func (c *Client) readFlowSession(ctx context.Context, root string) (*FlowSession, error) {
	dir, err := c.flowStorage(ctx, root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.Open("mier/gitflow-finish.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 128*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 128*1024 {
		return nil, errors.New("Gitflow progress exceeds its storage limit")
	}
	var session FlowSession
	if err = json.Unmarshal(data, &session); err != nil {
		return nil, errors.New("Gitflow progress is unreadable. Preserve .git/mier/gitflow-finish.json and remove it to start a new plan")
	}
	if session.Stage < 0 || session.Stage > 3 || !objectID.MatchString(session.SourceSHA) || !objectID.MatchString(session.MainSHA) || !objectID.MatchString(session.DevelopSHA) {
		return nil, errors.New("Invalid Gitflow progress")
	}
	if err = c.validateFlowConfig(ctx, root, session.Config); err != nil {
		return nil, err
	}
	if err = c.validateBranch(ctx, root, session.Source); err != nil {
		return nil, err
	}
	if session.Kind != "feature" && session.Kind != "release" && session.Kind != "hotfix" {
		return nil, errors.New("Invalid Gitflow kind")
	}
	if session.MergeTarget != "" {
		if session.MergeTarget != session.Config.Main && session.MergeTarget != session.Config.Develop {
			return nil, errors.New("Invalid Gitflow merge destination")
		}
		if !objectID.MatchString(session.MergeSource) || !objectID.MatchString(session.MergeBefore) {
			return nil, errors.New("Invalid Gitflow merge progress")
		}
	}
	return &session, nil
}
func (c *Client) saveFlowSession(ctx context.Context, root string, session *FlowSession) error {
	dir, err := c.flowStorage(ctx, root)
	if err != nil {
		return err
	}
	defer dir.Close()
	const target = "mier/gitflow-finish.json"
	if session == nil {
		err = dir.Remove(target)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err = dir.MkdirAll("mier", 0700); err != nil {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	name := "mier/gitflow-" + rand.Text() + ".tmp"
	f, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return dir.Rename(name, target)
}
func (c *Client) validateFlowConfig(ctx context.Context, root string, config FlowConfig) error {
	if config.Main == config.Develop {
		return errors.New("Production and development branches must differ")
	}
	for _, name := range []string{config.Main, config.Develop} {
		if err := c.validateBranch(ctx, root, name); err != nil {
			return err
		}
	}
	prefixes := []string{config.FeaturePrefix, config.ReleasePrefix, config.HotfixPrefix}
	for i, prefix := range prefixes {
		if !strings.HasSuffix(prefix, "/") {
			return errors.New("Gitflow prefixes must end with /")
		}
		if err := c.validateBranch(ctx, root, prefix+"mier-check"); err != nil {
			return err
		}
		if strings.HasPrefix(config.Main, prefix) || strings.HasPrefix(config.Develop, prefix) {
			return errors.New("Gitflow prefixes cannot contain the main or develop branch")
		}
		for j := 0; j < i; j++ {
			if strings.HasPrefix(prefix, prefixes[j]) || strings.HasPrefix(prefixes[j], prefix) {
				return errors.New("Gitflow prefixes must not overlap")
			}
		}
	}
	return nil
}
func flowPrefix(config FlowConfig, kind string) (string, error) {
	switch kind {
	case "feature":
		return config.FeaturePrefix, nil
	case "release":
		return config.ReleasePrefix, nil
	case "hotfix":
		return config.HotfixPrefix, nil
	}
	return "", errors.New("Choose a feature, release, or hotfix")
}
func (c *Client) flowState(ctx context.Context, root string) (FlowState, error) {
	result := FlowState{Config: FlowConfig{Main: "main", Develop: "develop", FeaturePrefix: "feature/", ReleasePrefix: "release/", HotfixPrefix: "hotfix/"}}
	for key, dest := range map[string]*string{"gitflow.branch.master": &result.Config.Main, "gitflow.branch.develop": &result.Config.Develop, "gitflow.prefix.feature": &result.Config.FeaturePrefix, "gitflow.prefix.release": &result.Config.ReleasePrefix, "gitflow.prefix.hotfix": &result.Config.HotfixPrefix} {
		out, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--local", "--get", key)
		if err == nil {
			*dest = strings.TrimSpace(string(out))
			if key == "gitflow.branch.master" {
				result.Configured = true
			}
		} else if !isExitCode(err, 1) {
			return result, err
		}
	}
	if !result.Configured {
		if _, err := c.resolveRevision(ctx, root, "refs/heads/main"); err != nil {
			if _, err = c.resolveRevision(ctx, root, "refs/heads/master"); err == nil {
				result.Config.Main = "master"
			}
		}
	}
	state, err := c.status(ctx, root)
	if err != nil {
		return result, err
	}
	for _, kind := range []string{"feature", "release", "hotfix"} {
		prefix, _ := flowPrefix(result.Config, kind)
		if prefix != "" && strings.HasPrefix(state.CurrentBranch, prefix) && state.CurrentBranch != prefix {
			result.Kind = kind
		}
	}
	result.Session, err = c.readFlowSession(ctx, root)
	if err != nil {
		return result, err
	}
	refs, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/", "refs/tags/")
	if err != nil {
		return result, err
	}
	data, _ := json.Marshal(result)
	result.Token = digest(root, state.HeadSHA, state.CurrentBranch, string(refs), string(data))
	return result, nil
}
func (c *Client) Flow(ctx context.Context, path string) (FlowState, error) {
	var state FlowState
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		state, err = c.flowState(ctx, root)
		return err
	})
	return state, err
}
func (c *Client) MutateFlow(ctx context.Context, path string, request FlowRequest) (OperationResult, error) {
	var result OperationResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		flow, err := c.flowState(ctx, root)
		if err != nil {
			return err
		}
		if request.Token == "" || flow.Token != request.Token {
			return errors.New("Gitflow branches or progress changed. Refresh this plan")
		}
		if request.Action == "cancel" {
			if err = c.saveFlowSession(ctx, root, nil); err != nil {
				return err
			}
			result, err = c.operationResult(ctx, root)
			return err
		}
		if err = c.requireCleanBranch(ctx, root, request.ExpectedHead, request.ExpectedBranch); err != nil {
			return err
		}
		if request.Action == "configure" {
			if flow.Session != nil {
				return errors.New("Finish or cancel the saved Gitflow plan before changing configuration")
			}
			if err = c.validateFlowConfig(ctx, root, request.Config); err != nil {
				return err
			}
			main, err := c.resolveRevision(ctx, root, "refs/heads/"+request.Config.Main)
			if err != nil {
				return errors.New("Choose an existing local production branch")
			}
			if _, err = c.resolveRevision(ctx, root, "refs/heads/"+request.Config.Develop); err != nil {
				if _, err = c.run(ctx, root, c.options.LocalTimeout, "branch", "--", request.Config.Develop, main); err != nil {
					return err
				}
			}
			for _, entry := range [][2]string{{"gitflow.branch.master", request.Config.Main}, {"gitflow.branch.develop", request.Config.Develop}, {"gitflow.prefix.feature", request.Config.FeaturePrefix}, {"gitflow.prefix.release", request.Config.ReleasePrefix}, {"gitflow.prefix.hotfix", request.Config.HotfixPrefix}} {
				if _, err = c.run(ctx, root, c.options.LocalTimeout, "config", "--local", entry[0], entry[1]); err != nil {
					return err
				}
			}
		} else if request.Action == "start" {
			if !flow.Configured || flow.Session != nil {
				return errors.New("Configure Gitflow and finish any saved plan first")
			}
			if err = c.validateFlowConfig(ctx, root, flow.Config); err != nil {
				return err
			}
			prefix, err := flowPrefix(flow.Config, request.Kind)
			if err != nil {
				return err
			}
			branch := prefix + strings.TrimSpace(request.Name)
			if strings.TrimSpace(request.Name) == "" {
				return errors.New("Choose a branch name")
			}
			if err = c.validateBranch(ctx, root, branch); err != nil {
				return err
			}
			base := flow.Config.Develop
			if request.Kind == "hotfix" {
				base = flow.Config.Main
			}
			sha, err := c.resolveRevision(ctx, root, "refs/heads/"+base)
			if err != nil {
				return err
			}
			_, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-overwrite-ignore", "--no-track", "--create", branch, sha)
			if err != nil {
				return err
			}
		} else if request.Action == "prepare" {
			if !flow.Configured || flow.Kind == "" || flow.Session != nil {
				return errors.New("Check out a Gitflow branch and finish or cancel any existing plan first")
			}
			if err = c.validateFlowConfig(ctx, root, flow.Config); err != nil {
				return err
			}
			main, err := c.resolveRevision(ctx, root, "refs/heads/"+flow.Config.Main)
			if err != nil {
				return err
			}
			develop, err := c.resolveRevision(ctx, root, "refs/heads/"+flow.Config.Develop)
			if err != nil {
				return err
			}
			if flow.Kind != "feature" {
				if err = c.validateFlowTag(ctx, root, request.Tag); err != nil {
					return err
				}
				if _, err = c.resolveRevision(ctx, root, "refs/tags/"+request.Tag); err == nil {
					return errors.New("The release tag already exists")
				}
			}
			message := request.Message
			if strings.TrimSpace(message) == "" {
				message = "Release " + request.Tag
			}
			if len(message) > 32*1024 || strings.ContainsRune(message, 0) {
				return errors.New("Choose a tag message of at most 32 KiB")
			}
			session := &FlowSession{Kind: flow.Kind, Source: request.ExpectedBranch, SourceSHA: request.ExpectedHead, Config: flow.Config, MainSHA: main, DevelopSHA: develop, Tag: request.Tag, Message: message, DeleteBranch: request.DeleteBranch}
			if err = c.saveFlowSession(ctx, root, session); err != nil {
				return err
			}
		} else if request.Action == "advance" {
			return c.advanceFlow(ctx, root, flow.Session, &result)
		} else {
			return errors.New("Unknown Gitflow action")
		}
		result, err = c.operationResult(ctx, root)
		return err
	})
	return result, err
}
func (c *Client) validateFlowTag(ctx context.Context, root, tag string) error {
	if tag == "" || strings.HasPrefix(tag, "-") || strings.ContainsRune(tag, 0) {
		return errors.New("Choose a valid release tag")
	}
	_, err := c.run(ctx, root, c.options.LocalTimeout, "check-ref-format", "refs/tags/"+tag)
	return err
}
func (c *Client) advanceFlow(ctx context.Context, root string, session *FlowSession, result *OperationResult) error {
	if session == nil {
		return errors.New("Preview a Gitflow finish plan first")
	}
	source, err := c.resolveRevision(ctx, root, "refs/heads/"+session.Source)
	if err != nil || source != session.SourceSHA {
		return errors.New("The source branch changed. Cancel and preview a new finish plan")
	}
	main, err := c.resolveRevision(ctx, root, "refs/heads/"+session.Config.Main)
	if err != nil {
		return err
	}
	develop, err := c.resolveRevision(ctx, root, "refs/heads/"+session.Config.Develop)
	if err != nil {
		return err
	}
	if session.MergeTarget != "" {
		current := develop
		if session.MergeTarget == session.Config.Main {
			current = main
		}
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if state.CurrentBranch != session.MergeTarget {
			return errors.New("Return to the Gitflow merge destination before resuming this plan")
		}
		_, ancestorErr := c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", session.MergeSource, current)
		if current == session.MergeBefore && ancestorErr != nil {
			session.MergeTarget = ""
			if err = c.saveFlowSession(ctx, root, session); err != nil {
				return err
			}
		} else {
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", session.MergeBefore, current); err != nil {
				return errors.New("The merge destination was rewritten. Cancel and preview a new plan")
			}
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "merge-base", "--is-ancestor", session.MergeSource, current); err != nil {
				return errors.New("The source is not merged yet. Finish or abort the merge first")
			}
			if session.MergeTarget == session.Config.Main {
				session.MainSHA = current
				main = current
			} else {
				session.DevelopSHA = current
				develop = current
			}
			session.MergeTarget = ""
			if session.Stage == 0 && session.Kind == "feature" {
				session.Stage = 3
			} else {
				session.Stage++
			}
			if err = c.saveFlowSession(ctx, root, session); err != nil {
				return err
			}
			*result, err = c.operationResult(ctx, root)
			return err
		}
	}
	if main != session.MainSHA || develop != session.DevelopSHA {
		return errors.New("A Gitflow destination branch changed. Cancel and preview a new plan")
	}
	switch session.Stage {
	case 0, 2:
		target, before, incoming := session.Config.Main, main, source
		if session.Kind == "feature" || session.Stage == 2 {
			target, before = session.Config.Develop, develop
		}
		if session.Stage == 2 {
			incoming = main
		}
		if err = c.checkpoint(ctx, root, before, "gitflow-"+session.Kind); err != nil {
			return err
		}
		if _, err = c.run(ctx, root, c.options.LocalTimeout, "switch", "--no-overwrite-ignore", "--no-guess", "--", target); err != nil {
			return err
		}
		session.MergeTarget = target
		session.MergeBefore = before
		session.MergeSource = incoming
		if err = c.saveFlowSession(ctx, root, session); err != nil {
			return err
		}
		_, mergeErr := c.execute(ctx, root, c.options.LocalTimeout, []string{"GIT_EDITOR=true"}, nil, "merge", "--no-ff", "--no-edit", "--no-overwrite-ignore", "--", incoming)
		*result, err = c.operationResult(ctx, root)
		if err != nil {
			return err
		}
		if result.Operation != nil {
			return nil
		}
		if mergeErr != nil {
			return mergeErr
		}
		// Record the completed merge through the same validation used after a conflict.
		return c.advanceFlow(ctx, root, session, result)
	case 1:
		if session.Kind == "feature" {
			return errors.New("Invalid feature finish stage")
		}
		if err = c.validateFlowTag(ctx, root, session.Tag); err != nil {
			return err
		}
		existing, tagErr := c.resolveRevision(ctx, root, "refs/tags/"+session.Tag)
		if tagErr == nil && existing != main {
			return errors.New("The release tag points to another commit")
		}
		if tagErr != nil {
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "tag", "--annotate", "--message", session.Message, "--", session.Tag, main); err != nil {
				return err
			}
		}
		session.Stage = 2
		if err = c.saveFlowSession(ctx, root, session); err != nil {
			return err
		}
	case 3:
		if session.DeleteBranch {
			if _, err = c.run(ctx, root, c.options.LocalTimeout, "branch", "--delete", "--", session.Source); err != nil {
				return fmt.Errorf("Merges are complete; source branch could not be deleted. Keep it by cancelling the saved plan: %w", err)
			}
		}
		if err = c.saveFlowSession(ctx, root, nil); err != nil {
			return err
		}
	default:
		return errors.New("Invalid Gitflow finish stage")
	}
	*result, err = c.operationResult(ctx, root)
	return err
}
