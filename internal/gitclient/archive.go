package gitclient

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var fullCommitID = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

type CommitArchiveRequest struct {
	Revision, ComparisonHead, Host, Owner, Repo string
	PullRequest                                 int
}
type CommitArchiveResult struct {
	Revision string
	Data     []byte
}

// ArchiveCommit reads an immutable commit. If a PR object is missing, it fetches
// only from a matching configured repository into Git's object database. No
// checkout, branch, remote-tracking ref, index, or FETCH_HEAD is changed.
func (c *Client) ArchiveCommit(ctx context.Context, path string, request CommitArchiveRequest) (CommitArchiveResult, error) {
	if !fullCommitID.MatchString(request.Revision) {
		return CommitArchiveResult{}, errors.New("Code intelligence requires a complete immutable commit SHA")
	}
	if request.ComparisonHead != "" && !fullCommitID.MatchString(request.ComparisonHead) {
		return CommitArchiveResult{}, errors.New("Code comparison requires a complete immutable head SHA")
	}
	var archive CommitArchiveResult
	err := c.withRepository(ctx, path, c.options.RemoteTimeout, func(ctx context.Context, root string) error {
		revision := strings.ToLower(request.Revision)
		present := func(sha string) bool {
			output, err := c.run(ctx, root, c.options.LocalTimeout, "rev-parse", "--verify", "--quiet", "--end-of-options", sha+"^{commit}")
			return err == nil && strings.TrimSpace(string(output)) == sha
		}
		revisions := []string{revision}
		if request.ComparisonHead != "" {
			revisions = append(revisions, strings.ToLower(request.ComparisonHead))
		}
		fetched := false
		for _, wanted := range revisions {
			if present(wanted) {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if request.Host == "" || request.Owner == "" || request.Repo == "" || request.PullRequest <= 0 || request.PullRequest > 2147483647 {
				return errors.New("The selected commit is not available locally. Fetch the repository or open the pull request's repository")
			}
			remotes, err := c.remotes(ctx, root)
			if err != nil {
				return err
			}
			name := ""
			for _, remote := range remotes {
				if strings.EqualFold(remote.Host, request.Host) && strings.EqualFold(remote.Owner, request.Owner) && strings.EqualFold(remote.Repository, request.Repo) {
					name = remote.Name
					break
				}
			}
			if name == "" {
				return errors.New("No configured Git remote matches the selected pull request repository")
			}
			remote, err := c.selectRemote(ctx, root, name)
			if err != nil {
				return err
			}
			if err := validateRemoteURL(remote.fetchSource); err != nil {
				return err
			}
			ref := "refs/pull/" + strconv.Itoa(request.PullRequest) + "/head"
			if !fetched {
				if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.fetchSource, "fetch", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", "--", remote.Name, ref); err != nil {
					return err
				}
				fetched = true
			}
			if !present(wanted) {
				if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, remote.fetchSource, "fetch", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", "--", remote.Name, wanted); err != nil {
					return err
				}
			}
			if !present(wanted) {
				return errors.New("The selected pull request commit could not be fetched")
			}
		}
		if request.ComparisonHead != "" {
			output, err := c.run(ctx, root, c.options.LocalTimeout, "merge-base", revision, strings.ToLower(request.ComparisonHead))
			if err != nil {
				return err
			}
			revision = strings.TrimSpace(string(output))
			if !fullCommitID.MatchString(revision) {
				return errors.New("Git returned an invalid comparison merge base")
			}
		}
		entries, err := c.treeEntries(ctx, root, revision)
		if err != nil {
			return err
		}
		archive.Revision = revision
		archive.Data, err = entriesArchive(ctx, entries)
		return err
	})
	return archive, err
}
