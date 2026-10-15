package reviewagent

import (
	"encoding/json"
	"math"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// Nil means not reported, including for costs. Subscription usage is not a
// dollar invoice; premium request multipliers and nano-AI units stay distinct.
type Usage struct {
	Source            string   `json:"source,omitempty"`
	Partial           bool     `json:"partial"`
	InputTokens       *int64   `json:"inputTokens,omitempty"`
	OutputTokens      *int64   `json:"outputTokens,omitempty"`
	CachedInputTokens *int64   `json:"cachedInputTokens,omitempty"`
	CacheWriteTokens  *int64   `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens   *int64   `json:"reasoningTokens,omitempty"`
	TotalTokens       *int64   `json:"totalTokens,omitempty"`
	ContextTokens     *int64   `json:"contextTokens,omitempty"`
	ContextWindow     *int64   `json:"contextWindow,omitempty"`
	ModelCalls        *int64   `json:"modelCalls,omitempty"`
	APIDurationMs     *int64   `json:"apiDurationMs,omitempty"`
	PremiumRequests   *float64 `json:"premiumRequests,omitempty"`
	NanoAIU           *float64 `json:"nanoAiu,omitempty"`
	BillingStatus     string   `json:"billingStatus,omitempty"`
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneUsage(value Usage) Usage {
	value.InputTokens = clonePtr(value.InputTokens)
	value.OutputTokens = clonePtr(value.OutputTokens)
	value.CachedInputTokens = clonePtr(value.CachedInputTokens)
	value.CacheWriteTokens = clonePtr(value.CacheWriteTokens)
	value.ReasoningTokens = clonePtr(value.ReasoningTokens)
	value.TotalTokens = clonePtr(value.TotalTokens)
	value.ContextTokens = clonePtr(value.ContextTokens)
	value.ContextWindow = clonePtr(value.ContextWindow)
	value.ModelCalls = clonePtr(value.ModelCalls)
	value.APIDurationMs = clonePtr(value.APIDurationMs)
	value.PremiumRequests = clonePtr(value.PremiumRequests)
	value.NanoAIU = clonePtr(value.NanoAIU)
	return value
}
func nonnegative(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	return clonePtr(value)
}
func nonnegativeFloat(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return clonePtr(value)
}
func addInt(total **int64, value *int64) {
	if value == nil || *value < 0 {
		return
	}
	if *total == nil {
		*total = new(int64)
	}
	if **total <= math.MaxInt64-*value {
		**total += *value
	}
}
func addFloat(total **float64, value *float64) {
	if nonnegativeFloat(value) == nil {
		return
	}
	if *total == nil {
		*total = new(float64)
	}
	if next := **total + *value; !math.IsInf(next, 0) {
		**total = next
	}
}

func (r *Run) recordCodexUsage(params json.RawMessage, threadID string) {
	var event struct {
		ThreadID   string `json:"threadId"`
		TokenUsage struct {
			Total struct {
				Input      *int64 `json:"inputTokens"`
				Output     *int64 `json:"outputTokens"`
				Cached     *int64 `json:"cachedInputTokens"`
				CacheWrite *int64 `json:"cacheWriteInputTokens"`
				Reasoning  *int64 `json:"reasoningOutputTokens"`
				Total      *int64 `json:"totalTokens"`
			} `json:"total"`
			Last struct {
				Total *int64 `json:"totalTokens"`
			} `json:"last"`
			Window *int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &event) != nil || threadID == "" || event.ThreadID != threadID || nonnegative(event.TokenUsage.Total.Total) == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u := event.TokenUsage
	if r.snapshot.Usage.TotalTokens != nil && *u.Total.Total < *r.snapshot.Usage.TotalTokens {
		return
	}
	// These are cumulative thread totals. Repeated notifications replace them;
	// cache and reasoning are subsets, never added to the reported total.
	r.snapshot.Usage = Usage{Source: "codex", InputTokens: nonnegative(u.Total.Input), OutputTokens: nonnegative(u.Total.Output), CachedInputTokens: nonnegative(u.Total.Cached), CacheWriteTokens: nonnegative(u.Total.CacheWrite), ReasoningTokens: nonnegative(u.Total.Reasoning), TotalTokens: nonnegative(u.Total.Total), ContextTokens: nonnegative(u.Last.Total), ContextWindow: nonnegative(u.Window)}
	r.snapshot.Version++
}

func (r *Run) recordCopilotEvent(event copilot.SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := &r.snapshot.Usage
	switch data := event.Data.(type) {
	case *copilot.AssistantUsageData:
		if u.Source == "copilot-session" {
			return
		}
		id := event.ID
		if data.APICallID != nil && *data.APICallID != "" {
			id = *data.APICallID
		}
		if id == "" || len(id) > 256 || len(r.usageEvents) >= 4096 {
			return
		}
		if r.usageEvents == nil {
			r.usageEvents = map[string]bool{}
		}
		if r.usageEvents[id] {
			return
		}
		r.usageEvents[id] = true
		u.Source, u.Partial = "copilot-events", true
		one := int64(1)
		addInt(&u.ModelCalls, &one)
		addInt(&u.InputTokens, data.InputTokens)
		addInt(&u.OutputTokens, data.OutputTokens)
		addInt(&u.CachedInputTokens, data.CacheReadTokens)
		addInt(&u.CacheWriteTokens, data.CacheWriteTokens)
		addInt(&u.ReasoningTokens, data.ReasoningTokens)
		addInt(&u.APIDurationMs, data.Duration)
		addFloat(&u.PremiumRequests, data.Cost)
		if data.CopilotUsage != nil && (data.AiCreditsStatus == nil || string(*data.AiCreditsStatus) != "unavailable") {
			addFloat(&u.NanoAIU, &data.CopilotUsage.TotalNanoAiu)
		}
		// Event costs are observed subtotals until authoritative session metrics
		// arrive. A later complete call cannot make earlier missing calls whole.
		if u.NanoAIU != nil {
			u.BillingStatus = "partial"
		}
	case *copilot.SessionUsageInfoData:
		u.ContextTokens, u.ContextWindow = nonnegative(&data.CurrentTokens), nonnegative(&data.TokenLimit)
	default:
		return
	}
	r.snapshot.Version++
}
func billingStatus(value string) string {
	switch value {
	case "complete", "partial", "unavailable":
		return value
	}
	return ""
}
func (r *Run) recordCopilotMetrics(metrics *rpc.UsageGetMetricsResult) {
	if metrics == nil || len(metrics.ModelMetrics) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u := Usage{Source: "copilot-session", ContextTokens: clonePtr(r.snapshot.Usage.ContextTokens), ContextWindow: clonePtr(r.snapshot.Usage.ContextWindow), PremiumRequests: nonnegativeFloat(&metrics.TotalPremiumRequestCost), APIDurationMs: nonnegative(&metrics.TotalAPIDurationMs)}
	if metrics.AiCreditsStatus != nil {
		u.BillingStatus = billingStatus(string(*metrics.AiCreditsStatus))
	}
	if u.BillingStatus != "unavailable" {
		u.NanoAIU = nonnegativeFloat(metrics.TotalNanoAiu)
	}
	for _, metric := range metrics.ModelMetrics {
		addInt(&u.InputTokens, &metric.Usage.InputTokens)
		addInt(&u.OutputTokens, &metric.Usage.OutputTokens)
		addInt(&u.CachedInputTokens, &metric.Usage.CacheReadTokens)
		addInt(&u.CacheWriteTokens, &metric.Usage.CacheWriteTokens)
		addInt(&u.ReasoningTokens, metric.Usage.ReasoningTokens)
		addInt(&u.ModelCalls, &metric.Requests.Count)
	}
	// Final session metrics replace event subtotals, rather than adding twice.
	r.snapshot.Usage = u
	r.snapshot.Version++
}
