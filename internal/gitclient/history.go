package gitclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const commitFormat = "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%s%x00%b%x00"

func (c *Client) CompareExact(ctx context.Context, path, base, head string) ([]ChangedFile, error) {
	var files []ChangedFile
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if !objectID.MatchString(base) || !objectID.MatchString(head) {
			return errors.New("Choose full commit IDs for this comparison")
		}
		for _, sha := range []string{base, head} {
			if _, err := c.resolveRevision(ctx, root, sha); err != nil {
				return err
			}
		}
		var err error
		files, err = c.diffRevisions(ctx, root, []string{base, head})
		return err
	})
	return files, err
}

type HistoryQuery struct {
	Revision string `json:"revision"`
	Query    string `json:"query"`
	Search   string `json:"search"`
	Path     string `json:"path"`
	Cursor   string `json:"cursor"`
	Limit    int    `json:"limit"`
}
type HistoryPage struct {
	Commits    []Commit `json:"commits"`
	NextCursor string   `json:"nextCursor"`
}
type historyCursor struct {
	Offset int
	Key    string
}

var shortObjectID = regexp.MustCompile(`^[a-fA-F0-9]{4,64}$`)

// Paging is tied to the refs and query used by the first page. A concurrent
// fetch/commit cannot silently introduce missing or duplicate history rows.
func (c *Client) HistoryPage(ctx context.Context, path string, query HistoryQuery) (HistoryPage, error) {
	page := HistoryPage{Commits: []Commit{}}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		if query.Limit == 0 {
			query.Limit = 100
		}
		if query.Limit < 1 || query.Limit > 200 || len(query.Query) > 500 || strings.ContainsRune(query.Query, 0) {
			return errors.New("Invalid history search")
		}
		refs := []string{"--exclude=refs/stash", "--exclude=refs/mier/*", "--all", "HEAD"}
		state, err := c.status(ctx, root)
		if err != nil {
			return err
		}
		if !state.HasHead {
			return nil
		}
		identity := state.HeadSHA
		if query.Revision != "" {
			sha, err := c.resolveRevision(ctx, root, query.Revision)
			if err != nil {
				return err
			}
			refs = []string{sha}
			identity = sha
		} else {
			out, err := c.run(ctx, root, c.options.LocalTimeout, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/remotes", "refs/tags")
			if err != nil {
				return err
			}
			identity += string(out)
		}
		files := []string{}
		if query.Path != "" {
			file, err := validateFile(root, query.Path)
			if err != nil {
				return err
			}
			files = append(files, file)
		}
		key := digest(identity, query.Revision, query.Query, query.Search, query.Path, strconv.Itoa(query.Limit))
		cursor := historyCursor{Key: key}
		if query.Cursor != "" {
			data, err := base64.RawURLEncoding.DecodeString(query.Cursor)
			if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > 1000000 || cursor.Key != key {
				return errors.New("History changed. Refresh the search before loading more commits")
			}
		}
		args := []string{"log", fmt.Sprintf("-%d", query.Limit+1), "--skip=" + strconv.Itoa(cursor.Offset), "--date-order", "-z", "--no-show-signature", "--no-decorate", commitFormat}
		if query.Path != "" {
			args = append(args, "--follow", "--name-status")
		}
		if query.Query != "" {
			switch query.Search {
			case "", "message":
				args = append(args, "--fixed-strings", "--regexp-ignore-case", "--grep="+query.Query)
			case "author":
				args = append(args, "--fixed-strings", "--regexp-ignore-case", "--author="+query.Query)
			case "sha":
				if !shortObjectID.MatchString(query.Query) {
					return errors.New("Enter at least four hexadecimal characters for a commit ID")
				}
				sha, err := c.resolveRevision(ctx, root, query.Query)
				if err != nil {
					return err
				}
				refs = []string{sha}
				args = append(args, "--no-walk")
			default:
				return errors.New("Choose message, author, or commit ID search")
			}
		}
		args = append(args, refs...)
		args = append(args, "--")
		args = append(args, files...)
		out, err := c.run(ctx, root, c.options.LocalTimeout, args...)
		if err != nil {
			return err
		}
		if query.Path != "" {
			page.Commits, err = parseFileCommits(out)
		} else {
			page.Commits, err = parseCommits(out)
		}
		if err != nil {
			return err
		}
		if len(page.Commits) > query.Limit {
			page.Commits = page.Commits[:query.Limit]
			next, _ := json.Marshal(historyCursor{Offset: cursor.Offset + query.Limit, Key: key})
			page.NextCursor = base64.RawURLEncoding.EncodeToString(next)
		}
		return nil
	})
	return page, err
}

func parseFileCommits(output []byte) ([]Commit, error) {
	fields := strings.Split(string(output), "\x00")
	result := []Commit{}
	for i := 0; i < len(fields); {
		if fields[i] == "" {
			i++
			continue
		}
		if !objectID.MatchString(fields[i]) || len(fields)-i < 7 {
			return nil, errors.New("Git returned invalid file history")
		}
		commits, err := parseCommits([]byte(strings.Join(fields[i:i+7], "\x00") + "\x00"))
		if err != nil {
			return nil, err
		}
		commit := commits[0]
		i += 7
		for i < len(fields) {
			if fields[i] == "" {
				i++
				continue
			}
			if objectID.MatchString(fields[i]) {
				break
			}
			status := strings.TrimPrefix(fields[i], "\n")
			i++
			if status == "" {
				continue
			}
			if i >= len(fields) {
				return nil, errors.New("Incomplete file history")
			}
			commit.FilePath = fields[i]
			i++
			if status[0] == 'R' || status[0] == 'C' {
				if i >= len(fields) {
					return nil, errors.New("Incomplete rename history")
				}
				commit.FilePath = fields[i]
				i++
			}
		}
		result = append(result, commit)
	}
	return result, nil
}

type RevisionFile struct {
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
}

func (c *Client) revisionFile(ctx context.Context, root, revision, file string) (RevisionFile, error) {
	file, err := validateFile(root, file)
	if err != nil {
		return RevisionFile{}, err
	}
	sha, err := c.resolveRevision(ctx, root, revision)
	if err != nil {
		return RevisionFile{}, err
	}
	size, err := c.run(ctx, root, c.options.LocalTimeout, "cat-file", "-s", sha+":"+file)
	if err != nil {
		return RevisionFile{}, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
	if err != nil {
		return RevisionFile{}, err
	}
	if n > c.options.MaxFileBytes {
		return RevisionFile{}, errors.New("This file is too large to display")
	}
	out, err := c.run(ctx, root, c.options.LocalTimeout, "show", sha+":"+file)
	if err != nil {
		return RevisionFile{}, err
	}
	binary := strings.ContainsRune(string(out), 0)
	content := string(out)
	if binary {
		content = ""
	}
	return RevisionFile{Path: file, SHA: sha, Content: content, Binary: binary}, nil
}
func (c *Client) RevisionFile(ctx context.Context, path, revision, file string) (RevisionFile, error) {
	var result RevisionFile
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		var err error
		result, err = c.revisionFile(ctx, root, revision, file)
		return err
	})
	return result, err
}

type BlameLine struct {
	SHA          string `json:"sha"`
	OriginalLine int    `json:"originalLine"`
	Line         int    `json:"line"`
	Text         string `json:"text"`
	Author       string `json:"author"`
	CreatedAt    string `json:"createdAt"`
	Summary      string `json:"summary"`
}

func (c *Client) Blame(ctx context.Context, path, revision, file string) ([]BlameLine, error) {
	result := []BlameLine{}
	err := c.withRepository(ctx, path, c.options.LocalTimeout, func(ctx context.Context, root string) error {
		document, err := c.revisionFile(ctx, root, revision, file)
		if err != nil {
			return err
		}
		if document.Binary {
			return errors.New("Blame is available for text files")
		}
		out, err := c.run(ctx, root, c.options.LocalTimeout, "blame", "--line-porcelain", document.SHA, "--", document.Path)
		if err != nil {
			return err
		}
		var current BlameLine
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && objectID.MatchString(fields[0]) {
				old, e1 := strconv.Atoi(fields[1])
				number, e2 := strconv.Atoi(fields[2])
				if e1 != nil || e2 != nil {
					return errors.New("Invalid blame line")
				}
				current = BlameLine{SHA: fields[0], OriginalLine: old, Line: number}
				continue
			}
			switch {
			case strings.HasPrefix(line, "author "):
				current.Author = strings.TrimPrefix(line, "author ")
			case strings.HasPrefix(line, "author-time "):
				if seconds, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
					current.CreatedAt = time.Unix(seconds, 0).UTC().Format(time.RFC3339)
				}
			case strings.HasPrefix(line, "summary "):
				current.Summary = strings.TrimPrefix(line, "summary ")
			case strings.HasPrefix(line, "\t"):
				current.Text = line[1:]
				result = append(result, current)
			}
		}
		return nil
	})
	return result, err
}
