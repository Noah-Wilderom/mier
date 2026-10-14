package githubclient

import (
	"bytes"
	"encoding/json"
	"strings"
)

type User struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	URL       string `json:"url"`
}

func (u *User) UnmarshalJSON(data []byte) error {
	type plain User
	return decodeCamel(data, (*plain)(u))
}

type Branch struct {
	Ref        string `json:"ref"`
	SHA        string `json:"sha"`
	Label      string `json:"label"`
	Repository string `json:"repository"`
}

func (b *Branch) UnmarshalJSON(data []byte) error {
	type plain Branch
	var wire struct {
		plain
		Repo *struct {
			FullName string `json:"fullName"`
		} `json:"repo"`
	}
	if err := decodeCamel(data, &wire); err != nil {
		return err
	}
	*b = Branch(wire.plain)
	if wire.Repo != nil {
		b.Repository = wire.Repo.FullName
	}
	return nil
}

type PullRequestSummary struct {
	Number              int      `json:"number"`
	NodeID              string   `json:"nodeId"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	URL                 string   `json:"url"`
	Status              string   `json:"status"`
	Author              User     `json:"author"`
	CreatedAt           string   `json:"createdAt"`
	UpdatedAt           string   `json:"updatedAt"`
	Head                Branch   `json:"head"`
	Base                Branch   `json:"base"`
	Reviewers           []User   `json:"reviewers"`
	Assignees           []User   `json:"assignees"`
	Labels              []string `json:"labels"`
	Draft               bool     `json:"draft"`
	Merged              bool     `json:"merged"`
	Mergeable           *bool    `json:"mergeable"`
	MergeableState      string   `json:"mergeableState"`
	Additions           int      `json:"additions"`
	Deletions           int      `json:"deletions"`
	ChangedFiles        int      `json:"changedFiles"`
	CommitsCount        int      `json:"commitsCount"`
	CommentsCount       int      `json:"commentsCount"`
	ReviewCommentsCount int      `json:"reviewCommentsCount"`
	ThreadsCount        int      `json:"threadsCount"`
	CountsKnown         bool     `json:"countsKnown"`
	Warnings            []string `json:"warnings"`
}

func (p *PullRequestSummary) UnmarshalJSON(data []byte) error {
	type plain PullRequestSummary
	var wire struct {
		plain
		User               User    `json:"user"`
		State              string  `json:"state"`
		MergedAt           *string `json:"mergedAt"`
		RequestedReviewers []User  `json:"requestedReviewers"`
		Labels             []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Commits        *int `json:"commits"`
		Comments       *int `json:"comments"`
		ReviewComments *int `json:"reviewComments"`
	}
	if err := decodeCamel(data, &wire); err != nil {
		return err
	}
	*p = PullRequestSummary(wire.plain)
	p.Author = wire.User
	p.Status = wire.State
	p.Merged = p.Merged || wire.MergedAt != nil
	if p.Merged {
		p.Status = "merged"
	} else if p.Draft && wire.State == "open" {
		p.Status = "draft"
	}
	p.Reviewers = append([]User{}, wire.RequestedReviewers...)
	p.Labels = []string{}
	for _, label := range wire.Labels {
		p.Labels = append(p.Labels, label.Name)
	}
	if p.Assignees == nil {
		p.Assignees = []User{}
	}
	if wire.Commits != nil {
		p.CommitsCount = *wire.Commits
	}
	if wire.Comments != nil {
		p.CommentsCount = *wire.Comments
	}
	if wire.ReviewComments != nil {
		p.ReviewCommentsCount = *wire.ReviewComments
	}
	p.CountsKnown = wire.Commits != nil && wire.Comments != nil && wire.ReviewComments != nil
	p.Warnings = []string{}
	return nil
}

type ChangedFile struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Patch        string `json:"patch"`
}

func (f *ChangedFile) UnmarshalJSON(data []byte) error {
	type plain ChangedFile
	return decodeCamel(data, (*plain)(f))
}

type Commit struct {
	SHA        string `json:"sha"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	Author     User   `json:"author"`
	AuthorName string `json:"authorName"`
	CreatedAt  string `json:"createdAt"`
	URL        string `json:"url"`
}

func (c *Commit) UnmarshalJSON(data []byte) error {
	var wire struct {
		SHA    string `json:"sha"`
		URL    string `json:"url"`
		Author User   `json:"author"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := decodeCamel(data, &wire); err != nil {
		return err
	}
	*c = Commit{SHA: wire.SHA, URL: wire.URL, Author: wire.Author, Body: wire.Commit.Message, Subject: strings.Split(wire.Commit.Message, "\n")[0], AuthorName: wire.Commit.Author.Name, CreatedAt: wire.Commit.Author.Date}
	return nil
}

type Comment struct {
	ID           int64  `json:"id"`
	NodeID       string `json:"nodeId"`
	Body         string `json:"body"`
	User         User   `json:"user"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	URL          string `json:"url"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"originalLine"`
	Side         string `json:"side"`
	CommitID     string `json:"commitId"`
	InReplyToID  int64  `json:"inReplyToId"`
}

func (c *Comment) UnmarshalJSON(data []byte) error {
	type plain Comment
	return decodeCamel(data, (*plain)(c))
}

type Review struct {
	ID          int64  `json:"id"`
	NodeID      string `json:"nodeId"`
	Body        string `json:"body"`
	User        User   `json:"user"`
	State       string `json:"state"`
	CommitID    string `json:"commitId"`
	SubmittedAt string `json:"submittedAt"`
	URL         string `json:"url"`
}

func (r *Review) UnmarshalJSON(data []byte) error {
	type plain Review
	return decodeCamel(data, (*plain)(r))
}

type CheckRun struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	DetailsURL  string `json:"detailsUrl"`
	App         struct {
		Slug string `json:"slug"`
	} `json:"app"`
	CheckSuite struct {
		ID int64 `json:"id"`
	} `json:"checkSuite"`
	Output struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	} `json:"output"`
}

func (c *CheckRun) UnmarshalJSON(data []byte) error {
	type plain CheckRun
	return decodeCamel(data, (*plain)(c))
}

type CommitStatus struct {
	ID          int64  `json:"id"`
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	TargetURL   string `json:"targetUrl"`
	CreatedAt   string `json:"createdAt"`
}

func (s *CommitStatus) UnmarshalJSON(data []byte) error {
	type plain CommitStatus
	return decodeCamel(data, (*plain)(s))
}

type Thread struct {
	ID                 string    `json:"id"`
	Path               string    `json:"path"`
	Line               *int      `json:"line"`
	OriginalLine       *int      `json:"originalLine"`
	DiffSide           string    `json:"diffSide"`
	Resolved           bool      `json:"resolved"`
	Outdated           bool      `json:"outdated"`
	ViewerCanResolve   bool      `json:"viewerCanResolve"`
	ViewerCanUnresolve bool      `json:"viewerCanUnresolve"`
	Comments           []Comment `json:"comments"`
}

type PullRequestDetail struct {
	PullRequest                 PullRequestSummary `json:"pullRequest"`
	Files                       []ChangedFile      `json:"files"`
	Commits                     []Commit           `json:"commits"`
	Comments                    []Comment          `json:"comments"`
	Reviews                     []Review           `json:"reviews"`
	Checks                      []CheckRun         `json:"checks"`
	Statuses                    []CommitStatus     `json:"statuses"`
	Threads                     []Thread           `json:"threads"`
	MergeStateStatus            string             `json:"mergeStateStatus"`
	ReviewDecision              string             `json:"reviewDecision"`
	ViewerCanUpdate             bool               `json:"viewerCanUpdate"`
	AllowedMergeMethods         []string           `json:"allowedMergeMethods"`
	RequiredApprovals           *int               `json:"requiredApprovals"`
	RequiredStatusCheckContexts []string           `json:"requiredStatusCheckContexts"`
	Warnings                    []string           `json:"warnings"`
}

type CreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Base  string `json:"base"`
	Head  string `json:"head"`
	Draft bool   `json:"draft"`
}
type UpdateRequest struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
	State *string `json:"state,omitempty"`
}
type ReviewCommentRequest struct {
	Body     string `json:"body"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Side     string `json:"side"`
	CommitID string `json:"commitId,omitempty"`
}
type ReviewRequest struct {
	Event    string                 `json:"event"`
	Body     string                 `json:"body"`
	CommitID string                 `json:"commitId"`
	Comments []ReviewCommentRequest `json:"comments"`
}
type MergeResult struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}

func decodeCamel(data []byte, target any) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	converted, err := json.Marshal(camelValue(value))
	if err != nil {
		return err
	}
	return json.Unmarshal(converted, target)
}
func camelValue(value any) any {
	switch value := value.(type) {
	case []any:
		for i := range value {
			value[i] = camelValue(value[i])
		}
		return value
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			parts := strings.Split(key, "_")
			for i := 1; i < len(parts); i++ {
				if parts[i] != "" {
					parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
				}
			}
			name := strings.Join(parts, "")
			if key == "filename" {
				name = "path"
			}
			if key == "previous_filename" {
				name = "previousPath"
			}
			result[name] = camelValue(item)
		}
		if html, ok := value["html_url"]; ok {
			result["url"] = html
		}
		return result
	default:
		return value
	}
}
