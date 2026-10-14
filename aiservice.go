package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type AIPreferences struct {
	CodexPath   string `json:"codexPath"`
	CopilotPath string `json:"copilotPath"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
}
type AIReviewRequest struct {
	Host         string `json:"host"`
	Owner        string `json:"owner"`
	Repository   string `json:"repository"`
	PullRequest  int    `json:"pullRequest"`
	HeadSHA      string `json:"headSha"`
	BaseSHA      string `json:"baseSha"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Instructions string `json:"instructions"`
}
type AIConnection struct {
	Run   reviewagent.RunSnapshot `json:"run"`
	URL   string                  `json:"url"`
	Token string                  `json:"token"`
	Tools []string                `json:"tools"`
}
type AILogin struct {
	LoginID string `json:"loginId"`
	URL     string `json:"url"`
	Code    string `json:"code"`
}
type AILoginStatus struct {
	State string `json:"state"`
	Error string `json:"error"`
}
type aiTask struct {
	run    *reviewagent.Run
	bridge *reviewagent.Bridge
	cancel context.CancelFunc
}

type AIService struct {
	github                *GitHubService
	credentials           *CredentialService
	mu                    sync.Mutex
	tasks                 map[string]*aiTask
	active                map[string]string
	preferences           AIPreferences
	directory, configPath string
	initErr               error
	preferencesErr        error
	closed                bool
	wg                    sync.WaitGroup
	authMu                sync.Mutex
	auth                  *reviewagent.Codex
	authPath              string
	loginID               string
	loginMu               sync.Mutex
	loginStates           map[string]AILoginStatus
}

func NewAIService(github *GitHubService, credentials *CredentialService) *AIService {
	config, err := os.UserConfigDir()
	cache, cacheErr := os.UserCacheDir()
	if err == nil {
		err = cacheErr
	}
	s := &AIService{github: github, credentials: credentials, tasks: map[string]*aiTask{}, active: map[string]string{}, loginStates: map[string]AILoginStatus{}, preferences: AIPreferences{Provider: "codex"}, directory: filepath.Join(cache, "mag-ik-een-review", "ai"), configPath: filepath.Join(config, "mag-ik-een-review", "ai.json"), initErr: err}
	if data, readErr := os.ReadFile(s.configPath); readErr == nil {
		if json.Unmarshal(data, &s.preferences) != nil {
			s.preferencesErr = errors.New("AI preferences could not be read. Save provider settings to reset them")
			s.preferences = AIPreferences{Provider: "codex"}
		}
	}
	return s
}
func (s *AIService) GetPreferences() (AIPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.preferences, s.initErr
	}
	return s.preferences, s.preferencesErr
}
func (s *AIService) SavePreferences(input AIPreferences) error {
	if input.Provider != "codex" && input.Provider != "copilot" {
		return errors.New("Choose Codex or GitHub Copilot")
	}
	if len(input.Model) > 200 || strings.ContainsAny(input.Model, "\x00\r\n") {
		return errors.New("Choose a valid provider model")
	}
	for _, path := range []string{input.CodexPath, input.CopilotPath} {
		if path != "" && (!filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n")) {
			return errors.New("Custom CLI paths must be absolute file paths without arguments")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initErr != nil {
		return s.initErr
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(input, "", "  ")
	file, err := os.CreateTemp(filepath.Dir(s.configPath), "ai-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.configPath); err != nil {
		return err
	}
	s.preferences = input
	s.preferencesErr = nil
	return nil
}
func (s *AIService) cli(provider string) (string, error) {
	s.mu.Lock()
	prefs := s.preferences
	closed := s.closed
	s.mu.Unlock()
	if s.initErr != nil {
		return "", s.initErr
	}
	if closed {
		return "", errors.New("M.I.E.R. is shutting down")
	}
	name, custom := "codex", prefs.CodexPath
	if provider == "copilot" {
		name, custom = "copilot", prefs.CopilotPath
	} else if provider != "codex" {
		return "", errors.New("Choose a supported AI provider")
	}
	if custom != "" {
		resolved, err := exec.LookPath(custom)
		if err != nil {
			return "", fmt.Errorf("The configured %s CLI is unavailable", name)
		}
		return resolved, nil
	}
	if resolved, err := exec.LookPath(name); err == nil {
		return resolved, nil
	}
	home, _ := os.UserHomeDir()
	for _, directory := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".bun", "bin"), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "share", "mier-ai", "node_modules", ".bin")} {
		if resolved, err := exec.LookPath(filepath.Join(directory, name)); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("Install the %s CLI, then retry detection or set its executable path", name)
}
func (s *AIService) privateDirectory(name string) (string, error) {
	directory := filepath.Join(s.directory, name)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}
func (s *AIService) authClient() (*reviewagent.Codex, error) {
	binary, err := s.cli("codex")
	if err != nil {
		return nil, err
	}
	if s.auth != nil && s.authPath == binary {
		return s.auth, nil
	}
	if s.auth != nil {
		s.auth.Close()
		s.auth = nil
	}
	directory, err := s.privateDirectory("codex-control")
	if err != nil {
		return nil, err
	}
	client, err := reviewagent.NewCodex(context.Background(), binary, directory, appVersion, s.authNotification)
	if err != nil {
		return nil, err
	}
	s.auth, s.authPath = client, binary
	return client, nil
}
func (s *AIService) authNotification(method string, params json.RawMessage) {
	if method != "account/login/completed" {
		return
	}
	var event struct {
		LoginID string
		Success bool
		Error   string
	}
	if json.Unmarshal(params, &event) != nil || event.LoginID == "" {
		return
	}
	status := AILoginStatus{State: "completed"}
	if !event.Success {
		status = AILoginStatus{State: "failed", Error: sanitizeAIError(event.Error)}
		if status.Error == "" {
			status.Error = "ChatGPT sign-in failed or expired. Start a new sign-in."
		}
	}
	s.loginMu.Lock()
	s.loginStates[event.LoginID] = status
	s.loginMu.Unlock()
}
func (s *AIService) GetCodexLoginStatus(id string) (AILoginStatus, error) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	status, ok := s.loginStates[id]
	if !ok {
		return AILoginStatus{}, errors.New("This sign-in attempt is no longer available. Start a new sign-in.")
	}
	return status, nil
}
func (s *AIService) copilotToken(ctx context.Context) (string, error) {
	_, token, _, err := s.credentials.resolveToken(ctx, "github.com")
	if err != nil {
		return "", errors.New("Connect a GitHub account using GitHub CLI or an OAuth token before using Copilot")
	}
	if strings.HasPrefix(token, "ghp_") {
		return "", errors.New("Copilot needs GitHub OAuth or a fine-grained Copilot token. Use gh auth login; classic PATs are not supported")
	}
	return token, nil
}
func (s *AIService) GetProviderStatus(ctx context.Context, provider string) (reviewagent.ProviderStatus, error) {
	result := reviewagent.ProviderStatus{Provider: provider, Models: []reviewagent.Model{}}
	binary, err := s.cli(provider)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Installed, result.Executable = true, binary
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if provider == "codex" {
		s.authMu.Lock()
		defer s.authMu.Unlock()
		client, err := s.authClient()
		if err == nil {
			result, err = reviewagent.CodexStatus(ctx, client)
		}
		result.Executable = binary
		if err != nil {
			result.Error = sanitizeAIError(err.Error())
			if s.auth != nil {
				s.auth.Close()
				s.auth = nil
			}
		}
		return result, nil
	}
	token, err := s.copilotToken(ctx)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	directory, err := s.privateDirectory("copilot-control")
	if err != nil {
		return result, err
	}
	client := reviewagent.NewCopilot(binary, directory, token)
	defer client.Stop()
	result, err = reviewagent.CopilotStatus(ctx, client)
	result.Executable = binary
	if err != nil {
		result.Error = strings.ReplaceAll(sanitizeAIError(err.Error()), token, "[redacted]")
	}
	return result, nil
}

func (s *AIService) BeginCodexLogin(ctx context.Context) (AILogin, error) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	client, err := s.authClient()
	if err != nil {
		return AILogin{}, err
	}
	if s.loginID != "" {
		_ = client.Call(ctx, "account/login/cancel", map[string]string{"loginId": s.loginID}, nil)
		s.loginID = ""
	}
	var login struct{ Type, LoginID, VerificationURL, VerificationURI, UserCode string }
	if err := client.Call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &login); err != nil {
		return AILogin{}, err
	}
	if login.VerificationURL == "" {
		login.VerificationURL = login.VerificationURI
	}
	if login.LoginID == "" || login.VerificationURL == "" || login.UserCode == "" {
		return AILogin{}, errors.New("Update Codex to a version supporting device-code sign-in")
	}
	s.loginID = login.LoginID
	s.loginMu.Lock()
	if len(s.loginStates) > 20 {
		s.loginStates = map[string]AILoginStatus{}
	}
	if _, ok := s.loginStates[login.LoginID]; !ok {
		s.loginStates[login.LoginID] = AILoginStatus{State: "waiting"}
	}
	s.loginMu.Unlock()
	return AILogin{LoginID: login.LoginID, URL: login.VerificationURL, Code: login.UserCode}, nil
}
func (s *AIService) CancelCodexLogin(ctx context.Context, id string) error {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if id == "" || id != s.loginID {
		return errors.New("This sign-in attempt is no longer active")
	}
	if s.auth == nil {
		return errors.New("Codex is no longer running")
	}
	err := s.auth.Call(ctx, "account/login/cancel", map[string]string{"loginId": id}, nil)
	if err == nil {
		s.loginMu.Lock()
		s.loginStates[id] = AILoginStatus{State: "cancelled"}
		s.loginMu.Unlock()
		s.loginID = ""
	}
	return err
}

func (s *AIService) StartReview(ctx context.Context, request AIReviewRequest) (reviewagent.RunSnapshot, error) {
	if request.Provider != "codex" && request.Provider != "copilot" {
		return reviewagent.RunSnapshot{}, errors.New("Choose Codex or GitHub Copilot")
	}
	connection, err := s.start(ctx, request, false)
	if err != nil {
		return reviewagent.RunSnapshot{}, err
	}
	return connection.Run, nil
}
func (s *AIService) StartMCPReview(ctx context.Context, request AIReviewRequest) (AIConnection, error) {
	return s.start(ctx, request, true)
}
func (s *AIService) start(ctx context.Context, request AIReviewRequest, external bool) (AIConnection, error) {
	if len(request.Instructions) > 4000 || len(request.Model) > 200 {
		return AIConnection{}, errors.New("Review instructions or model name exceed the supported length")
	}
	if request.PullRequest < 1 || request.HeadSHA == "" || request.BaseSHA == "" {
		return AIConnection{}, errors.New("Open a fully loaded pull request first")
	}
	key := fmt.Sprintf("%s/%s/%s#%d@%s:%s", strings.ToLower(request.Host), strings.ToLower(request.Owner), strings.ToLower(request.Repository), request.PullRequest, request.HeadSHA, request.BaseSHA)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return AIConnection{}, errors.New("M.I.E.R. is shutting down")
	}
	if s.active[key] != "" {
		s.mu.Unlock()
		return AIConnection{}, errors.New("A reviewer is already running for this pull request revision")
	}
	if len(s.active) >= 4 {
		s.mu.Unlock()
		return AIConnection{}, errors.New("Four reviews are already active. Stop or finish a review first")
	}
	s.active[key] = "preparing"
	s.mu.Unlock()
	registered := false
	defer func() {
		if !registered {
			s.mu.Lock()
			delete(s.active, key)
			s.mu.Unlock()
		}
	}()
	client, err := s.github.client(ctx, request.Host)
	if err != nil {
		return AIConnection{}, err
	}
	changes, err := client.ReviewChanges(ctx, request.Owner, request.Repository, request.PullRequest, request.HeadSHA, request.BaseSHA)
	if err != nil {
		return AIConnection{}, err
	}
	scope := reviewagent.Context{Key: key, Host: request.Host, Owner: request.Owner, Repository: request.Repository, PullRequest: request.PullRequest, HeadSHA: request.HeadSHA, BaseSHA: request.BaseSHA, MergeBaseSHA: changes.MergeBaseSHA, Title: changes.PullRequest.Title, Description: changes.PullRequest.Body, Files: []reviewagent.File{}, Warnings: []string{}}
	bytes := 0
	for _, file := range changes.Files {
		if len(scope.Files) >= 64 || bytes+len(file.Patch) > 384*1024 {
			scope.Warnings = append(scope.Warnings, "Review scope is limited to 64 files and 384 KiB of diff. Review remaining changes manually.")
			break
		}
		if file.Patch == "" {
			scope.Warnings = append(scope.Warnings, "No text patch for "+file.Path+"; this file needs manual review.")
		}
		scope.Files = append(scope.Files, reviewagent.File{Path: file.Path, PreviousPath: file.PreviousPath, Status: file.Status, Patch: file.Patch})
		bytes += len(file.Patch)
	}
	if len(scope.Files) == 0 {
		return AIConnection{}, errors.New("This pull request has no text changes available for AI review")
	}
	provider := request.Provider
	model := request.Model
	if external {
		provider = "external"
		// The external client chooses its own model; never attribute the last
		// selected built-in provider's model to that independent reviewer.
		model = ""
	}
	reader := func(ctx context.Context, path, side string, start, count int) (reviewagent.Source, error) {
		revision := scope.HeadSHA
		owner, repository := scope.Owner, scope.Repository
		if side == "old" {
			revision = scope.MergeBaseSHA
			for _, file := range scope.Files {
				if file.Path == path && file.PreviousPath != "" {
					path = file.PreviousPath
					break
				}
			}
		} else if parts := strings.Split(changes.PullRequest.Head.Repository, "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			owner, repository = parts[0], parts[1]
		}
		text, err := client.ReadFileAtCommit(ctx, owner, repository, path, revision)
		if err != nil {
			return reviewagent.Source{}, err
		}
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		if start > len(lines) {
			return reviewagent.Source{}, errors.New("Start line is beyond the end of this file")
		}
		end := min(len(lines), start-1+count)
		return reviewagent.Source{Path: path, Revision: revision, StartLine: start, Lines: lines[start-1 : end], TotalLines: len(lines)}, nil
	}
	run := reviewagent.NewRun(provider, model, scope, reader)
	bridge, err := reviewagent.StartBridge(run)
	if err != nil {
		return AIConnection{}, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	run.SetCancel(cancel)
	task := &aiTask{run: run, bridge: bridge, cancel: cancel}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		bridge.Close()
		return AIConnection{}, errors.New("M.I.E.R. is shutting down")
	}
	s.tasks[run.Snapshot().ID] = task
	s.active[key] = run.Snapshot().ID
	s.wg.Add(1)
	registered = true
	s.mu.Unlock()
	go s.execute(runCtx, request, task, external)
	return AIConnection{Run: run.Snapshot(), URL: bridge.URL, Token: bridge.Token, Tools: append([]string{}, reviewagent.ToolNames...)}, nil
}
func (s *AIService) execute(ctx context.Context, request AIReviewRequest, task *aiTask, external bool) {
	defer s.wg.Done()
	defer task.cancel()
	defer task.bridge.Close()
	run := task.run
	key := run.Snapshot().Key
	defer func() {
		s.mu.Lock()
		delete(s.active, key)
		if len(s.tasks) > 20 {
			for id, old := range s.tasks {
				state := old.run.Snapshot().State
				if state == "completed" || state == "failed" || state == "cancelled" {
					delete(s.tasks, id)
					if len(s.tasks) <= 20 {
						break
					}
				}
			}
		}
		s.mu.Unlock()
	}()
	if external {
		run.Progress("Waiting for your MCP client")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				run.Finish("cancelled", "", "MCP review stopped or expired.")
				return
			case <-ticker.C:
				if run.Snapshot().State == "completed" {
					return
				}
			}
		}
	}
	binary, err := s.cli(request.Provider)
	directory := ""
	if err == nil {
		var runs string
		runs, err = s.privateDirectory("runs")
		if err == nil {
			directory, err = os.MkdirTemp(runs, "review-")
		}
	}
	if directory != "" {
		defer os.RemoveAll(directory)
	}
	prompt := "Review the captured pull request using the mier MCP tools. Start with get_review_context. Add actionable comments with add_review_comment and record a summary with finish_review."
	if request.Instructions != "" {
		prompt += "\nAdditional reviewer focus: " + request.Instructions
	}
	secret := ""
	if err == nil {
		if request.Provider == "codex" {
			err = reviewagent.RunCodex(ctx, binary, directory, appVersion, request.Model, prompt, task.bridge, run)
		} else {
			secret, err = s.copilotToken(ctx)
			if err == nil {
				err = reviewagent.RunCopilot(ctx, binary, directory, secret, request.Model, prompt, task.bridge, run)
			}
		}
	}
	if ctx.Err() != nil {
		run.Finish("cancelled", "", "Review cancelled or reached its 30-minute limit. Existing suggestions have been kept.")
		return
	}
	if err != nil {
		message := sanitizeAIError(err.Error())
		for _, secret := range []string{secret, task.bridge.Token} {
			if secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		run.Finish("failed", "", message)
		return
	}
	if !run.HasContext() {
		run.Finish("failed", "", "The provider completed without reading the review context. Check its MCP support and retry.")
		return
	}
	run.Finish("completed", "", "")
}
func (s *AIService) GetReview(id string) (reviewagent.RunSnapshot, error) {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil {
		return reviewagent.RunSnapshot{}, errors.New("Review session not found. Start a new review")
	}
	return task.run.Snapshot(), nil
}

// The bearer stays in memory and is only exposed for an explicitly created
// external review. Restoring the pane never creates a second server or token.
func (s *AIService) GetMCPConnection(id string) (AIConnection, error) {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil {
		return AIConnection{}, errors.New("Review session not found")
	}
	run := task.run.Snapshot()
	if run.Provider != "external" || run.State != "preparing" && run.State != "running" {
		return AIConnection{}, errors.New("This MCP connection is no longer active")
	}
	return AIConnection{Run: run, URL: task.bridge.URL, Token: task.bridge.Token, Tools: append([]string{}, reviewagent.ToolNames...)}, nil
}
func (s *AIService) CancelReview(id string) error {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil {
		return errors.New("Review session not found")
	}
	task.run.Cancel()
	task.bridge.Close()
	return nil
}
func (s *AIService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	tasks := make([]*aiTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}
	s.mu.Unlock()
	for _, task := range tasks {
		task.run.Cancel()
		task.bridge.Close()
	}
	s.authMu.Lock()
	if s.auth != nil {
		s.auth.Close()
		s.auth = nil
	}
	s.authMu.Unlock()
	s.wg.Wait()
	return nil
}

var aiSecretPattern = regexp.MustCompile(`(?i)(?:bearer\s+[^\s"'<>]+|(?:gh[pousr]_|github_pat_|sk-)[a-zA-Z0-9_\-]{10,})`)

func sanitizeAIError(value string) string {
	value = aiSecretPattern.ReplaceAllString(value, "[redacted]")
	if len(value) > 4000 {
		value = value[:4000]
	}
	return value
}
