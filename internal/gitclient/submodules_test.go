package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmoduleLifecyclePreservesDirtyWork(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	ctx := context.Background()
	testGit(t, root, "config", "--file", os.Getenv("GIT_CONFIG_GLOBAL"), "protocol.file.allow", "always")
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	testGit(t, source, "init", "--initial-branch=main")
	testGit(t, source, "config", "user.name", "Submodule Tester")
	testGit(t, source, "config", "user.email", "sub@example.invalid")
	writeFile(t, source, "source.txt", "first\n")
	testGit(t, source, "add", ".")
	testGit(t, source, "commit", "-m", "First")
	first := testGit(t, source, "rev-parse", "HEAD")
	request := SubmoduleRequest{Action: "add", Path: "module [one]", URL: source, ExpectedHead: head, ExpectedBranch: "main"}
	if err := c.MutateSubmodule(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	testGit(t, root, "commit", "-m", "Add submodule")
	request.ExpectedHead = testGit(t, root, "rev-parse", "HEAD")
	list, err := c.Submodules(ctx, root)
	if err != nil || len(list) != 1 || !list[0].Initialized || list[0].CurrentSHA != first {
		t.Fatal(list, err)
	}
	child := filepath.Join(root, request.Path)
	writeFile(t, source, "source.txt", "second\n")
	testGit(t, source, "commit", "-am", "Second")
	second := testGit(t, source, "rev-parse", "HEAD")
	testGit(t, root, "update-index", "--cacheinfo", "160000", second, request.Path)
	list, err = c.Submodules(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	request.Action = "update"
	request.Token = list[0].Token
	writeFile(t, child, "source.txt", "private edit\n")
	if err = c.MutateSubmodule(ctx, root, request); err == nil {
		t.Fatal("dirty update accepted")
	}
	testGit(t, child, "restore", "source.txt")
	list, _ = c.Submodules(ctx, root)
	request.Token = list[0].Token
	if err = c.MutateSubmodule(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	if testGit(t, child, "rev-parse", "HEAD") != second {
		t.Fatal("did not update recorded commit")
	}
	list, _ = c.Submodules(ctx, root)
	request.Token = list[0].Token
	request.Action = "deinit"
	writeFile(t, child, "private.txt", "keep\n")
	if err = c.MutateSubmodule(ctx, root, request); err == nil {
		t.Fatal("dirty deinit accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(child, "private.txt")); string(data) != "keep\n" {
		t.Fatal("private file lost")
	}
	os.Remove(filepath.Join(child, "private.txt"))
	testGit(t, root, "commit", "-m", "Update submodule")
	request.ExpectedHead = testGit(t, root, "rev-parse", "HEAD")
	list, _ = c.Submodules(ctx, root)
	request.Token = list[0].Token
	if err = c.MutateSubmodule(ctx, root, request); err != nil {
		t.Fatal(err)
	}
	list, _ = c.Submodules(ctx, root)
	if list[0].Initialized {
		t.Fatal("not deinitialized")
	}
	request.Action = "update"
	request.Token = list[0].Token
	if err = c.MutateSubmodule(ctx, root, request); err != nil {
		t.Fatal(err)
	}
}
func TestSubmoduleRejectsEscapesAndCustomUpdate(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	for _, path := range []string{"../escape", ".", ".git/modules/pwn"} {
		if err := c.MutateSubmodule(context.Background(), root, SubmoduleRequest{Action: "add", Path: path, URL: "https://example.invalid/repo.git", ExpectedHead: head, ExpectedBranch: "main"}); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	writeFile(t, root, ".gitmodules", "[submodule \"outside\"]\n path = ../outside\n url = https://example.invalid/repo.git\n")
	if _, err := c.Submodules(context.Background(), root); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatal(err)
	}
}
