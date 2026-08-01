package skills

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ── TestEstimateCostUSD ───────────────────────────────────────────────────────

func TestEstimateCostUSD(t *testing.T) {
	t.Run("nil usage returns 0", func(t *testing.T) {
		if got := EstimateCostUSD(nil); got != 0 {
			t.Errorf("EstimateCostUSD(nil) = %v, want 0", got)
		}
	})

	t.Run("unknown model returns 0 (local backends)", func(t *testing.T) {
		usage := &Usage{Backend: "llama.cpp", Model: "eldamo-gemma-q4_k_m", PromptTokens: 1000, CompletionTokens: 500}
		if got := EstimateCostUSD(usage); got != 0 {
			t.Errorf("EstimateCostUSD(local model) = %v, want 0", got)
		}
	})

	t.Run("known model computes cost from token counts", func(t *testing.T) {
		usage := &Usage{
			Backend:          "vertex-gemini",
			Model:            "gemini-3.1-flash-lite",
			PromptTokens:     1_000_000,
			CompletionTokens: 1_000_000,
		}
		pricing := knownPricing["gemini-3.1-flash-lite"]
		want := pricing.PromptPerMillionUSD + pricing.CompletionPerMillionUSD
		if got := EstimateCostUSD(usage); got != want {
			t.Errorf("EstimateCostUSD() = %v, want %v", got, want)
		}
	})

	t.Run("zero tokens costs zero even for a known model", func(t *testing.T) {
		usage := &Usage{Model: "gemini-3.1-flash-lite"}
		if got := EstimateCostUSD(usage); got != 0 {
			t.Errorf("EstimateCostUSD(zero tokens) = %v, want 0", got)
		}
	})
}

// ── TestUsageLogLine ──────────────────────────────────────────────────────────

func TestUsageLogLine(t *testing.T) {
	t.Run("success line includes backend, model, tokens, cost", func(t *testing.T) {
		usage := &Usage{
			Backend:          "llama.cpp",
			Model:            "eldamo-gemma-q4_k_m",
			PromptTokens:     120,
			CompletionTokens: 40,
			TotalTokens:      160,
		}
		line := UsageLogLine("translate", "dev-user", usage, 2*time.Second, nil)

		for _, want := range []string{
			"skill=translate", `user="dev-user"`, "status=ok",
			"backend=llama.cpp", "model=eldamo-gemma-q4_k_m",
			"prompt_tokens=120", "completion_tokens=40", "total_tokens=160",
			"cost_usd=0.000000",
		} {
			if !strings.Contains(line, want) {
				t.Errorf("log line missing %q: %s", want, line)
			}
		}
		if strings.Contains(line, "err=") {
			t.Errorf("success line should not include err=: %s", line)
		}
	})

	t.Run("error line includes status=error and err=", func(t *testing.T) {
		line := UsageLogLine("neologism", "dev-user", nil, time.Second, errors.New("boom"))
		for _, want := range []string{"status=error", "backend=?", "model=?", "err=boom"} {
			if !strings.Contains(line, want) {
				t.Errorf("log line missing %q: %s", want, line)
			}
		}
	})

	t.Run("known model reflects non-zero cost", func(t *testing.T) {
		usage := &Usage{Model: "gemini-3.1-flash-lite", PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
		line := UsageLogLine("translate", "dev-user", usage, time.Millisecond, nil)
		if strings.Contains(line, "cost_usd=0.000000") {
			t.Errorf("expected non-zero cost for a known hosted model: %s", line)
		}
	})
}
