package main

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"mag-ik-een-review/internal/reviewproviders"
	"reflect"
	"strings"
	"testing"
)

type fileRead struct{ Owner, Repo, Path, SHA string }
type reviewFileAPI struct {
	reviewproviders.API
	files   []githubclient.ChangedFile
	reads   []fileRead
	err     error
	request ReviewFileRequest
}

func (f *reviewFileAPI) ReviewChanges(_ context.Context, owner, repo string, number int, head, base string) (githubclient.ReviewChanges, error) {
	f.request = ReviewFileRequest{Owner: owner, Repository: repo, PullRequest: number, HeadSHA: head, BaseSHA: base}
	return githubclient.ReviewChanges{PullRequest: githubclient.PullRequestSummary{Head: githubclient.Branch{Repository: "fork/app"}}, MergeBaseSHA: strings.Repeat("c", 40), Files: f.files}, f.err
}
func (f *reviewFileAPI) ReadFileAtCommit(_ context.Context, owner, repo, path, sha string) (string, error) {
	f.reads = append(f.reads, fileRead{owner, repo, path, sha})
	return path + "\n", nil
}
func TestReviewFullFileUsesMergeBaseRenameAndForkHead(t *testing.T) {
	f := &reviewFileAPI{files: []githubclient.ChangedFile{{Path: "new.go", PreviousPath: "old.go", Status: "renamed"}}}
	r := ReviewFileRequest{Owner: "team", Repository: "app", PullRequest: 12, HeadSHA: strings.Repeat("b", 40), BaseSHA: strings.Repeat("a", 40), Path: "new.go"}
	file, err := readCapturedReviewFile(context.Background(), f, r)
	if err != nil || file.Old != "old.go\n" || file.New != "new.go\n" || !file.OldExists || !file.NewExists {
		t.Fatal(file, err)
	}
	want := []fileRead{{"team", "app", "old.go", strings.Repeat("c", 40)}, {"fork", "app", "new.go", r.HeadSHA}}
	if !reflect.DeepEqual(f.reads, want) {
		t.Fatal("mixed revision or repository", f.reads)
	}
	if f.request.HeadSHA != r.HeadSHA || f.request.BaseSHA != r.BaseSHA {
		t.Fatal("displayed revisions not verified")
	}
}
func TestReviewSourceRejectsUncapturedPathsAndSkipsMissingSides(t *testing.T) {
	for _, status := range []string{"added", "removed", "deleted"} {
		f := &reviewFileAPI{files: []githubclient.ChangedFile{{Path: "file", Status: status}}}
		file, err := readCapturedReviewFile(context.Background(), f, ReviewFileRequest{Path: "file"})
		if err != nil || len(f.reads) != 1 {
			t.Fatal(status, file, err, f.reads)
		}
		if status == "added" && file.OldExists || status != "added" && file.NewExists {
			t.Fatal("read absent side")
		}
	}
	f := &reviewFileAPI{files: []githubclient.ChangedFile{{Path: "file", Status: "modified"}}}
	if _, err := readCapturedReviewFile(context.Background(), f, ReviewFileRequest{Path: "../outside"}); err == nil || len(f.reads) != 0 {
		t.Fatal("unreviewed path read")
	}
	f.err = errors.New("PR changed")
	if _, err := readCapturedReviewFile(context.Background(), f, ReviewFileRequest{Path: "file"}); err == nil || len(f.reads) != 0 {
		t.Fatal("changed PR source was read")
	}
}
