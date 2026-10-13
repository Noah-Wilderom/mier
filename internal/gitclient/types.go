package gitclient

type RepositorySnapshot struct {
	Root          string       `json:"root"`
	Name          string       `json:"name"`
	CurrentBranch string       `json:"currentBranch"`
	Detached      bool         `json:"detached"`
	HasHead       bool         `json:"hasHead"`
	HeadSHA       string       `json:"headSha"`
	Upstream      string       `json:"upstream"`
	Ahead         int          `json:"ahead"`
	Behind        int          `json:"behind"`
	Remotes       []Remote     `json:"remotes"`
	Branches      []Branch     `json:"branches"`
	History       []Commit     `json:"history"`
	Changes       []FileStatus `json:"changes"`
}

type Remote struct {
	Name       string `json:"name"`
	FetchURL   string `json:"fetchUrl"`
	PushURL    string `json:"pushUrl"`
	Host       string `json:"host"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Provider   string `json:"provider"`
}

type Branch struct {
	Name      string `json:"name"`
	Current   bool   `json:"current"`
	Remote    bool   `json:"remote"`
	Upstream  string `json:"upstream"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	CommitSHA string `json:"commitSha"`
}

type Commit struct {
	SHA         string   `json:"sha"`
	Parents     []string `json:"parents"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	AuthorName  string   `json:"authorName"`
	AuthorEmail string   `json:"authorEmail"`
	CreatedAt   string   `json:"createdAt"`
}

type FileStatus struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"originalPath,omitempty"`
	IndexStatus    string `json:"indexStatus"`
	WorktreeStatus string `json:"worktreeStatus"`
	Status         string `json:"status"`
	Staged         bool   `json:"staged"`
	Unstaged       bool   `json:"unstaged"`
	Conflict       bool   `json:"conflict"`
	Untracked      bool   `json:"untracked"`
	Submodule      string `json:"submodule,omitempty"`
}

type DiffLine struct {
	Kind    string `json:"kind"`
	OldLine *int   `json:"oldLine,omitempty"`
	NewLine *int   `json:"newLine,omitempty"`
	Text    string `json:"text"`
}

type ChangedFile struct {
	Path         string     `json:"path"`
	Status       string     `json:"status"`
	PreviousPath string     `json:"previousPath,omitempty"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	Lines        []DiffLine `json:"lines"`
	RawPatch     string     `json:"rawPatch"`
	Binary       bool       `json:"binary"`
	Truncated    bool       `json:"truncated"`
}
