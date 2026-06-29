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
	"strings"

	"github.com/ghchinoy/eldamoapi/index"
	"google.golang.org/genai"
)

// ── LexiconSearcher ───────────────────────────────────────────────────────────

// LexiconSearcher is the minimal interface skills require from the in-memory
// lexicon. *index.Index satisfies it; tests can inject a lightweight stub.
type LexiconSearcher interface {
	SearchKeyword(query, lang, speech, category string) []*index.FlatWord
	GetRootAnchors(id string) []*index.FlatWord
}

// ── Deps ──────────────────────────────────────────────────────────────────────

// Deps holds every external dependency injected into skill executors.
// Construct once per server startup in newA2AHandler and pass to the executor.
type Deps struct {
	// Index is always non-nil; provided by the server's in-memory lexicon.
	Index LexiconSearcher

	// GenAI is the shared Vertex AI client. nil when GEMINI_TRANSLATE_MODEL is
	// unset — translate and neologism skills self-disable when this is nil.
	GenAI *genai.Client

	// ModelName is the Gemini model to use (e.g. "gemini-3.1-flash-lite").
	ModelName string

	// TranslateMD is the embedded tolkien-translation/SKILL.md content,
	// used as the Gemini system instruction for the translate skill.
	// Embedded in package main (//go:embed cannot cross directory boundaries).
	TranslateMD string

	// NeologismMD is the embedded neologism-builder/SKILL.md content,
	// used as the Gemini system instruction for the neologism skill.
	NeologismMD string
}

// LLMEnabled reports whether the GenAI client is available.
// Translate and neologism skills check this before attempting Gemini calls.
func (d *Deps) LLMEnabled() bool {
	return d.GenAI != nil && d.ModelName != ""
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


