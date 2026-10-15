package gitclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func (c *Client) Snapshot(ctx context.Context, path string) (RepositorySnapshot, error) {
	var snapshot RepositorySnapshot
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		snapshot, err = c.status(ctx, root)
		if err != nil {
			return err
		}
		snapshot.Root, snapshot.Name = root, filepath.Base(root)
		snapshot.Operation, err = c.operation(ctx, root, snapshot)
		if err != nil {
			return err
		}
		snapshot.Remotes, err = c.remotes(ctx, root)
		if err != nil {
			return err
		}
		snapshot.Branches, err = c.branches(ctx, root)
		if err != nil {
			return err
		}
		if snapshot.HasHead {
			snapshot.History, err = c.history(ctx, root)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return snapshot, err
}

func (c *Client) status(ctx context.Context, root string) (RepositorySnapshot, error) {
	output, err := c.run(ctx, root, c.options.LocalTimeout, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	return parseStatus(output)
}

func parseStatus(output []byte) (RepositorySnapshot, error) {
	result := RepositorySnapshot{Changes: []FileStatus{}, History: []Commit{}, Branches: []Branch{}, Remotes: []Remote{}}
	records := strings.Split(string(output), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			header := strings.SplitN(record[2:], " ", 2)
			if len(header) != 2 {
				continue
			}
			switch header[0] {
			case "branch.oid":
				result.HasHead = header[1] != "(initial)"
				if result.HasHead {
					result.HeadSHA = header[1]
				}
			case "branch.head":
				result.Detached = header[1] == "(detached)"
				if result.Detached {
					result.CurrentBranch = "HEAD"
				} else {
					result.CurrentBranch = header[1]
				}
			case "branch.upstream":
				result.Upstream = header[1]
			case "branch.ab":
				counts := strings.Fields(header[1])
				if len(counts) == 2 {
					result.Ahead, _ = strconv.Atoi(strings.TrimPrefix(counts[0], "+"))
					result.Behind, _ = strconv.Atoi(strings.TrimPrefix(counts[1], "-"))
				}
			}
			continue
		}
		if strings.HasPrefix(record, "? ") {
			result.Changes = append(result.Changes, FileStatus{Path: record[2:], IndexStatus: ".", WorktreeStatus: "?", Status: "untracked", Unstaged: true, Untracked: true})
			continue
		}
		if strings.HasPrefix(record, "! ") {
			continue
		}
		var fields []string
		var change FileStatus
		switch record[0] {
		case '1':
			fields = strings.SplitN(record, " ", 9)
			if len(fields) != 9 {
				return result, errors.New("Git returned an invalid file status record")
			}
			change.Path = fields[8]
		case '2':
			fields = strings.SplitN(record, " ", 10)
			if len(fields) != 10 || i+1 >= len(records) {
				return result, errors.New("Git returned an invalid rename record")
			}
			change.Path = fields[9]
			i++
			change.OriginalPath = records[i]
		case 'u':
			fields = strings.SplitN(record, " ", 11)
			if len(fields) != 11 {
				return result, errors.New("Git returned an invalid conflict record")
			}
			change.Path, change.Conflict = fields[10], true
		default:
			return result, fmt.Errorf("Git returned an unsupported status record: %q", record[0])
		}
		if len(fields[1]) != 2 {
			return result, errors.New("Git returned an invalid index status")
		}
		change.IndexStatus, change.WorktreeStatus = fields[1][:1], fields[1][1:]
		change.Submodule = fields[2]
		change.Staged = change.IndexStatus != "."
		change.Unstaged = change.WorktreeStatus != "."
		change.Status = statusName(change.IndexStatus, change.WorktreeStatus, change.Conflict)
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

func statusName(index, worktree string, conflict bool) string {
	if conflict {
		return "conflicted"
	}
	for _, status := range []struct{ code, name string }{{"R", "renamed"}, {"D", "deleted"}, {"A", "added"}, {"T", "typechanged"}} {
		if index == status.code || worktree == status.code {
			return status.name
		}
	}
	return "modified"
}

func (c *Client) history(ctx context.Context, root string) ([]Commit, error) {
	return c.historyFor(ctx, root, []string{"--exclude=refs/stash", "--exclude=refs/mier/*", "--all", "HEAD"})
}

func (c *Client) historyFor(ctx context.Context, root string, revisions []string) ([]Commit, error) {
	args := []string{"log", "-200", "--date-order", "-z", "--no-show-signature", "--no-decorate", "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%s%x00%b%x00"}
	args = append(args, revisions...)
	args = append(args, "--")
	output, err := c.run(ctx, root, c.options.LocalTimeout, args...)
	if err != nil {
		return nil, err
	}
	return parseCommits(output)
}

func parseCommits(output []byte) ([]Commit, error) {
	fields := strings.Split(string(output), "\x00")
	result := []Commit{}
	for i := 0; i < len(fields); {
		if fields[i] == "" {
			i++
			continue
		}
		if len(fields)-i < 7 {
			return nil, errors.New("Git returned an incomplete commit record")
		}
		parents := strings.Fields(fields[i+1])
		if parents == nil {
			parents = []string{}
		}
		result = append(result, Commit{SHA: fields[i], Parents: parents, AuthorName: fields[i+2], AuthorEmail: fields[i+3], CreatedAt: fields[i+4], Subject: fields[i+5], Body: strings.TrimRight(fields[i+6], "\n")})
		i += 7
	}
	return result, nil
}

var aheadBehind = regexp.MustCompile(`(ahead|behind) ([0-9]+)`)

func (c *Client) branches(ctx context.Context, root string) ([]Branch, error) {
	output, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--sort=-committerdate", "--format=%(refname)%00%(objectname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track)%00%(symref)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	result := []Branch{}
	for _, record := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x00")
		if len(fields) != 6 {
			return nil, errors.New("Git returned an invalid branch record")
		}
		if fields[5] != "" {
			continue
		}
		remote := strings.HasPrefix(fields[0], "refs/remotes/")
		name := strings.TrimPrefix(strings.TrimPrefix(fields[0], "refs/heads/"), "refs/remotes/")
		branch := Branch{Name: name, Remote: remote, CommitSHA: fields[1], Current: strings.TrimSpace(fields[2]) == "*", Upstream: fields[3]}
		for _, match := range aheadBehind.FindAllStringSubmatch(fields[4], -1) {
			count, _ := strconv.Atoi(match[2])
			if match[1] == "ahead" {
				branch.Ahead = count
			} else {
				branch.Behind = count
			}
		}
		result = append(result, branch)
	}
	return result, nil
}

func isExitCode(err error, code int) bool {
	var commandError *CommandError
	return errors.As(err, &commandError) && commandError.ExitCode == code
}

func (c *Client) remotes(ctx context.Context, root string) ([]Remote, error) {
	output, err := c.run(ctx, root, c.options.LocalTimeout, "config", "--null", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	if isExitCode(err, 1) {
		return []Remote{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Remote{}
	indices := make(map[string]int)
	for _, record := range strings.Split(string(output), "\x00") {
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\n", 2)
		if len(fields) != 2 {
			return nil, errors.New("Git returned an invalid remote configuration")
		}
		key := fields[0]
		push := strings.HasSuffix(key, ".pushurl")
		suffix := ".url"
		if push {
			suffix = ".pushurl"
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), suffix)
		index, exists := indices[name]
		if !exists {
			index = len(result)
			indices[name] = index
			result = append(result, Remote{Name: name})
		}
		value := redactCredentials(fields[1])
		if push && result[index].PushURL == "" {
			result[index].PushURL = value
		}
		if !push && result[index].FetchURL == "" {
			result[index].FetchURL = value
		}
	}
	for i := range result {
		if result[i].PushURL == "" {
			result[i].PushURL = result[i].FetchURL
		}
		result[i].Host, result[i].Owner, result[i].Repository, result[i].Provider = remoteMetadata(result[i].FetchURL)
	}
	return result, nil
}

var scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?(\[[^\]]+\]|[^:/]+):(.+)$`)

func remoteMetadata(raw string) (host, owner, repository, provider string) {
	provider = "local"
	var path string
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return
		}
		if strings.EqualFold(parsed.Scheme, "file") {
			return
		}
		host, path = strings.ToLower(parsed.Host), parsed.Path
	} else if match := scpRemote.FindStringSubmatch(raw); len(match) == 3 && !filepath.IsAbs(raw) {
		host, path = strings.ToLower(match[1]), match[2]
	} else {
		return
	}
	segments := strings.Split(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if len(segments) >= 2 {
		owner, repository = strings.Join(segments[:len(segments)-1], "/"), segments[len(segments)-1]
	}
	switch host {
	case "github.com":
		provider = "github"
	case "gitlab.com":
		provider = "gitlab"
	case "bitbucket.org":
		provider = "bitbucket"
	default:
		provider = "unknown"
	}
	return
}
