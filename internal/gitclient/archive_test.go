package gitclient

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func archiveFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	result := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			result[header.Name] = string(body)
		}
	}
	return result
}
func TestArchiveCommitUsesExactImmutableSHAAndLeavesWorktreeUntouched(t *testing.T) {
	client, root := testRepository(t, Options{})
	original := initialCommit(t, client, root)
	writeFile(t, root, "review.txt", "new committed revision\n")
	if err := client.Stage(context.Background(), root, []string{"review.txt"}); err != nil {
		t.Fatal(err)
	}
	current, err := client.Commit(context.Background(), root, "Later commit")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "review.txt", "uncommitted local change\n")
	before := testGit(t, root, "status", "--porcelain=v2")
	archive, err := client.ArchiveCommit(context.Background(), root, CommitArchiveRequest{Revision: original})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Revision != original || archiveFiles(t, archive.Data)["review.txt"] != "first\nsecond\n" {
		t.Fatalf("Wrong immutable source: %#v", archive)
	}
	if testGit(t, root, "rev-parse", "HEAD") != current || testGit(t, root, "status", "--porcelain=v2") != before {
		t.Fatal("Code snapshot changed local Git state")
	}
	for _, revision := range []string{"HEAD", "--all", "../main", "abcd", strings.Repeat("0", 40)} {
		if _, err = client.ArchiveCommit(context.Background(), root, CommitArchiveRequest{Revision: revision}); err == nil {
			t.Fatalf("Invalid or unavailable immutable revision accepted: %s", revision)
		}
	}
}
func TestArchiveComparisonUsesMergeBaseRatherThanLatestBase(t *testing.T) {
	client, root := testRepository(t, Options{})
	common := initialCommit(t, client, root)
	testGit(t, root, "switch", "-c", "feature")
	writeFile(t, root, "feature.php", "<?php $feature = true;\n")
	testGit(t, root, "add", "feature.php")
	testGit(t, root, "commit", "-m", "Feature change")
	head := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "switch", "main")
	writeFile(t, root, "review.txt", "BASE ONLY new line\nfirst\nsecond\n")
	testGit(t, root, "add", "review.txt")
	testGit(t, root, "commit", "-m", "Base advanced separately")
	base := testGit(t, root, "rev-parse", "HEAD")
	archive, err := client.ArchiveCommit(context.Background(), root, CommitArchiveRequest{Revision: base, ComparisonHead: head})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Revision != common || archiveFiles(t, archive.Data)["review.txt"] != "first\nsecond\n" {
		t.Fatal("Old diff side used latest base instead of merge-base")
	}
	if testGit(t, root, "rev-parse", "HEAD") != base {
		t.Fatal("Archive comparison checked out another branch")
	}
}
