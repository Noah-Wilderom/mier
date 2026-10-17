package lspclient

import (
	"context"
	"strings"
	"time"
)

type AnalysisStatus struct {
	Phase     string    `json:"phase"`
	StartedAt time.Time `json:"startedAt"`
	ElapsedMS int64     `json:"elapsedMs"`
	Cached    bool      `json:"cached"`
	Error     string    `json:"error"`
}

func analysisKey(r CodePositionRequest) string {
	return strings.Join([]string{r.Root, r.Revision, r.ComparisonHead, r.Path}, "\x00")
}
func (m *Manager) analysisPhase(r CodePositionRequest, phase string, cached bool) {
	m.warmupMu.Lock()
	defer m.warmupMu.Unlock()
	key := analysisKey(r)
	value := m.analysis[key]
	if value.StartedAt.IsZero() {
		value.StartedAt = time.Now()
	}
	value.Phase = phase
	value.Cached = value.Cached || cached
	value.ElapsedMS = time.Since(value.StartedAt).Milliseconds()
	if len(m.analysis) > 100 {
		m.analysis = map[string]AnalysisStatus{}
	}
	m.analysis[key] = value
}
func (m *Manager) AnalysisStatus(ctx context.Context, r CodePositionRequest) (AnalysisStatus, error) {
	root, err := canonicalRoot(r.Root)
	if err != nil {
		return AnalysisStatus{}, err
	}
	r.Root = root
	m.warmupMu.Lock()
	defer m.warmupMu.Unlock()
	value := m.analysis[analysisKey(r)]
	if !value.StartedAt.IsZero() && value.Phase != "Ready" && value.Phase != "Failed" {
		value.ElapsedMS = time.Since(value.StartedAt).Milliseconds()
	}
	return value, nil
}
