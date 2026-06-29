package skills

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

// ── Suffix tables ─────────────────────────────────────────────────────────────

var quenyaSuffixes = map[string]string{
	"masculine": "ndil",
	"feminine":  "wen",
}

var sindarinSuffixes = map[string]string{
	"masculine": "on",
	"feminine":  "iel",
}

// ── Language / gender keywords ────────────────────────────────────────────────

var genderKeywords = map[string]string{
	"masculine": "masculine", "male": "masculine", "man": "masculine",
	"father": "masculine", "son": "masculine",
	"feminine": "feminine", "female": "feminine", "woman": "feminine",
	"maiden": "feminine", "daughter": "feminine", "mother": "feminine",
}

var stopConcepts = map[string]bool{
	"name": true, "generate": true, "make": true, "create": true,
	"for": true, "a": true, "an": true, "the": true, "of": true,
}

// ── nameRequest ───────────────────────────────────────────────────────────────

type nameRequest struct {
	lang     string
	langName string
	gender   string
	concepts []string
}

// ParseNameRequest extracts language, gender, and concept keywords.
func ParseNameRequest(msg *a2a.Message) nameRequest {
	req := nameRequest{lang: "q", gender: "masculine"}
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
	raw = strings.TrimPrefix(raw, "name ")
	for _, w := range strings.Fields(raw) {
		if lang, ok := TargetLanguageKeywords[w]; ok {
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
		req.concepts = append(req.concepts, w)
	}
	if len(req.concepts) > 3 {
		req.concepts = req.concepts[:3]
	}
	if len(req.concepts) == 0 {
		req.concepts = []string{"star"}
	}
	if req.lang == "q" {
		req.langName = "Quenya"
	} else {
		req.langName = "Sindarin"
	}
	return req
}

// IsNameRequest returns true when the message triggers the name-generation skill.
func IsNameRequest(msg *a2a.Message) bool {
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
	for kw := range TargetLanguageKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// ── Root selection helpers ────────────────────────────────────────────────────

// IsUsableWord returns true when the entry looks like a real Elvish word
// (single token, non-empty) rather than a grammatical label.
func IsUsableWord(w *index.FlatWord) bool {
	return w.Word != "" && !strings.Contains(w.Word, " ")
}

// BestRoot picks the most useful FlatWord: primary nouns/adjectives preferred.
func BestRoot(results []*index.FlatWord) *index.FlatWord {
	for _, want := range []string{"primary", "neo", ""} {
		for _, w := range results {
			if !IsUsableWord(w) {
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
	for _, w := range results {
		if IsUsableWord(w) {
			return w
		}
	}
	return nil
}

// BaseForm returns the compounding-ready form: Stem if set, otherwise Word.
func BaseForm(w *index.FlatWord) string {
	s := w.Stem
	if s == "" {
		s = w.Word
	}
	return strings.ToLower(strings.TrimRight(s, "-"))
}

// ── Phonetic compounding ──────────────────────────────────────────────────────

var vowels = map[rune]bool{
	'a': true, 'e': true, 'i': true, 'o': true, 'u': true,
	'á': true, 'é': true, 'í': true, 'ó': true, 'ú': true,
	'â': true, 'ê': true, 'î': true, 'ô': true, 'û': true,
}

func isVowel(r rune) bool { return vowels[r] }

// JoinRoots concatenates two Elvish root strings with phonetic rules:
// 1. Vowel elision when both boundary characters are vowels.
// 2. Consonant assimilation: n+l→ll, r+l→ll, t+l→ld.
func JoinRoots(a, b string) string {
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
	if isVowel(aEnd) && isVowel(bStart) {
		a = string(ar[:len(ar)-1])
		ar = []rune(a)
		if len(ar) == 0 {
			return b
		}
		aEnd = ar[len(ar)-1]
	}
	switch {
	case aEnd == 'n' && bStart == 'l':
		return string(ar[:len(ar)-1]) + "ll" + string(br[1:])
	case aEnd == 'r' && bStart == 'l':
		return string(ar[:len(ar)-1]) + "ll" + string(br[1:])
	case aEnd == 't' && bStart == 'l':
		return string(ar[:len(ar)-1]) + "ld" + string(br[1:])
	}
	return a + b
}

// AppendSuffix joins base + suffix with vowel elision at the junction.
func AppendSuffix(base, suffix string) string {
	if suffix == "" {
		return base
	}
	br := []rune(base)
	sr := []rune(suffix)
	if len(br) == 0 {
		return suffix
	}
	if isVowel(br[len(br)-1]) && isVowel(sr[0]) {
		base = string(br[:len(br)-1])
	}
	return base + suffix
}

// Capitalize title-cases the first rune.
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// ── Executor ──────────────────────────────────────────────────────────────────

// RunNameGenerate is the name-generation skill executor.
// Streams Working events for each concept lookup, yields an Artifact + Completed.
func RunNameGenerate(_ context.Context, execCtx *a2asrv.ExecutorContext, deps *Deps) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := ParseNameRequest(execCtx.Message)
		log.Printf("[Skill:name-generate] user=%q lang=%s gender=%s concepts=%v",
			execCtx.User.Name, req.lang, req.gender, req.concepts)

		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}
		searchMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Searching %s lexicon for: %s",
				req.langName, strings.Join(req.concepts, ", "))))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, searchMsg), nil) {
			return
		}

		type foundRoot struct {
			word    *index.FlatWord
			concept string
		}
		var roots []foundRoot

		for _, concept := range req.concepts {
			candidates := deps.Index.SearchKeyword(concept, req.lang, "", "primary")
			if len(candidates) == 0 {
				candidates = deps.Index.SearchKeyword(concept, req.lang, "", "")
			}
			best := BestRoot(candidates)
			if best == nil {
				msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf("No %s root for '%s'; skipping.", req.langName, concept)))
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, msg), nil) {
					return
				}
				continue
			}
			roots = append(roots, foundRoot{word: best, concept: concept})
			gloss := best.Gloss
			if gloss == "" {
				gloss = best.NGloss
			}
			msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf("Found '%s' (%s) for '%s'", best.Word, gloss, concept)))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, msg), nil) {
				return
			}
		}

		if len(roots) == 0 {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf("Could not find any %s roots. Try different keywords.", req.langName)))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		compMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart("Applying compounding rules and selecting suffix..."))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, compMsg), nil) {
			return
		}

		compound := ""
		var rootParts []string
		for _, r := range roots {
			base := BaseForm(r.word)
			gloss := r.word.Gloss
			if gloss == "" {
				gloss = r.word.NGloss
			}
			rootParts = append(rootParts, fmt.Sprintf("%s/%s (%s → %s)", r.word.Word, base, r.concept, gloss))
			compound = JoinRoots(compound, base)
		}

		var suffixTable map[string]string
		if req.lang == "q" {
			suffixTable = quenyaSuffixes
		} else {
			suffixTable = sindarinSuffixes
		}
		suffix := suffixTable[req.gender]
		name := Capitalize(AppendSuffix(compound, suffix))

		suffixMeaning := map[string]string{
			"ndil": "friend/lover of",
			"wen":  "maiden",
			"on":   "great/noble (masculine)",
			"iel":  "daughter/maiden",
		}[suffix]

		result := fmt.Sprintf(
			"**%s**\n\nRoots: %s\nSuffix: -%s (%s)\nLanguage: %s, %s",
			name, strings.Join(rootParts, " + "), suffix, suffixMeaning, req.langName, req.gender)

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}
