package main

import (
	"encoding/json"
	"errors"
	"io"
	"mag-ik-een-review/internal/reviewagent"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var aiHistoryID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type aiHistoryRecord struct {
	Review        *AIReviewRequest        `json:"review,omitempty"`
	ActionRequest *AIActionRequest        `json:"actionRequest,omitempty"`
	Run           reviewagent.RunSnapshot `json:"run"`
	Action        *AIActionSnapshot       `json:"action,omitempty"`
}
type AIReviewHistoryRequest struct {
	Host        string `json:"host"`
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
	PullRequest int    `json:"pullRequest"`
}

func (s *AIService) historyDirectory() string {
	return filepath.Join(filepath.Dir(s.configPath), "ai-history")
}
func (s *AIService) readAIHistory(id string) (aiHistoryRecord, error) {
	var record aiHistoryRecord
	if !aiHistoryID.MatchString(id) {
		return record, errors.New("Invalid AI history identifier")
	}
	file, err := os.Open(filepath.Join(s.historyDirectory(), id+".json"))
	if err != nil {
		return record, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8*1024*1024+1))
	if err != nil {
		return record, err
	}
	if len(data) > 8*1024*1024 || json.Unmarshal(data, &record) != nil || record.Run.ID != id {
		return record, errors.New("Saved AI conversation is unreadable")
	}
	// Only snapshots are restored. No bridge, bearer, approval, agent process, or
	// write capability is recreated from disk.
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task != nil {
		record.Run = task.run.Snapshot()
	} else if record.Run.State == "preparing" || record.Run.State == "running" {
		record.Run.State = "cancelled"
		record.Run.Error = "The application closed before this task finished. Existing findings were retained; start a follow-up to continue."
		record.Run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if record.Action != nil {
		record.Action.Run = record.Run
	}
	return record, nil
}
func (s *AIService) persistAITask(task *aiTask) error {
	if task.reviewRequest == nil && task.actionRequest == nil {
		return nil
	}
	snapshot := task.run.Snapshot()
	record := aiHistoryRecord{Review: task.reviewRequest, ActionRequest: task.actionRequest, Run: snapshot}
	if task.action != nil {
		action := s.actionSnapshot(task)
		record.Action = &action
		record.Run = action.Run
	}
	return s.persistAIHistoryRecord(record)
}
func (s *AIService) persistAIHistoryRecord(record aiHistoryRecord) error {
	snapshot := record.Run
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	if s.historyVersions == nil {
		s.historyVersions = map[string]uint64{}
	}
	if s.historyVersions[snapshot.ID] >= snapshot.Version {
		return nil
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(encoded) >= 8*1024*1024 {
		return errors.New("AI history snapshot exceeds its 8 MiB limit")
	}
	if err := writePrivateJSON(filepath.Join(s.historyDirectory(), snapshot.ID+".json"), json.RawMessage(encoded)); err != nil {
		return err
	}
	s.historyVersions[snapshot.ID] = snapshot.Version
	entries, _ := os.ReadDir(s.historyDirectory())
	type old struct {
		path string
		at   time.Time
	}
	var records []old
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !aiHistoryID.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			records = append(records, old{filepath.Join(s.historyDirectory(), entry.Name()), info.ModTime()})
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].at.Before(records[j].at) })
	// 100 bounded snapshots, each containing at most 50 findings and bounded
	// activity/output. Active tasks are the newest entries and are not discarded.
	for len(records) > 100 {
		if records[0].path != filepath.Join(s.historyDirectory(), snapshot.ID+".json") {
			if os.Remove(records[0].path) == nil {
				delete(s.historyVersions, strings.TrimSuffix(filepath.Base(records[0].path), ".json"))
			}
		}
		records = records[1:]
	}
	return nil
}
func sameReviewHistory(request AIReviewHistoryRequest, previous AIReviewRequest) bool {
	return strings.EqualFold(request.Host, previous.Host) && request.Owner == previous.Owner && request.Repository == previous.Repository && request.PullRequest == previous.PullRequest
}
func (s *AIService) ReviewHistory(request AIReviewHistoryRequest) ([]reviewagent.RunSnapshot, error) {
	entries, err := os.ReadDir(s.historyDirectory())
	if os.IsNotExist(err) {
		return []reviewagent.RunSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []reviewagent.RunSnapshot{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !aiHistoryID.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		record, err := s.readAIHistory(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if record.Review != nil && sameReviewHistory(request, *record.Review) {
			result = append(result, record.Run)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt < result[j].CreatedAt })
	if len(result) > 20 {
		result = result[len(result)-20:]
	}
	return result, nil
}
func (s *AIService) previousReviewContext(request AIReviewRequest) ([]reviewagent.PreviousReview, error) {
	var reviews []reviewagent.PreviousReview
	id := request.PreviousReviewID
	total := 0
	for id != "" && len(reviews) < 6 {
		record, err := s.readAIHistory(id)
		if err != nil {
			return nil, errors.New("The previous conversation could not be restored. Start a new review instead")
		}
		if record.Review == nil || !sameReviewHistory(AIReviewHistoryRequest{request.Host, request.Owner, request.Repository, request.PullRequest}, *record.Review) {
			return nil, errors.New("A follow-up can only reference reviews of this pull request")
		}
		summary := record.Run.Summary
		if len(summary) > 8000 {
			summary = strings.ToValidUTF8(summary[:8000], "")
		}
		review := reviewagent.PreviousReview{ID: record.Run.ID, HeadSHA: record.Run.HeadSHA, Instructions: record.Run.Instructions, Summary: summary, TotalFindings: len(record.Run.Findings), Truncated: len(summary) < len(record.Run.Summary), Findings: []reviewagent.Finding{}}
		for _, finding := range record.Run.Findings {
			if len(finding.Body) > 2000 {
				finding.Body = strings.ToValidUTF8(finding.Body[:2000], "") + "…"
				review.Truncated = true
			}
			review.Findings = append(review.Findings, finding)
			encoded, _ := json.Marshal(review)
			if len(encoded) > 32000 {
				review.Findings = review.Findings[:len(review.Findings)-1]
				review.Truncated = true
				break
			}
		}
		data, _ := json.Marshal(review)
		if total+len(data) > 48000 {
			break
		}
		total += len(data)
		reviews = append(reviews, review)
		id = record.Review.PreviousReviewID
	}
	for i, j := 0, len(reviews)-1; i < j; i, j = i+1, j-1 {
		reviews[i], reviews[j] = reviews[j], reviews[i]
	}
	return reviews, nil
}
