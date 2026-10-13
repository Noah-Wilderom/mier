package lspclient

import "context"

type ServerConfiguration struct {
	Command           string   `json:"command"`
	Args              []string `json:"args"`
	TypeScriptSDKPath string   `json:"typescriptSdkPath"`
	VuePluginPath     string   `json:"vuePluginPath"`
}
type ServerStatus struct {
	Language      string              `json:"language"`
	Name          string              `json:"name"`
	Available     bool                `json:"available"`
	Command       string              `json:"command"`
	Args          []string            `json:"args"`
	Source        string              `json:"source"`
	SetupURL      string              `json:"setupUrl"`
	Error         string              `json:"error"`
	Configuration ServerConfiguration `json:"configuration"`
}
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type CodePositionRequest struct {
	Root           string  `json:"root"`
	Revision       string  `json:"revision"`
	ComparisonHead string  `json:"comparisonHead,omitempty"`
	Path           string  `json:"path"`
	Host           string  `json:"host"`
	Owner          string  `json:"owner"`
	Repo           string  `json:"repo"`
	PullRequest    int     `json:"pullRequest"`
	Line           int     `json:"line"`
	Character      int     `json:"character"`
	LineText       *string `json:"lineText,omitempty"`
}
type Hover struct {
	Contents string `json:"contents"`
	Kind     string `json:"kind"`
	Range    *Range `json:"range,omitempty"`
	Revision string `json:"revision"`
	Language string `json:"language"`
}
type DefinitionTarget struct {
	ID       string `json:"id"`
	URI      string `json:"uri"`
	Path     string `json:"path"`
	Range    Range  `json:"range"`
	Revision string `json:"revision"`
	Language string `json:"language"`
	External bool   `json:"external"`
}
type ReadDefinitionRequest struct {
	Root     string `json:"root"`
	Revision string `json:"revision"`
	Target   string `json:"target"`
}
type DefinitionDocument struct {
	URI      string `json:"uri"`
	Text     string `json:"text"`
	Path     string `json:"path"`
	Language string `json:"language"`
	Revision string `json:"revision"`
	Range    Range  `json:"range"`
	External bool   `json:"external"`
}
type Identity struct {
	Host, Owner, Repo string
	PullRequest       int
}
type Options struct {
	Archive        func(context.Context, CodePositionRequest) (ArchiveResult, error)
	ConfigPath     string
	CacheDirectory string
}
type ArchiveResult struct {
	Revision string
	Tar      []byte
}
