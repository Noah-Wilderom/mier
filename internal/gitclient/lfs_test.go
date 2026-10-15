package gitclient

import (
	"context"
	"strings"
	"testing"
)

func TestLFSTrackingLifecycle(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	state, err := c.LFS(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Installed {
		t.Skip("Git LFS is not installed")
	}
	request := LFSRequest{Action: "install", ExpectedHead: head, ExpectedBranch: "main", Token: state.Token}
	if _, err = c.MutateLFS(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	state, err = c.LFS(ctx, root)
	if err != nil || !state.Initialized {
		t.Fatal(state, err)
	}
	request.Action = "track"
	request.Pattern = "*.bin"
	request.Token = state.Token
	if _, err = c.MutateLFS(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "data.bin", "binary payload\x00\x01")
	testGit(t, root, "add", ".gitattributes", "data.bin")
	if !strings.Contains(testGit(t, root, "show", ":data.bin"), "https://git-lfs.github.com/spec/v1") {
		t.Fatal("not stored as LFS pointer")
	}
	state, err = c.LFS(ctx, root)
	if err != nil || !strings.Contains(state.Summary, "data.bin") {
		t.Fatal(state, err)
	}
	request.Action = "untrack"
	request.Token = state.Token
	if _, err = c.MutateLFS(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	state, _ = c.LFS(ctx, root)
	request.Action = "track"
	request.Pattern = "literal [one].asset"
	request.Filename = true
	request.Token = state.Token
	if _, err = c.MutateLFS(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(testGit(t, root, "check-attr", "filter", "--", request.Pattern), "lfs") {
		t.Fatal("literal pattern did not match")
	}
	if _, err = c.MutateLFS(ctx, root, request); err == nil {
		t.Fatal("stale LFS preview accepted")
	}
}
