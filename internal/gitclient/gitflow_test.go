package gitclient

import (
	"context"
	"strings"
	"testing"
)

func flowAction(t *testing.T, c *Client, root string, request FlowRequest) OperationResult {
	t.Helper()
	state := snapshot(t, c, root)
	flow, err := c.Flow(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedHead = state.HeadSHA
	request.ExpectedBranch = state.CurrentBranch
	request.Token = flow.Token
	result, err := c.MutateFlow(context.Background(), root, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func configureFlow(t *testing.T, c *Client, root string) {
	t.Helper()
	flow, err := c.Flow(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	flowAction(t, c, root, FlowRequest{Action: "configure", Config: flow.Config})
}

func TestGitflowFeatureReleaseAndHotfixFinish(t *testing.T) {
	for _, kind := range []string{"feature", "release", "hotfix"} {
		t.Run(kind, func(t *testing.T) {
			c, root := testRepository(t, Options{})
			initialCommit(t, c, root)
			configureFlow(t, c, root)
			flowAction(t, c, root, FlowRequest{Action: "start", Kind: kind, Name: "one"})
			writeFile(t, root, "feature.txt", kind+" work\n")
			testGit(t, root, "add", "feature.txt")
			testGit(t, root, "commit", "-m", kind+" work")
			source := testGit(t, root, "rev-parse", "HEAD")
			tag := "v1.0.0"
			flowAction(t, c, root, FlowRequest{Action: "prepare", Tag: tag, Message: "Release one"})
			for i := 0; i < 8; i++ {
				flow, err := c.Flow(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				if flow.Session == nil {
					break
				}
				result := flowAction(t, c, root, FlowRequest{Action: "advance"})
				if result.Operation != nil {
					t.Fatal(result)
				}
			}
			flow, _ := c.Flow(context.Background(), root)
			if flow.Session != nil {
				t.Fatal("finish plan remained", flow)
			}
			if state := snapshot(t, c, root); state.CurrentBranch != "develop" {
				t.Fatal(state.CurrentBranch)
			}
			testGit(t, root, "merge-base", "--is-ancestor", source, "develop")
			if kind != "feature" {
				testGit(t, root, "merge-base", "--is-ancestor", source, "main")
				if testGit(t, root, "cat-file", "-t", "refs/tags/"+tag) != "tag" {
					t.Fatal("release tag not annotated")
				}
				testGit(t, root, "merge-base", "--is-ancestor", "refs/tags/"+tag+"^{commit}", "develop")
			}
			if testGit(t, root, "rev-parse", "refs/heads/"+kind+"/one") != source {
				t.Fatal("source branch lost")
			}
		})
	}
}
func TestGitflowConflictProgressSurvivesAbortAndResume(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	configureFlow(t, c, root)
	flowAction(t, c, root, FlowRequest{Action: "start", Kind: "feature", Name: "conflict"})
	writeFile(t, root, "review.txt", "feature\nsecond\n")
	testGit(t, root, "commit", "-am", "Feature edit")
	testGit(t, root, "switch", "develop")
	writeFile(t, root, "review.txt", "develop\nsecond\n")
	testGit(t, root, "commit", "-am", "Develop edit")
	testGit(t, root, "switch", "feature/conflict")
	flowAction(t, c, root, FlowRequest{Action: "prepare"})
	result := flowAction(t, c, root, FlowRequest{Action: "advance"})
	if result.Operation == nil || result.Operation.Kind != "merge" {
		t.Fatal(result)
	}
	if _, err := c.ControlOperation(context.Background(), root, result.Operation.Token, "abort"); err != nil {
		t.Fatal(err)
	}
	result = flowAction(t, c, root, FlowRequest{Action: "advance"})
	if result.Operation == nil {
		t.Fatal("aborted merge not retryable")
	}
	file, err := c.Conflict(context.Background(), root, "review.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ResolveConflict(context.Background(), root, ResolveConflictRequest{Path: file.Path, Token: file.Token, Choice: "incoming"}); err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, c, root)
	if _, err = c.ControlOperation(context.Background(), root, state.Operation.Token, "continue"); err != nil {
		t.Fatal(err)
	}
	flowAction(t, c, root, FlowRequest{Action: "advance"})
	flowAction(t, c, root, FlowRequest{Action: "advance"})
	flow, _ := c.Flow(context.Background(), root)
	if flow.Session != nil {
		t.Fatal("resolved plan did not finish")
	}
}
func TestGitflowRejectsStalePlanAndOverlappingPrefixes(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	flow, _ := c.Flow(context.Background(), root)
	config := flow.Config
	config.HotfixPrefix = config.FeaturePrefix + "hotfix/"
	if _, err := c.MutateFlow(context.Background(), root, FlowRequest{Action: "configure", Config: config, ExpectedHead: head, ExpectedBranch: "main", Token: flow.Token}); err == nil {
		t.Fatal("overlap accepted")
	}
	configureFlow(t, c, root)
	flowAction(t, c, root, FlowRequest{Action: "start", Kind: "feature", Name: "stale"})
	flowAction(t, c, root, FlowRequest{Action: "prepare"})
	testGit(t, root, "update-ref", "refs/heads/develop", testGit(t, root, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "External update"))
	flow, _ = c.Flow(context.Background(), root)
	_, err := c.MutateFlow(context.Background(), root, FlowRequest{Action: "advance", ExpectedHead: head, ExpectedBranch: "feature/stale", Token: flow.Token})
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal(err)
	}
	flowAction(t, c, root, FlowRequest{Action: "cancel"})
}
func TestGitflowFinishesAlreadyMergedFeature(t *testing.T) {
	c, root := testRepository(t, Options{})
	initialCommit(t, c, root)
	configureFlow(t, c, root)
	flowAction(t, c, root, FlowRequest{Action: "start", Kind: "feature", Name: "empty"})
	flowAction(t, c, root, FlowRequest{Action: "prepare", DeleteBranch: true})
	flowAction(t, c, root, FlowRequest{Action: "advance"})
	flowAction(t, c, root, FlowRequest{Action: "advance"})
	flow, _ := c.Flow(context.Background(), root)
	if flow.Session != nil {
		t.Fatal("no-op merge did not finish")
	}
	if _, err := c.resolveRevision(context.Background(), root, "refs/heads/feature/empty"); err == nil {
		t.Fatal("approved branch deletion did not happen")
	}
}
