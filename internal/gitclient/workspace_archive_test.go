package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceArchivesKeepHeadIndexAndWorkingLayersDistinct(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	writeFile(t, root, "symbol.php", "<?php const VALUE = 'HEAD';\n")
	writeFile(t, root, "use.php", "<?php use VALUE;\n")
	testGit(t, root, "add", "symbol.php", "use.php")
	testGit(t, root, "commit", "-m", "Add source files")
	head := testGit(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "symbol.php", "<?php const VALUE = 'INDEX';\n")
	testGit(t, root, "add", "symbol.php")
	writeFile(t, root, "symbol.php", "<?php const VALUE = 'WORKING';\n")
	writeFile(t, root, "untracked.php", "<?php class Untracked {}\n")
	if err := os.Remove(filepath.Join(root, "review.txt")); err != nil {
		t.Fatal(err)
	}
	before := testGit(t, root, "status", "--porcelain=v2")
	index, err := client.ArchiveWorkspace(context.Background(), root, "index")
	if err != nil {
		t.Fatal(err)
	}
	working, err := client.ArchiveWorkspace(context.Background(), root, "working")
	if err != nil {
		t.Fatal(err)
	}
	indexFiles, workingFiles := archiveFiles(t, index.Data), archiveFiles(t, working.Data)
	if !strings.Contains(indexFiles["symbol.php"], "INDEX") || !strings.Contains(workingFiles["symbol.php"], "WORKING") || indexFiles["use.php"] != workingFiles["use.php"] {
		t.Fatal("Cross-file source layers were mixed")
	}
	if _, exists := indexFiles["untracked.php"]; exists {
		t.Fatal("Untracked file appeared in index snapshot")
	}
	if workingFiles["untracked.php"] == "" {
		t.Fatal("Working untracked source omitted")
	}
	if _, exists := workingFiles["review.txt"]; exists {
		t.Fatal("Working deletion was not applied")
	}
	if indexFiles["review.txt"] == "" {
		t.Fatal("Working deletion incorrectly removed index source")
	}
	if len(index.Revision) != 64 || len(working.Revision) != 64 || index.Revision == working.Revision {
		t.Fatal("Distinct layers lacked immutable content digests")
	}
	again, err := client.ArchiveWorkspace(context.Background(), root, "working")
	if err != nil || again.Revision != working.Revision {
		t.Fatal("Identical local content changed its digest")
	}
	if testGit(t, root, "rev-parse", "HEAD") != head || testGit(t, root, "status", "--porcelain=v2") != before {
		t.Fatal("Snapshot changed source/index/Git state")
	}
}
func TestUnbornIndexAndWorkingSnapshotsWithRenameAndDelete(t *testing.T) {
	client, root := testRepository(t, Options{})
	writeFile(t, root, "staged.php", "<?php const SOURCE = 'INDEX';\n")
	testGit(t, root, "add", "staged.php")
	writeFile(t, root, "staged.php", "<?php const SOURCE = 'WORKING';\n")
	index, err := client.ArchiveWorkspace(context.Background(), root, "index")
	if err != nil {
		t.Fatal(err)
	}
	working, err := client.ArchiveWorkspace(context.Background(), root, "working")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archiveFiles(t, index.Data)["staged.php"], "INDEX") || !strings.Contains(archiveFiles(t, working.Data)["staged.php"], "WORKING") {
		t.Fatal("Unborn index/working layers mixed")
	}
	testGit(t, root, "commit", "-m", "Initial staged source")
	testGit(t, root, "mv", "staged.php", "renamed.php")
	renamed, err := client.ArchiveWorkspace(context.Background(), root, "index")
	if err != nil {
		t.Fatal(err)
	}
	files := archiveFiles(t, renamed.Data)
	if files["renamed.php"] == "" || files["staged.php"] != "" {
		t.Fatal("Index rename did not replace old path")
	}
	testGit(t, root, "rm", "-f", "renamed.php")
	deleted, err := client.ArchiveWorkspace(context.Background(), root, "index")
	if err != nil {
		t.Fatal(err)
	}
	if archiveFiles(t, deleted.Data)["renamed.php"] != "" {
		t.Fatal("Index deletion not applied")
	}
}
func TestWorkingSnapshotSkipsExternalSymlinkSource(t *testing.T) {
	client, root := testRepository(t, Options{})
	initialCommit(t, client, root)
	outside := filepath.Join(t.TempDir(), "private.php")
	if err := os.WriteFile(outside, []byte("<?php SECRET;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.php")); err != nil {
		t.Skip("Symlinks unavailable")
	}
	archive, err := client.ArchiveWorkspace(context.Background(), root, "working")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := archiveFiles(t, archive.Data)["linked.php"]; exists {
		t.Fatal("Working snapshot followed an external file link")
	}
}
