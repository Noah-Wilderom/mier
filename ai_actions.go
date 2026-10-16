package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type AIActionRequest struct {
	Root          string  `json:"root"`
	Kind          string  `json:"kind"`
	Base          string  `json:"base"`
	Head          string  `json:"head"`
	ExpectedHead  string  `json:"expectedHead"`
	Path          string  `json:"path"`
	WorktreeID    string  `json:"worktreeId"`
	ClientID      string  `json:"clientId"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Instructions  string  `json:"instructions"`
	Intensity     string  `json:"intensity"`
	CurrentResult *string `json:"currentResult,omitempty"`
}
type AIWorktreeInfo struct {
	ID             string `json:"id"`
	RepositoryRoot string `json:"repositoryRoot"`
	Path           string `json:"path"`
	Branch         string `json:"branch"`
	HeadSHA        string `json:"headSha"`
	CreatedAt      string `json:"createdAt"`
	LastActionID   string `json:"lastActionId"`
	State          string `json:"state"`
}
type aiWorktreeRecord struct {
	AIWorktreeInfo
	CommonDirectory string `json:"commonDirectory"`
}

func (r aiWorktreeRecord) guard() gitclient.AgentWorktree {
	return gitclient.AgentWorktree{Path: r.Path, Branch: r.Branch, HeadSHA: r.HeadSHA, CommonDirectory: r.CommonDirectory}
}

type AIActionSnapshot struct {
	Run      reviewagent.RunSnapshot     `json:"run"`
	Kind     string                      `json:"kind"`
	Root     string                      `json:"root"`
	Proposal *reviewagent.ActionProposal `json:"proposal"`
	Worktree *AIWorktreeInfo             `json:"worktree,omitempty"`
	Writes   int                         `json:"writes"`
}

func (s *AIService) worktreeRecordsPath() string {
	return filepath.Join(filepath.Dir(s.configPath), "agent-worktrees.json")
}
func (s *AIService) loadWorktreesLocked() error {
	if s.worktreesLoaded {
		return s.worktreesErr
	}
	s.worktreesLoaded = true
	s.worktrees = map[string]aiWorktreeRecord{}
	if s.initErr != nil {
		s.worktreesErr = s.initErr
		return s.worktreesErr
	}
	data, err := readReviewSettingsFile(s.worktreeRecordsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		s.worktreesErr = err
		return err
	}
	var records []aiWorktreeRecord
	if json.Unmarshal(data, &records) != nil || len(records) > 200 {
		s.worktreesErr = errors.New("Agent worktree records are unreadable. Keep a backup before repairing " + s.worktreeRecordsPath())
		return s.worktreesErr
	}
	for _, record := range records {
		if record.ID == "" || len(record.ID) > 128 || !filepath.IsAbs(record.Path) || !filepath.IsAbs(record.RepositoryRoot) || record.CommonDirectory == "" || !strings.HasPrefix(record.Branch, "mier/agent/") {
			s.worktreesErr = errors.New("Saved agent worktree identity is incomplete")
			return s.worktreesErr
		}
		if record.State == "running" || record.State == "preparing" {
			record.State = "interrupted"
		}
		s.worktrees[record.ID] = record
	}
	return nil
}
func (s *AIService) saveWorktreesLocked() error {
	items := []aiWorktreeRecord{}
	for _, record := range s.worktrees {
		items = append(items, record)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return writePrivateJSON(s.worktreeRecordsPath(), items)
}
func (s *AIService) AgentWorktrees(root string) ([]AIWorktreeInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadWorktreesLocked(); err != nil {
		return nil, err
	}
	items := []AIWorktreeInfo{}
	for _, record := range s.worktrees {
		if root == "" || sameAIRepository(record.RepositoryRoot, root) {
			items = append(items, record.AIWorktreeInfo)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}
func (s *AIService) actionSnapshot(task *aiTask) AIActionSnapshot {
	result := AIActionSnapshot{Run: task.run.Snapshot(), Kind: task.actionKind, Root: task.capture.Root, Proposal: task.action.Proposal(), Writes: task.action.Writes()}
	if task.worktreeID != "" {
		s.mu.Lock()
		record, ok := s.worktrees[task.worktreeID]
		s.mu.Unlock()
		if ok {
			info := record.AIWorktreeInfo
			result.Worktree = &info
			result.Root = record.RepositoryRoot
		}
	}
	return result
}
func (s *AIService) GetAction(id string) (AIActionSnapshot, error) {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil || task.action == nil {
		return AIActionSnapshot{}, errors.New("AI task not found. Start a new task")
	}
	return s.actionSnapshot(task), nil
}
func (s *AIService) ValidateAction(ctx context.Context, id string) error {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil || task.action == nil || task.action.Proposal() == nil {
		return errors.New("This AI task has no complete proposal")
	}
	if task.actionKind == "code" {
		return nil
	}
	if s.repositories == nil || s.repositories.initErr != nil {
		return errors.New("Open a local Git repository first")
	}
	return s.repositories.client.ValidateAssistantCapture(ctx, task.capture)
}
func (s *AIService) CancelAction(id string) error {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task == nil || task.action == nil {
		return errors.New("AI task not found")
	}
	task.cancel()
	task.run.Finish("cancelled", "", "AI task stopped. Existing proposals and agent worktree edits have been kept.")
	task.bridge.Close()
	return nil
}

func actionContextWithRun(parent, run context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(run, cancel)
	return ctx, func() { stop(); cancel() }
}
func (s *AIService) StartAction(ctx context.Context, request AIActionRequest) (AIActionSnapshot, error) {
	if s.initErr != nil {
		return AIActionSnapshot{}, s.initErr
	}
	if s.repositories == nil || s.repositories.initErr != nil {
		return AIActionSnapshot{}, errors.New("Open a local Git repository before using AI tools")
	}
	if request.Provider != "codex" && request.Provider != "copilot" {
		return AIActionSnapshot{}, errors.New("Choose Codex or GitHub Copilot")
	}
	intensity, err := reviewagent.NormalizeIntensity(request.Intensity)
	if err != nil {
		return AIActionSnapshot{}, err
	}
	request.Intensity = intensity
	if len(request.Instructions) > 4000 || !utf8.ValidString(request.Instructions) || len(request.Model) > 200 || strings.ContainsAny(request.Model, "\x00\r\n") || len(request.ClientID) > 128 {
		return AIActionSnapshot{}, errors.New("AI instructions or model exceed their supported limit")
	}
	if request.CurrentResult != nil && len(*request.CurrentResult) > 256*1024 {
		return AIActionSnapshot{}, errors.New("The existing conflict result exceeds the AI text limit")
	}
	if request.WorktreeID != "" && request.Kind != "code" {
		return AIActionSnapshot{}, errors.New("Only coding tasks can resume an agent worktree")
	}
	if s.actionRunner == nil {
		if _, err := s.cli(request.Provider); err != nil {
			return AIActionSnapshot{}, err
		}
	}
	client := s.repositories.client
	captureRoot := request.Root
	var record aiWorktreeRecord
	if request.WorktreeID != "" {
		s.mu.Lock()
		err = s.loadWorktreesLocked()
		record = s.worktrees[request.WorktreeID]
		s.mu.Unlock()
		if err != nil {
			return AIActionSnapshot{}, err
		}
		if record.ID == "" || !sameAIRepository(record.RepositoryRoot, request.Root) {
			return AIActionSnapshot{}, errors.New("This agent worktree belongs to a different repository")
		}
		captureRoot = record.Path
		state, issue := client.Snapshot(ctx, record.Path)
		if issue != nil {
			return AIActionSnapshot{}, issue
		}
		guard, issue := client.PinAgentWorktree(ctx, record.RepositoryRoot, record.Path, record.Branch, state.HeadSHA)
		if issue != nil {
			return AIActionSnapshot{}, issue
		}
		if !sameAIPath(guard.CommonDirectory, record.CommonDirectory) {
			return AIActionSnapshot{}, errors.New("The saved agent worktree identity changed")
		}
		record.HeadSHA = state.HeadSHA
		request.ExpectedHead = state.HeadSHA
	}
	captured, err := client.AssistantCapture(ctx, captureRoot, gitclient.AssistantCaptureRequest{Kind: request.Kind, Base: request.Base, Head: request.Head, ExpectedHead: request.ExpectedHead, Path: request.Path})
	if err != nil {
		return AIActionSnapshot{}, err
	}
	if request.CurrentResult != nil && captured.Conflict != nil {
		copy := *captured.Conflict
		copy.Working = *request.CurrentResult
		captured.Conflict = &copy
	}
	key := "action:" + captured.Root + ":" + request.Kind + ":" + request.Path
	if request.Kind == "code" {
		if request.WorktreeID != "" {
			key = "agent:" + request.WorktreeID
		} else {
			if request.ClientID == "" {
				request.ClientID = reviewagent.ID()
			}
			key += "\x00" + request.ClientID
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return AIActionSnapshot{}, errors.New("M.I.E.R. is shutting down")
	}
	if len(s.active) >= 4 {
		s.mu.Unlock()
		return AIActionSnapshot{}, errors.New("Four AI tasks are already running. Stop one before starting another")
	}
	if _, exists := s.active[key]; exists {
		s.mu.Unlock()
		return AIActionSnapshot{}, errors.New("This AI task is already running")
	}
	if request.WorktreeID != "" {
		if saved, ok := s.worktrees[record.ID]; !ok || saved.Path != record.Path {
			s.mu.Unlock()
			return AIActionSnapshot{}, errors.New("The agent worktree was removed. Refresh its list")
		}
		for _, existing := range s.tasks {
			if existing.worktreeID == record.ID {
				if _, active := s.active[existing.run.Snapshot().Key]; active {
					s.mu.Unlock()
					return AIActionSnapshot{}, errors.New("The previous agent task is still stopping. Wait for it before starting a follow-up")
				}
			}
		}
	}
	s.active[key] = ""
	s.mu.Unlock()
	registered := false
	defer func() {
		if !registered {
			s.mu.Lock()
			delete(s.active, key)
			s.mu.Unlock()
		}
	}()
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	prepared := false
	defer func() {
		if !prepared {
			cancel()
		}
	}()
	if request.Kind == "code" && record.ID == "" {
		id := reviewagent.ID()
		branch := "mier/agent/" + id[:12]
		parent := filepath.Join(filepath.Dir(s.configPath), "agent-worktrees")
		if err := os.MkdirAll(parent, 0700); err != nil {
			return AIActionSnapshot{}, err
		}
		s.mu.Lock()
		err = s.loadWorktreesLocked()
		if err == nil && len(s.worktrees) >= 200 {
			err = errors.New("Review or remove existing agent worktrees before creating more")
		}
		if err == nil {
			err = s.saveWorktreesLocked()
		}
		s.mu.Unlock()
		if err != nil {
			return AIActionSnapshot{}, err
		}
		target := filepath.Join(parent, id)
		if err = client.MutateWorktree(ctx, captured.Root, gitclient.WorktreeRequest{Action: "add", Path: target, Mode: "new", Branch: branch, StartSHA: captured.HeadSHA, ExpectedHead: captured.HeadSHA, ExpectedBranch: captured.Branch}); err != nil {
			return AIActionSnapshot{}, err
		}
		guard, err := client.PinAgentWorktree(ctx, captured.Root, target, branch, captured.HeadSHA)
		if err != nil {
			return AIActionSnapshot{}, fmt.Errorf("The worktree was preserved at %s: %w", target, err)
		}
		record = aiWorktreeRecord{AIWorktreeInfo: AIWorktreeInfo{ID: id, RepositoryRoot: captured.Root, Path: guard.Path, Branch: branch, HeadSHA: captured.HeadSHA, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "ready"}, CommonDirectory: guard.CommonDirectory}
		s.mu.Lock()
		s.worktrees[id] = record
		err = s.saveWorktreesLocked()
		s.mu.Unlock()
		if err != nil {
			return AIActionSnapshot{}, fmt.Errorf("Agent worktree kept at %s, but its record could not be saved: %w", target, err)
		}
	}
	scope := reviewagent.Context{Key: key, HeadSHA: captured.HeadSHA, BaseSHA: captured.BaseSHA, MergeBaseSHA: captured.MergeBaseSHA, Title: "AI " + request.Kind, Warnings: captured.Warnings, Intensity: request.Intensity, Focus: request.Instructions}
	reader := func(ctx context.Context, path, side string, start, count int) (reviewagent.Source, error) {
		file, err := client.AssistantSource(ctx, captured, path, side)
		if err != nil {
			return reviewagent.Source{}, err
		}
		lines := strings.Split(strings.TrimSuffix(file.Content, "\n"), "\n")
		if start > len(lines) {
			return reviewagent.Source{}, errors.New("The requested line is beyond this captured source file")
		}
		end := min(len(lines), start-1+count)
		return reviewagent.Source{Path: path, Revision: file.SHA, StartLine: start, Lines: lines[start-1 : end], TotalLines: len(lines)}, nil
	}
	var worktree *reviewagent.WorktreeTools
	if record.ID != "" {
		guard := record.guard()
		worktree = &reviewagent.WorktreeTools{
			List: func(ctx context.Context, input reviewagent.WorktreeListInput) (reviewagent.WorktreeFilePage, error) {
				ctx, done := actionContextWithRun(ctx, runCtx)
				defer done()
				value, err := client.AgentFiles(ctx, guard, gitclient.AgentFileQuery{Prefix: input.Prefix, Cursor: input.Cursor})
				return reviewagent.WorktreeFilePage{Paths: value.Paths, NextCursor: value.NextCursor, Total: value.Total}, err
			},
			Read: func(ctx context.Context, path string) (reviewagent.WorkFile, error) {
				ctx, done := actionContextWithRun(ctx, runCtx)
				defer done()
				value, err := client.AgentRead(ctx, guard, path)
				return reviewagent.WorkFile{Path: value.Path, Content: value.Content, Hash: value.Hash, Exists: value.Exists}, err
			},
			Write: func(ctx context.Context, input reviewagent.WriteWorkFile) (reviewagent.WorkFile, error) {
				ctx, done := actionContextWithRun(ctx, runCtx)
				defer done()
				value, err := client.AgentWrite(ctx, guard, input.Path, input.ExpectedHash, input.Content)
				return reviewagent.WorkFile{Path: value.Path, Hash: value.Hash, Exists: value.Exists}, err
			},
		}
	}
	data, err := json.Marshal(captured)
	if err != nil {
		return AIActionSnapshot{}, err
	}
	action, err := reviewagent.NewAction(request.Provider, request.Model, scope, reviewagent.ActionContext{Kind: request.Kind, Instructions: request.Instructions, Input: string(data)}, reader, worktree)
	if err != nil {
		return AIActionSnapshot{}, err
	}
	bridge, err := reviewagent.StartActionBridge(action)
	if err != nil {
		return AIActionSnapshot{}, err
	}
	action.Run.SetCancel(cancel)
	task := &aiTask{run: action.Run, bridge: bridge, cancel: cancel, action: action, capture: captured, actionKind: request.Kind, worktreeID: record.ID}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		bridge.Close()
		return AIActionSnapshot{}, errors.New("M.I.E.R. is shutting down")
	}
	s.tasks[action.Run.Snapshot().ID] = task
	s.active[key] = action.Run.Snapshot().ID
	if record.ID != "" {
		record.LastActionID = action.Run.Snapshot().ID
		record.State = "running"
		s.worktrees[record.ID] = record
		err = s.saveWorktreesLocked()
	}
	if err != nil {
		delete(s.tasks, action.Run.Snapshot().ID)
		s.mu.Unlock()
		bridge.Close()
		return AIActionSnapshot{}, err
	}
	s.wg.Add(1)
	s.mu.Unlock()
	registered = true
	prepared = true
	go s.executeAction(runCtx, request, task)
	return s.actionSnapshot(task), nil
}

func (s *AIService) executeAction(ctx context.Context, request AIActionRequest, task *aiTask) {
	defer s.wg.Done()
	defer task.cancel()
	defer task.bridge.Close()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active, task.run.Snapshot().Key)
		if task.worktreeID != "" {
			record := s.worktrees[task.worktreeID]
			record.State = task.run.Snapshot().State
			s.worktrees[record.ID] = record
			_ = s.saveWorktreesLocked()
		}
		s.pruneAITasksLocked(task.run.Snapshot().ID)
	}()
	var err error
	secret := ""
	if s.actionRunner != nil {
		err = s.actionRunner(ctx, request, task.bridge, task.action)
	} else {
		var binary, directory string
		binary, err = s.cli(request.Provider)
		if err == nil {
			var parent string
			parent, err = s.privateDirectory("runs")
			if err == nil {
				directory, err = os.MkdirTemp(parent, "action-")
			}
		}
		if directory != "" {
			defer os.RemoveAll(directory)
		}
		prompt := "Complete the scoped " + request.Kind + " task using only the mier tools. Start with get_task_context, inspect relevant source, and return a typed proposal with finish_task. Keep changes and claims grounded in the captured input."
		prompt += "\nDepth preference: " + request.Intensity + ". Use concise output for low; inspect surrounding behavior for medium; inspect dependencies and edge cases for high. Do not invent findings or test results."
		if err == nil {
			if request.Provider == "codex" {
				err = reviewagent.RunCodex(ctx, binary, directory, appVersion, request.Model, prompt, task.bridge, task.run)
			} else {
				secret, err = s.copilotToken(ctx)
				if err == nil {
					err = reviewagent.RunCopilot(ctx, binary, directory, secret, request.Model, prompt, task.bridge, task.run)
				}
			}
		}
	}
	if ctx.Err() != nil {
		task.run.Finish("cancelled", "", "AI task stopped or reached its 30-minute limit. Existing proposals and worktree edits were kept.")
		return
	}
	if err != nil {
		message := sanitizeAIError(err.Error())
		for _, value := range []string{secret, task.bridge.Token} {
			if value != "" {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
		task.run.Finish("failed", "", message)
		return
	}
	if !task.run.HasContext() || task.action.Proposal() == nil {
		task.run.Finish("failed", "", "The provider completed without a typed task result. Existing worktree edits were kept; retry the task.")
		return
	}
	task.run.Finish("completed", "", "")
}

// Removing an owned worktree uses the normal clean-only Git removal. Its branch
// remains available in history; active tasks must be stopped explicitly first.
func (s *AIService) RemoveAgentWorktree(ctx context.Context, id string) error {
	if s.repositories == nil {
		return errors.New("Open a local repository first")
	}
	s.mu.Lock()
	if err := s.loadWorktreesLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	record, ok := s.worktrees[id]
	for _, task := range s.tasks {
		if task.worktreeID == id && task.action != nil {
			if _, active := s.active[task.run.Snapshot().Key]; active {
				s.mu.Unlock()
				return errors.New("Stop this agent task and wait for it to finish before removing its worktree")
			}
		}
	}
	if _, active := s.active["agent:"+id]; active {
		s.mu.Unlock()
		return errors.New("Stop this agent task before removing its worktree")
	}
	if !ok {
		s.mu.Unlock()
		return errors.New("Agent worktree not found")
	}
	// Reserve this worktree while Git runs, without blocking provider status,
	// cancellation, or unrelated tasks behind the global service mutex.
	key := "agent:" + id
	s.active[key] = "removing"
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.active, key); s.mu.Unlock() }()
	// An external Git client may already have removed the directory. Forget only
	// our private record; leave any Git branch/registration for ordinary recovery.
	if _, err := os.Lstat(record.Path); errors.Is(err, os.ErrNotExist) {
		return s.forgetAgentWorktree(record)
	}
	items, err := s.repositories.client.Worktrees(ctx, record.RepositoryRoot)
	if err != nil {
		return err
	}
	var selected *gitclient.Worktree
	for i := range items {
		if sameAIPath(items[i].Path, record.Path) {
			selected = &items[i]
			break
		}
	}
	if selected == nil {
		return errors.New("The agent worktree moved or was already removed. Inspect it in repository worktree tools")
	}
	if err = s.repositories.client.MutateWorktree(ctx, record.RepositoryRoot, gitclient.WorktreeRequest{Action: "remove", Path: selected.Path, Token: selected.Token}); err != nil {
		return err
	}
	return s.forgetAgentWorktree(record)
}

func (s *AIService) forgetAgentWorktree(record aiWorktreeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.worktrees, record.ID)
	if err := s.saveWorktreesLocked(); err != nil {
		s.worktrees[record.ID] = record
		return err
	}
	return nil
}

func sameAIPath(first, second string) bool {
	a, err := os.Stat(first)
	if err != nil {
		return false
	}
	b, err := os.Stat(second)
	return err == nil && os.SameFile(a, b)
}

func sameAIRepository(first, second string) bool {
	return filepath.Clean(first) == filepath.Clean(second) || sameAIPath(first, second)
}

// Keep active sessions and the most recently completed results. Generated source
// and unaccepted drafts live only in memory, bounded by this registry.
func (s *AIService) pruneAITasksLocked(keep string) {
	type oldTask struct{ id, created string }
	var finished []oldTask
	for id, task := range s.tasks {
		run := task.run.Snapshot()
		if id != keep && run.State != "running" && run.State != "preparing" {
			finished = append(finished, oldTask{id, run.CreatedAt})
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].created < finished[j].created })
	for _, old := range finished {
		if len(s.tasks) <= 40 {
			break
		}
		delete(s.tasks, old.id)
	}
}
