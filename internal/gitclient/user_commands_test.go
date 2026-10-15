package gitclient

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestUserCommandUsesLiteralArgumentsAndGuardsCheckout(t *testing.T) {
	c, root := testRepository(t, Options{})
	head := initialCommit(t, c, root)
	var stdout, stderr bytes.Buffer
	literal := "$(literal) with spaces; no shell"
	request := UserCommandRequest{Executable: c.gitPath, Arguments: []string{"config", "--local", "mier.literal", literal}, TimeoutSeconds: 5, ExpectedHead: head, ExpectedBranch: "main"}
	code, err := c.RunUserCommand(context.Background(), root, request, &stdout, &stderr)
	if err != nil || code != 0 {
		t.Fatal(code, err, stderr.String())
	}
	if testGit(t, root, "config", "--get", "mier.literal") != literal {
		t.Fatal("arguments interpreted")
	}
	request.ExpectedHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request.Arguments = []string{"config", "--local", "mier.literal", "changed"}
	if _, err = c.RunUserCommand(context.Background(), root, request, &stdout, &stderr); err == nil {
		t.Fatal("stale checkout accepted")
	}
	if testGit(t, root, "config", "--get", "mier.literal") != literal {
		t.Fatal("stale command ran")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request.ExpectedHead = head
	if _, err = c.RunUserCommand(ctx, root, request, &stdout, &stderr); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
