// Package skills contains the A2A interactional skill executors for the
// Eldamo agent. Skills are pure functions of their inputs via the Deps struct —
// no package-level globals are accessed.
//
// Layout:
//
//	skills.go        — Deps, LexiconSearcher interface, shared utilities
//	name_generate.go — deterministic Elvish name-generation skill
//	translate.go     — LLM-backed Tolkien translation skill
//	neologism.go     — LLM-backed two-path neologism-builder skill
package skills

import (
	"context"
	"iter"
	"strings"

	"github.com/ghchinoy/eldamoapi/index"
)

// ── LexiconSearcher ───────────────────────────────────────────────────────────

// LexiconSearcher is the minimal interface skills require from the in-memory
// lexicon. *index.Index satisfies it; tests can inject a lightweight stub.
type LexiconSearcher interface {
	SearchKeyword(query, lang, speech, category string) []*index.FlatWord
	GetRootAnchors(id string) []*index.FlatWord
}

// ── LLMClient ─────────────────────────────────────────────────────────────────

// LLMClient is the minimal interface skill executors need from an LLM
// backend: a streaming text completion given a system instruction and a user
// prompt. Skills never depend on a concrete SDK (e.g. google.golang.org/genai)
// directly — package main adapts each concrete backend (Vertex AI Gemini
// today; a local OpenAI-compatible client — llama.cpp / mlx_lm.server — as a
// fast-follow) to satisfy this interface and injects it via Deps.LLM.
type LLMClient interface {
	// GenerateContentStream streams a text completion for prompt, using
	// systemInstruction as the system/style guidance and model as the
	// backend-specific model identifier. The returned iterator yields
	// incremental GenChunk values; iteration ends after the first non-nil
	// error or once generation completes. A chunk carrying non-nil Usage may
	// be yielded as the final value when the backend reports token counts.
	GenerateContentStream(ctx context.Context, model, systemInstruction, prompt string) iter.Seq2[GenChunk, error]
}

// GenChunk is one incremental piece of a streamed LLM response.
type GenChunk struct {
	// Text is the incremental text produced by this chunk. May be empty on
	// a usage-only terminal chunk.
	Text string

	// Usage is non-nil only when the backend reports token accounting for
	// the completed call (typically on the final chunk).
	Usage *Usage
}

// Usage records token accounting for a single LLM call. Backend and Model
// let callers (see Phase 2 usage/cost tracking) break down usage by
// provider/runtime, e.g. to compare Vertex Gemini vs. local llama.cpp/MLX
// Gemma inference.
type Usage struct {
	// Backend identifies the provider/runtime, e.g. "vertex-gemini",
	// "llama.cpp", "mlx".
	Backend string

	// Model is the backend-specific model identifier used for the call.
	Model string

	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ── Deps ──────────────────────────────────────────────────────────────────────

// Deps holds every external dependency injected into skill executors.
// Construct once per server startup in newA2AHandler and pass to the executor.
type Deps struct {
	// Index is always non-nil; provided by the server's in-memory lexicon.
	Index LexiconSearcher

	// LLM is the shared LLM backend client. nil when no backend is
	// configured — translate and neologism skills self-disable when this is
	// nil. See package main's buildDeps for how concrete backends
	// (Vertex AI Gemini, and eventually llama.cpp/mlx_lm.server) are adapted
	// to this interface.
	LLM LLMClient

	// ModelName is the model identifier to pass to LLM.GenerateContentStream
	// (e.g. "gemini-3.1-flash-lite", or a local GGUF/MLX model name).
	ModelName string

	// TranslateMD is the embedded tolkien-translation/SKILL.md content,
	// used as the LLM system instruction for the translate skill.
	// Embedded in package main (//go:embed cannot cross directory boundaries).
	TranslateMD string

	// NeologismMD is the embedded neologism-builder/SKILL.md content,
	// used as the LLM system instruction for the neologism skill.
	NeologismMD string
}

// LLMEnabled reports whether an LLM backend is available.
// Translate and neologism skills check this before attempting LLM calls.
func (d *Deps) LLMEnabled() bool {
	return d.LLM != nil && d.ModelName != ""
}

// ── Shared vocabulary ─────────────────────────────────────────────────────────

// TargetLanguageKeywords maps user-typed language words to Eldamo language codes.
// Shared by name-generate, translate, and neologism parsers.
var TargetLanguageKeywords = map[string]string{
	"quenya":   "q",
	"sindarin": "s",
	"elvish":   "q",
}

// ── Shared utilities ──────────────────────────────────────────────────────────

// ConceptsFromText tokenizes source text into up to 5 candidate concept words,
// filtering English stop words and tokens shorter than 3 characters.
// Shared by translate (lexicon pre-fetch) and neologism (anchorage protocol).
func ConceptsFromText(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"of": true, "in": true, "on": true, "at": true, "to": true,
		"is": true, "are": true, "was": true, "my": true, "our": true,
		"your": true, "his": true, "her": true, "its": true, "we": true,
		"i": true, "you": true, "he": true, "she": true, "they": true,
		"this": true, "that": true, "with": true, "for": true, "from": true,
		"not": true, "no": true, "but": true, "it": true, "be": true,
		"have": true, "do": true, "will": true, "would": true, "could": true,
		"shall": true, "may": true, "might": true, "should": true,
	}
	words := strings.Fields(strings.ToLower(text))
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		w = strings.Trim(w, `"'.,!?;:-()[]`)
		if len(w) < 3 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) >= 5 {
			break
		}
	}
	return out
}


