package main

// skill_name_generate.go — Phase 3 deterministic (keyword-driven) name-generation skill.
//
// Input format (from the user's message text):
//
//	name <concept1> [concept2] [quenya|sindarin] [masculine|feminine]
//
// Examples:
//
//	"name star silver quenya"
//	"name grey flame sindarin"
//	"name ocean wisdom feminine sindarin"
//
// The executor searches the in-memory lexicon for each concept word,
// selects the best root candidate, applies Elvish compounding rules
// (vowel elision, basic consonant assimilation) and appends a suffix.
// Intermediate steps are streamed as TaskStateWorking status events.

import (
	"context"
	"fmt"
	"iter"
	"log"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/ghchinoy/eldamoapi/index"
)

// ── Suffix tables (from SKILL.md) ────────────────────────────────────────────

var quenyaSuffixes = map[string]string{
	"masculine": "ndil",  // -(n)dil: friend/lover of — Elendil, Valandil
	"feminine":  "wen",   // -wen: maiden — Eärwen
}

var sindarinSuffixes = map[string]string{
	"masculine": "on",   // -on: great/noble masculine — Turgon
	"feminine":  "iel",  // -iel: daughter/maiden — Galadriel
}

// ── Language / gender keywords ────────────────────────────────────────────────

var languageKeywords = map[string]string{
	"quenya":   "q",
	"sindarin": "s",
	"elvish":   "q", // default to Quenya when ambiguous
}

var genderKeywords = map[string]string{
	"masculine": "masculine",
	"male":      "masculine",
	"man":       "masculine",
	"father":    "masculine",
	"son":       "masculine",
	"feminine":  "feminine",
	"female":    "feminine",
	"woman":     "feminine",
	"maiden":    "feminine",
	"daughter":  "feminine",
	"mother":    "feminine",
}

// stopConcepts are words that should not be treated as concept lookups.
var stopConcepts = map[string]bool{
	"name": true, "generate": true, "make": true, "create": true,
	"for":  true, "a": true, "an": true, "the": true, "of": true,
}

// ── nameRequest holds the parsed skill parameters ─────────────────────────────

type nameRequest struct {
	lang     string   // "q" or "s"
	langName string   // "Quenya" or "Sindarin"
	gender   string   // "masculine" or "feminine"
	concepts []string // concept keywords to look up (max 3)
}

// parseNameRequest extracts language, gender, and concept keywords from the
// user's raw message text. Trigger word "name" (and leading whitespace) is
// stripped before parsing.
func parseNameRequest(msg *a2a.Message) nameRequest {
	req := nameRequest{
		lang:   "q",
		gender: "masculine",
	}

	// Collect text from all parts.
	var b strings.Builder
	if msg != nil {
		for _, p := range msg.Parts {
			if t := p.Text(); t != "" {
				b.WriteString(t)
				b.WriteRune(' ')
			}
		}
	}
	raw := strings.TrimSpace(strings.ToLower(b.String()))

	// Strip "name " trigger if present.
	raw = strings.TrimPrefix(raw, "name ")

	words := strings.Fields(raw)
	var concepts []string
	for _, w := range words {
		if lang, ok := languageKeywords[w]; ok {
			req.lang = lang
			continue
		}
		if gender, ok := genderKeywords[w]; ok {
			req.gender = gender
			continue
		}
		if stopConcepts[w] || len(w) <= 1 {
			continue
		}
		concepts = append(concepts, w)
	}

	// Cap at 3 concepts.
	if len(concepts) > 3 {
		concepts = concepts[:3]
	}
	// Ensure at least one.
	if len(concepts) == 0 {
		concepts = []string{"star"}
	}
	req.concepts = concepts

	if req.lang == "q" {
		req.langName = "Quenya"
	} else {
		req.langName = "Sindarin"
	}
	return req
}

// isNameRequest returns true when the message looks like a name-generation
// request: starts with "name " or contains a language keyword.
func isNameRequest(msg *a2a.Message) bool {
	if msg == nil {
		return false
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text())
		b.WriteRune(' ')
	}
	text := strings.ToLower(strings.TrimSpace(b.String()))
	if strings.HasPrefix(text, "name ") {
		return true
	}
	for kw := range languageKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// ── Root selection ────────────────────────────────────────────────────────────

// isUsableWord returns true when the entry looks like a real Elvish word
// rather than a grammatical description (e.g. "active participle").
// Real Elvish words are single tokens without internal spaces.
func isUsableWord(w *index.FlatWord) bool {
	if w.Word == "" {
		return false
	}
	// Multi-word entries (e.g. "active participle") are metalinguistic labels.
	if strings.Contains(w.Word, " ") {
		return false
	}
	return true
}

// bestRoot picks the most useful FlatWord from a result set:
// prefer primary-category nouns/adjectives with a non-empty, usable word form.
func bestRoot(results []*index.FlatWord) *index.FlatWord {
	// Priority tiers: attested primary > neo > anything else.
	for _, want := range []string{"primary", "neo", ""} {
		for _, w := range results {
			if !isUsableWord(w) {
				continue
			}
			if want != "" && !strings.EqualFold(w.Category, want) {
				continue
			}
			sp := strings.ToLower(w.Speech)
			if strings.Contains(sp, "noun") || strings.Contains(sp, "adj") {
				return w
			}
		}
	}
	// Fall through to the first usable result regardless of speech.
	for _, w := range results {
		if isUsableWord(w) {
			return w
		}
	}
	return nil
}

// baseForm returns the compounding-ready form of a word: Stem if set,
// otherwise Word. Normalises to lower-case and strips trailing hyphens.
func baseForm(w *index.FlatWord) string {
	s := w.Stem
	if s == "" {
		s = w.Word
	}
	s = strings.ToLower(strings.TrimRight(s, "-"))
	return s
}

// ── Phonetic compounding ──────────────────────────────────────────────────────

var vowels = map[rune]bool{
	'a': true, 'e': true, 'i': true, 'o': true, 'u': true,
	'á': true, 'é': true, 'í': true, 'ó': true, 'ú': true,
	'â': true, 'ê': true, 'î': true, 'ô': true, 'û': true,
}

func isVowel(r rune) bool { return vowels[r] }

// joinRoots concatenates two root strings with basic Elvish phonetic rules:
//  1. Vowel elision: drop the final vowel of a when b begins with a vowel.
//  2. Consonant assimilation at the boundary (n+l→ll, r+l→ll, t+l→ld).
func joinRoots(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	ar := []rune(a)
	br := []rune(b)
	aEnd := ar[len(ar)-1]
	bStart := br[0]

	// Rule 1 — vowel elision.
	if isVowel(aEnd) && isVowel(bStart) {
		a = string(ar[:len(ar)-1])
		ar = []rune(a)
		if len(ar) == 0 {
			return b
		}
		aEnd = ar[len(ar)-1]
	}

	// Rule 2 — consonant assimilation at boundary.
	switch {
	case aEnd == 'n' && bStart == 'l':
		// n+l → ll: drop the n, double the l.
		return string(ar[:len(ar)-1]) + "ll" + string(br[1:])
	case aEnd == 'r' && bStart == 'l':
		// r+l → ll.
		return string(ar[:len(ar)-1]) + "ll" + string(br[1:])
	case aEnd == 't' && bStart == 'l':
		// t+l → ld.
		return string(ar[:len(ar)-1]) + "ld" + string(br[1:])
	}

	return a + b
}

// appendSuffix joins the compound base with the chosen suffix, applying
// vowel elision at the junction.
func appendSuffix(base, suffix string) string {
	if suffix == "" {
		return base
	}
	br := []rune(base)
	sr := []rune(suffix)
	if len(br) == 0 {
		return suffix
	}
	// Drop trailing vowel of base if suffix also starts with a vowel
	// (e.g. "elen" + "ndil" → the 'n' prefix handles it; keep as-is for
	// consonant-starting suffixes).
	if isVowel(br[len(br)-1]) && isVowel(sr[0]) {
		base = string(br[:len(br)-1])
	}
	return base + suffix
}

// capitalize title-cases the first rune of a string.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// ── The skill executor ────────────────────────────────────────────────────────

// runNameGenerate is the core execute function for the name-generate skill.
// It streams Working status events as it searches and compounds, then yields
// the final Message result.
func runNameGenerate(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := parseNameRequest(execCtx.Message)

		log.Printf("[Skill:name-generate] user=%q lang=%s gender=%s concepts=%v",
			execCtx.User.Name, req.lang, req.gender, req.concepts)

		// — Submitted ————————————————————————————————————————————————————————
		task := a2a.NewSubmittedTask(execCtx, execCtx.Message)
		if !yield(task, nil) {
			return
		}

		// — Working: announce search ——————————————————————————————————————————
		searchMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf(
				"Searching %s lexicon for: %s",
				req.langName, strings.Join(req.concepts, ", "))))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, searchMsg), nil) {
			return
		}

		// — Concept lookups ———————————————————————————————————————————————————
		type foundRoot struct {
			word    *index.FlatWord
			concept string
		}
		var roots []foundRoot

		for _, concept := range req.concepts {
			// Try attested primary words first, then fall back to all categories.
			candidates := lexiconIndex.SearchKeyword(concept, req.lang, "", "primary")
			if len(candidates) == 0 {
				candidates = lexiconIndex.SearchKeyword(concept, req.lang, "", "")
			}

			best := bestRoot(candidates)
			if best == nil {
				notFound := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf(
						"No %s root found for '%s'; skipping.", req.langName, concept)))
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, notFound), nil) {
					return
				}
				continue
			}

			roots = append(roots, foundRoot{word: best, concept: concept})
			gloss := best.Gloss
			if gloss == "" {
				gloss = best.NGloss
			}
			foundMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf(
					"Found '%s' (%s) for '%s'", best.Word, gloss, concept)))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, foundMsg), nil) {
				return
			}
		}

		if len(roots) == 0 {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf(
					"Could not find any %s roots for the given concepts. "+
						"Try different keywords or check the lexicon with enquire_lexicon.",
					req.langName)))
			// Failed is terminal — yield and return.
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		// — Compounding ———————————————————————————————————————————————————————
		compMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart("Applying compounding rules and selecting suffix..."))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, compMsg), nil) {
			return
		}

		// Build the compound base from all roots left-to-right.
		compound := ""
		var rootParts []string
		for _, r := range roots {
			base := baseForm(r.word)
			gloss := r.word.Gloss
			if gloss == "" {
				gloss = r.word.NGloss
			}
			rootParts = append(rootParts, fmt.Sprintf("%s/%s (%s → %s)", r.word.Word, base, r.concept, gloss))
			compound = joinRoots(compound, base)
		}

		// Append the language+gender suffix.
		var suffixTable map[string]string
		if req.lang == "q" {
			suffixTable = quenyaSuffixes
		} else {
			suffixTable = sindarinSuffixes
		}
		suffix := suffixTable[req.gender]
		name := capitalize(appendSuffix(compound, suffix))

		// — Final result ——————————————————————————————————————————————————————
		suffixMeaning := map[string]string{
			"ndil": "friend/lover of",
			"wen":  "maiden",
			"on":   "great/noble (masculine)",
			"iel":  "daughter/maiden",
		}[suffix]

		result := fmt.Sprintf(
			"**%s**\n\nRoots: %s\nSuffix: -%s (%s)\nLanguage: %s, %s",
			name,
			strings.Join(rootParts, " + "),
			suffix, suffixMeaning,
			req.langName, req.gender,
		)

		// Yield the result as an Artifact so A2A clients can surface it.
		// Then close with a terminal Completed status event (no embedded message).
		resultPart := a2a.NewTextPart(result)
		if !yield(a2a.NewArtifactEvent(execCtx, resultPart), nil) {
			return
		}
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil) {
			return
		}
	}
}
