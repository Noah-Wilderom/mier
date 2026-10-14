package githubclient

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

type ReviewChanges struct {
	PullRequest  PullRequestSummary `json:"pullRequest"`
	Files        []ChangedFile      `json:"files"`
	MergeBaseSHA string             `json:"mergeBaseSha"`
}

var immutableCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)

// ReviewChanges captures only the PR metadata and diffs needed by a reviewer.
// The second revision check prevents a moving PR from mixing two previews.
func (c *Client) ReviewChanges(ctx context.Context, owner, repo string, number int, head, base string) (ReviewChanges, error) {
	if !immutableCommit.MatchString(head) || !immutableCommit.MatchString(base) {
		return ReviewChanges{}, errors.New("Review requires the displayed head and base commit SHAs")
	}
	pr, err := c.verifyHead(ctx, owner, repo, number, head)
	if err != nil {
		return ReviewChanges{}, err
	}
	if pr.Base.SHA != base {
		return ReviewChanges{}, errors.New("The pull request base changed. Refresh the review before starting AI")
	}
	path, _ := prPath(owner, repo, number)
	files, err := list[ChangedFile](ctx, c, path+"/files")
	if err != nil {
		return ReviewChanges{}, err
	}
	repositoryPath, _ := repoPath(owner, repo)
	var comparison struct {
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if _, err := c.request(ctx, http.MethodGet, repositoryPath+"/compare/"+base+"..."+head+"?per_page=1", nil, &comparison); err != nil {
		return ReviewChanges{}, err
	}
	if !immutableCommit.MatchString(comparison.MergeBase.SHA) {
		return ReviewChanges{}, errors.New("The review comparison has no immutable merge base")
	}
	current, err := c.verifyHead(ctx, owner, repo, number, head)
	if err != nil {
		return ReviewChanges{}, err
	}
	if current.Base.SHA != base {
		return ReviewChanges{}, errors.New("The pull request changed while preparing its review. Refresh and retry")
	}
	return ReviewChanges{PullRequest: pr, Files: files, MergeBaseSHA: comparison.MergeBase.SHA}, nil
}

// ReadFileAtCommit uses the authenticated Contents API at an immutable SHA.
// Download URLs from provider responses are never followed with credentials.
func (c *Client) ReadFileAtCommit(ctx context.Context, owner, repo, file, sha string) (string, error) {
	if !immutableCommit.MatchString(sha) {
		return "", errors.New("Choose an immutable source commit")
	}
	if file == "" || len(file) > 4096 || strings.ContainsAny(file, "\x00\\\r\n") || strings.HasPrefix(file, "/") {
		return "", errors.New("Choose a relative repository source path")
	}
	segments := strings.Split(file, "/")
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.EqualFold(segment, ".git") {
			return "", errors.New("Source paths must remain inside the repository")
		}
		segments[i] = url.PathEscape(segment)
	}
	base, err := repoPath(owner, repo)
	if err != nil {
		return "", err
	}
	var content struct {
		Type, Encoding, Content string
		Size                    int64
	}
	_, err = c.request(ctx, http.MethodGet, base+"/contents/"+strings.Join(segments, "/")+"?ref="+url.QueryEscape(sha), nil, &content)
	if err != nil {
		return "", err
	}
	if content.Type != "file" || content.Size > 2*1024*1024 || content.Encoding != "base64" {
		return "", errors.New("This source is binary, unavailable, or exceeds the 2 MiB text limit. Use the supplied diff")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil || len(data) > 2*1024*1024 || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return "", errors.New("This source cannot be read as text")
	}
	return string(data), nil
}
