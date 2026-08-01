package skills

import (
	"fmt"
	"log"
	"time"
)

// ── Cost estimation ──────────────────────────────────────────────────────────

// ModelPricing holds approximate USD list price per 1,000,000 tokens for a
// specific hosted model.
type ModelPricing struct {
	PromptPerMillionUSD     float64
	CompletionPerMillionUSD float64
}

// knownPricing is a small, easily-extended table of approximate USD list
// pricing per 1M tokens for hosted models used by this server. Local
// backends (llama.cpp, mlx) are intentionally absent — they have no
// per-token USD cost, and EstimateCostUSD correctly returns 0 for any model
// not present here.
//
// IMPORTANT: these figures are approximate list prices and may drift from
// Google's published pricing over time (region, committed-use discounts,
// and price changes are not reflected). Verify against
// https://cloud.google.com/vertex-ai/generative-ai/pricing before relying
// on EstimateCostUSD for anything beyond directional backend/model
// comparison (see docs/model-evaluation.md) — this is not a billing
// reconciliation tool. Extend this table when a hosted Gemma 4 model
// (eldamo-server-3k0, Phase 5) is priced in.
var knownPricing = map[string]ModelPricing{
	"gemini-3.1-flash-lite": {PromptPerMillionUSD: 0.10, CompletionPerMillionUSD: 0.40},
}

// EstimateCostUSD returns the estimated USD cost of usage based on
// knownPricing, keyed by usage.Model. Returns 0 for a nil usage or for any
// model not present in knownPricing — the expected/correct result for
// local backends (llama.cpp, mlx), which have no per-token USD cost.
func EstimateCostUSD(usage *Usage) float64 {
	if usage == nil {
		return 0
	}
	pricing, ok := knownPricing[usage.Model]
	if !ok {
		return 0
	}
	promptCost := float64(usage.PromptTokens) / 1_000_000 * pricing.PromptPerMillionUSD
	completionCost := float64(usage.CompletionTokens) / 1_000_000 * pricing.CompletionPerMillionUSD
	return promptCost + completionCost
}

// ── Usage logging ────────────────────────────────────────────────────────────

// UsageLogLine formats the structured usage/cost/latency accounting line for
// a single LLM-backed skill invocation. Exported (rather than folded
// directly into LogUsage) so tests can assert on its content without
// intercepting the log package's writer.
//
// usage may be nil if the backend never reported usage before failing (e.g.
// a connection error before any response was received) — the line still
// reports skill/user/status/latency/error in that case, with backend/model
// shown as "?" and token counts/cost as 0.
func UsageLogLine(skill, user string, usage *Usage, latency time.Duration, callErr error) string {
	status := "ok"
	if callErr != nil {
		status = "error"
	}
	backend, model := "?", "?"
	var promptTokens, completionTokens, totalTokens int
	var cost float64
	if usage != nil {
		backend, model = usage.Backend, usage.Model
		promptTokens, completionTokens, totalTokens = usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens
		cost = EstimateCostUSD(usage)
	}
	line := fmt.Sprintf(
		"[Usage] skill=%s user=%q status=%s backend=%s model=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d latency=%s cost_usd=%.6f",
		skill, user, status, backend, model, promptTokens, completionTokens, totalTokens, latency, cost)
	if callErr != nil {
		line += fmt.Sprintf(" err=%v", callErr)
	}
	return line
}

// LogUsage emits one structured log line recording token/cost/latency
// accounting for a single LLM-backed skill invocation (translate or
// neologism), regardless of which backend served it — so Vertex Gemini vs.
// llama.cpp vs. MLX comparisons (see docs/model-evaluation.md) can be read
// directly from server logs without needing a separate metrics pipeline.
// Called on every invocation, success or failure.
func LogUsage(skill, user string, usage *Usage, latency time.Duration, callErr error) {
	log.Println(UsageLogLine(skill, user, usage, latency, callErr))
}
