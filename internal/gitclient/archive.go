package gitclient

import (
	"context"
	"errors"
	"regexp"
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
		fetched := map[string]bool{}
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
			source, err := c.pullRequestSource(ctx, root, remote, request.PullRequest)
			if err != nil {
				return err
			}
			endpoint, ref := source.HeadURL, source.HeadRef
			if request.ComparisonHead != "" && wanted == revision {
				endpoint, ref = source.BaseURL, source.BaseRef
			}
			key := endpoint + "\x00" + ref
			if ref != "" && !fetched[key] {
				if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, endpoint, "fetch", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", "--", endpoint, ref); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					// Reviewed branches can be deleted after merge. The immutable
					// commit below may still be available from the same repository.
				} else {
					fetched[key] = true
				}
			}
			if !present(wanted) {
				if _, err := c.authenticatedRun(ctx, root, c.options.RemoteTimeout, endpoint, "fetch", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", "--", endpoint, wanted); err != nil {
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
