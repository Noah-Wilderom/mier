package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mag-ik-een-review/internal/desktopruntime"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type AIWorktreeCheckApproval struct {
	ID          string         `json:"id"`
	Worktree    AIWorktreeInfo `json:"worktree"`
	Command     CustomCommand  `json:"command"`
	SourceStamp string         `json:"sourceStamp"`
	ExpiresAt   time.Time      `json:"expiresAt"`
}
type AIWorktreeCheck struct {
	ID          string        `json:"id"`
	WorktreeID  string        `json:"worktreeId"`
	ActionID    string        `json:"actionId"`
	Command     CustomCommand `json:"command"`
	Run         CommandRun    `json:"run"`
	SourceStamp string        `json:"sourceStamp"`
	AfterStamp  string        `json:"afterStamp"`
	Stale       bool          `json:"stale"`
	Error       string        `json:"error"`
}

func (s *AIService) checkWorktree(id string) (aiWorktreeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return aiWorktreeRecord{}, errors.New("M.I.E.R. is shutting down")
	}
	if err := s.loadWorktreesLocked(); err != nil {
		return aiWorktreeRecord{}, err
	}
	record, ok := s.worktrees[id]
	if !ok {
		return record, errors.New("Choose an owned agent worktree")
	}
	if _, busy := s.active["agent:"+id]; busy {
		return record, errors.New("Stop the agent and wait for it to finish before preparing a check")
	}
	for _, task := range s.tasks {
		if task.worktreeID == id && s.active[task.run.Snapshot().Key] != "" {
			return record, errors.New("Wait for the agent to finish before checking its edits")
		}
	}
	return record, nil
}
func (s *AIService) PrepareWorktreeCheck(ctx context.Context, id string, command CustomCommand) (AIWorktreeCheckApproval, error) {
	var approval AIWorktreeCheckApproval
	if s.commands == nil || s.repositories == nil {
		return approval, errors.New("The command runner is unavailable")
	}
	if err := validateCustomCommand(command); err != nil {
		return approval, err
	}
	record, err := s.checkWorktree(id)
	if err != nil {
		return approval, err
	}
	// Resolve before presenting approval. The exact executable/arguments shown by
	// the desktop client are immutable for this single-use approval.
	if command.Executable == "node" || command.Executable == "npm" {
		node, err := desktopruntime.FindNode(ctx, "", command.Executable == "npm")
		if err != nil {
			return approval, err
		}
		if command.Executable == "npm" {
			command.Arguments = append([]string{node.NPMCLI}, command.Arguments...)
		}
		command.Executable = node.Executable
	} else {
		binary, err := exec.LookPath(command.Executable)
		if err != nil {
			return approval, fmt.Errorf("Find check executable: %w. Use its full installed path", err)
		}
		command.Executable, err = filepath.Abs(binary)
		if err != nil {
			return approval, err
		}
	}
	stamp, err := s.repositories.client.AgentWorktreeStamp(ctx, record.guard())
	if err != nil {
		return approval, err
	}
	approval = AIWorktreeCheckApproval{ID: reviewagent.ID(), Worktree: record.AIWorktreeInfo, Command: command, SourceStamp: stamp, ExpiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkApprovals == nil {
		s.checkApprovals = map[string]AIWorktreeCheckApproval{}
	}
	for key, old := range s.checkApprovals {
		if time.Now().After(old.ExpiresAt) {
			delete(s.checkApprovals, key)
		}
	}
	if len(s.checkApprovals) >= 20 {
		return AIWorktreeCheckApproval{}, errors.New("Too many pending command approvals; wait for older approvals to expire")
	}
	s.checkApprovals[approval.ID] = approval
	return approval, nil
}
func (s *AIService) StartWorktreeCheck(ctx context.Context, approvalID string) (AIWorktreeCheck, error) {
	s.mu.Lock()
	approval, ok := s.checkApprovals[approvalID]
	delete(s.checkApprovals, approvalID)
	s.mu.Unlock()
	if !ok || time.Now().After(approval.ExpiresAt) {
		return AIWorktreeCheck{}, errors.New("Check approval expired. Review and approve the command again")
	}
	record, err := s.checkWorktree(approval.Worktree.ID)
	if err != nil {
		return AIWorktreeCheck{}, err
	}
	s.mu.Lock()
	key := "agent:" + record.ID
	if s.closed || s.active[key] != "" {
		s.mu.Unlock()
		return AIWorktreeCheck{}, errors.New("The agent worktree is busy")
	}
	s.active[key] = "verification"
	s.mu.Unlock()
	started := false
	defer func() {
		if !started {
			s.mu.Lock()
			delete(s.active, key)
			s.mu.Unlock()
		}
	}()
	guard := record.guard()
	run, err := s.commands.startDefinition(ctx, record.Path, approval.Command, guard.HeadSHA, guard.Branch, &guard, approval.SourceStamp)
	if err != nil {
		return AIWorktreeCheck{}, err
	}
	check := AIWorktreeCheck{ID: run.ID, WorktreeID: record.ID, ActionID: record.LastActionID, Command: approval.Command, Run: run, SourceStamp: approval.SourceStamp}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = s.commands.Cancel(run.ID)
		return AIWorktreeCheck{}, errors.New("M.I.E.R. is shutting down")
	}
	if s.worktreeChecks == nil {
		s.worktreeChecks = map[string]AIWorktreeCheck{}
	}
	s.worktreeChecks[check.ID] = check
	s.wg.Add(1)
	s.mu.Unlock()
	if err = s.persistWorktreeCheck(check); err != nil {
		check.Error = "Check history could not be saved: " + err.Error()
	}
	started = true
	go func(check AIWorktreeCheck) {
		defer s.wg.Done()
		defer func() { s.mu.Lock(); delete(s.active, key); s.mu.Unlock() }()
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			current, err := s.commands.Run(run.ID)
			if err != nil {
				check.Error = err.Error()
				check.Run.Status = "failed"
				break
			}
			check.Run = current
			if current.Status != "running" {
				break
			}
		}
		after, err := s.repositories.client.AgentWorktreeStamp(context.Background(), guard)
		check.AfterStamp = after
		check.Stale = err != nil || after != check.SourceStamp
		if err != nil {
			check.Error = err.Error()
		}
		if err = s.persistWorktreeCheck(check); err != nil {
			check.Error = "Check history could not be saved: " + err.Error()
		}
		s.mu.Lock()
		s.worktreeChecks[check.ID] = check
		s.mu.Unlock()
	}(check)
	return check, nil
}
func (s *AIService) persistWorktreeCheck(check AIWorktreeCheck) error {
	directory := filepath.Join(s.historyDirectory(), "checks")
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	if err := writePrivateJSON(filepath.Join(directory, check.ID+".json"), check); err != nil {
		return err
	}
	entries, _ := os.ReadDir(directory)
	type record struct {
		name string
		at   time.Time
	}
	var records []record
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && !entry.IsDir() {
			records = append(records, record{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].at.Before(records[j].at) })
	for len(records) > 40 {
		_ = os.Remove(filepath.Join(directory, records[0].name))
		records = records[1:]
	}
	return nil
}
func (s *AIService) WorktreeChecks(ctx context.Context, id string) ([]AIWorktreeCheck, error) {
	s.mu.Lock()
	if err := s.loadWorktreesLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	record, ok := s.worktrees[id]
	active := s.active["agent:"+id] != ""
	live := map[string]AIWorktreeCheck{}
	for key, check := range s.worktreeChecks {
		if check.WorktreeID == id {
			live[key] = check
		}
	}
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("Choose an owned agent worktree")
	}
	entries, err := os.ReadDir(filepath.Join(s.historyDirectory(), "checks"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	all := map[string]AIWorktreeCheck{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !aiHistoryID.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		file, openErr := os.Open(filepath.Join(s.historyDirectory(), "checks", entry.Name()))
		if openErr != nil {
			return nil, openErr
		}
		data, err := io.ReadAll(io.LimitReader(file, 8*1024*1024+1))
		file.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > 8*1024*1024 {
			return nil, errors.New("Saved verification output exceeds its limit")
		}
		var check AIWorktreeCheck
		if json.Unmarshal(data, &check) != nil {
			return nil, errors.New("Saved verification output is unreadable")
		}
		if check.WorktreeID == id {
			if check.Run.Status == "running" {
				check.Run.Status = "canceled"
				check.Error = "Application closed while this check was running"
				check.Stale = true
			}
			all[check.ID] = check
		}
	}
	for key, check := range live {
		if check.Run.Status == "running" {
			if run, err := s.commands.Run(check.Run.ID); err == nil {
				check.Run = run
				if run.Status != "running" && check.AfterStamp == "" {
					check.Run.Status = "running"
				}
			}
		}
		all[key] = check
	}
	var currentStamp string
	if len(all) > 0 && !active {
		currentStamp, err = s.repositories.client.AgentWorktreeStamp(ctx, record.guard())
	}
	result := []AIWorktreeCheck{}
	for _, check := range all {
		check.Stale = check.Stale || (!active && (err != nil || check.SourceStamp != currentStamp)) || check.AfterStamp != "" && check.AfterStamp != check.SourceStamp
		result = append(result, check)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Run.StartedAt > result[j].Run.StartedAt })
	return result, nil
}
func (s *AIService) CancelWorktreeCheck(id string) error {
	s.mu.Lock()
	check, ok := s.worktreeChecks[id]
	s.mu.Unlock()
	if !ok || s.commands == nil {
		return errors.New("This worktree check is no longer running")
	}
	return s.commands.Cancel(check.Run.ID)
}
