package reviewagent

import (
	"encoding/json"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

func ptr[T any](value T) *T { return &value }
func TestCodexCumulativeUsagePreservesUnknownCostsAndSnapshotIsolation(t *testing.T) {
	run := NewRun("codex", "", reviewFixture(), nil)
	event := json.RawMessage(`{"threadId":"thread-1","tokenUsage":{"total":{"inputTokens":1000,"cachedInputTokens":400,"outputTokens":200,"reasoningOutputTokens":50,"totalTokens":1200},"last":{"totalTokens":800},"modelContextWindow":128000}}`)
	run.recordCodexUsage(event, "other-thread")
	if run.Snapshot().Usage.TotalTokens != nil {
		t.Fatal("another thread's usage was accepted")
	}
	for i := 0; i < 3; i++ {
		run.recordCodexUsage(event, "thread-1")
	}
	value := run.Snapshot()
	if value.Usage.TotalTokens == nil || *value.Usage.TotalTokens != 1200 || *value.Usage.CachedInputTokens != 400 || *value.Usage.ContextTokens != 800 {
		t.Fatal(value.Usage)
	}
	if value.Usage.PremiumRequests != nil || value.Usage.NanoAIU != nil || value.Usage.ModelCalls != nil {
		t.Fatal("unknown costs were fabricated", value.Usage)
	}
	*value.Usage.TotalTokens = 999
	if *run.Snapshot().Usage.TotalTokens != 1200 {
		t.Fatal("snapshot exposed mutable usage")
	}
	run.Finish("completed", "done", "")
	run.recordCodexUsage(event, "thread-1")
	if *run.Snapshot().Usage.TotalTokens != 1200 {
		t.Fatal("terminal usage was lost")
	}
}
func TestCopilotUsageDeduplicatesCallsAndReconcilesFinalTotals(t *testing.T) {
	run := NewRun("copilot", "", reviewFixture(), nil)
	call := &copilot.AssistantUsageData{APICallID: ptr("call-1"), InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(25)), CacheReadTokens: ptr(int64(50)), Cost: ptr(0.0), Duration: ptr(int64(300)), CopilotUsage: &copilot.AssistantUsageCopilotUsage{TotalNanoAiu: 10}}
	for _, id := range []string{"event-a", "event-b"} {
		run.recordCopilotEvent(copilot.SessionEvent{ID: id, Data: call})
	}
	u := run.Snapshot().Usage
	if u.ModelCalls == nil || *u.ModelCalls != 1 || *u.InputTokens != 100 || *u.PremiumRequests != 0 || *u.NanoAIU != 10 || !u.Partial {
		t.Fatal(u)
	}
	run.recordCopilotEvent(copilot.SessionEvent{ID: "event-c", Data: &copilot.AssistantUsageData{APICallID: ptr("call-2"), InputTokens: ptr(int64(150)), OutputTokens: ptr(int64(50)), Cost: ptr(0.5)}})
	run.recordCopilotEvent(copilot.SessionEvent{ID: "context", Data: &copilot.SessionUsageInfoData{CurrentTokens: 800, TokenLimit: 128000}})
	metrics := &rpc.UsageGetMetricsResult{TotalPremiumRequestCost: 0.5, TotalNanoAiu: ptr(12.0), AiCreditsStatus: ptr(rpc.AiCreditsStatusPartial), TotalAPIDurationMs: 500, ModelMetrics: map[string]rpc.UsageMetricsModelMetric{"model": {Requests: rpc.UsageMetricsModelMetricRequests{Count: 2}, Usage: rpc.UsageMetricsModelMetricUsage{InputTokens: 250, OutputTokens: 75, CacheReadTokens: 50}}}}
	run.recordCopilotMetrics(metrics)
	run.recordCopilotEvent(copilot.SessionEvent{ID: "late", Data: &copilot.AssistantUsageData{InputTokens: ptr(int64(9999)), Cost: ptr(999.0)}})
	u = run.Snapshot().Usage
	if u.Partial || u.Source != "copilot-session" || *u.ModelCalls != 2 || *u.InputTokens != 250 || *u.OutputTokens != 75 || *u.PremiumRequests != 0.5 || *u.NanoAIU != 12 || u.BillingStatus != "partial" || *u.ContextTokens != 800 {
		t.Fatal(u)
	}
	*metrics.TotalNanoAiu = 999
	if *run.Snapshot().Usage.NanoAIU != 12 {
		t.Fatal("retained mutable provider metrics")
	}
}
func TestCopilotMissingBillingIsNotAFreeReview(t *testing.T) {
	run := NewRun("copilot", "", reviewFixture(), nil)
	run.recordCopilotEvent(copilot.SessionEvent{ID: "unknown", Data: &copilot.AssistantUsageData{Model: "model"}})
	u := run.Snapshot().Usage
	if u.PremiumRequests != nil || u.NanoAIU != nil || u.InputTokens != nil || u.OutputTokens != nil {
		t.Fatal("missing fields treated as zero", u)
	}
	run.recordCopilotMetrics(&rpc.UsageGetMetricsResult{})
	if run.Snapshot().Usage.Source != "copilot-events" {
		t.Fatal("empty metrics replaced observed usage")
	}
	run.recordCopilotMetrics(&rpc.UsageGetMetricsResult{AiCreditsStatus: ptr(rpc.AiCreditsStatusUnavailable), TotalNanoAiu: ptr(0.0), ModelMetrics: map[string]rpc.UsageMetricsModelMetric{"model": {Requests: rpc.UsageMetricsModelMetricRequests{Count: 1}}}})
	if run.Snapshot().Usage.NanoAIU != nil {
		t.Fatal("unavailable zero amount shown as free usage")
	}
}
