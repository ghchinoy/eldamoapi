package main

// skill_translate.go — Phase 3b LLM-backed Tolkien translation skill.
//
// Architecture: two-phase executor.
//
//  Phase 1 (deterministic, in-process):
//    Parse target language and concept words from the user's message.
//    Search lexiconIndex for each concept to find attested Elvish roots.
//    Stream these as Working status events so the client sees progress immediately.
//
//  Phase 2 (LLM, Vertex AI / Gemini):
//    Build a prompt that includes the SKILL.md as the system instruction
//    (containing the full morphology rules, case tables, and mutation charts)
//    plus the user's source text and the lexicon candidates found in Phase 1.
//    Call GenerateContentStream and yield each text chunk as a Working event.
//    Accumulate the full response and emit it as the terminal Artifact.
//
// Trigger: message starts with "translate" (case-insensitive).
//
// Required env vars:
//   GCP_PROJECT            — GCP project ID (shared with Firebase / deploy.sh)
//   GEMINI_LOCATION        — Vertex AI API location (default "global"; separate
//                            from GCP_REGION which is the Cloud Run deploy region)
//   GEMINI_TRANSLATE_MODEL — Vertex AI model name; skill self-hides if unset
//
// Skill scope: skill:translate

import (
	"context"
	_ "embed"
	"fmt"
	"iter"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/genai"
)

// translateSkillMD is the full SKILL.md embedded at compile time.
// It becomes the Gemini system instruction verbatim, providing the model with
// the Quenya case tables, Sindarin mutation charts, and worked examples.
//
//go:embed skills/tolkien-translation/SKILL.md
var translateSkillMD string

// translateModelDefault is used when GEMINI_TRANSLATE_MODEL is not set to test
// whether the skill is enabled, but never actually called in that state.
const translateModelDefault = "gemini-3.1-flash-lite"

// TranslateEnabled reports whether the translate skill is configured.
// The skill self-hides from the AgentCard and executor when this is false,
// mirroring the ELVISH_TTS_URL / render_elvish_audio conditional pattern.
func TranslateEnabled() bool {
	return os.Getenv("GEMINI_TRANSLATE_MODEL") != ""
}

// translateModel returns the configured Gemini model name.
func translateModel() string {
	if m := os.Getenv("GEMINI_TRANSLATE_MODEL"); m != "" {
		return m
	}
	return translateModelDefault
}

// ── GenAI client (shared, lazy-initialized) ───────────────────────────────────

var (
	genaiOnce   sync.Once
	genaiClient *genai.Client
	genaiErr    error
)

// getGenAIClient returns a shared Vertex AI client, initializing it on the
// first call. Credentials come from Application Default Credentials (ADC),
// which are automatically available on Cloud Run via the service account.
func getGenAIClient() (*genai.Client, error) {
	genaiOnce.Do(func() {
		project := os.Getenv("GCP_PROJECT")
		location := os.Getenv("GEMINI_LOCATION")
		if location == "" {
			location = "global" // default: newer Gemini models require the global endpoint
		}
		log.Printf("[Translate] Initializing Vertex AI client (project=%s, location=%s)", project, location)
		genaiClient, genaiErr = genai.NewClient(context.Background(), &genai.ClientConfig{
			Project:  project,
			Location: location,
			Backend:  genai.BackendVertexAI,
		})
		if genaiErr != nil {
			log.Printf("[Translate] Failed to create Vertex AI client: %v", genaiErr)
		}
	})
	return genaiClient, genaiErr
}

// ── Input parsing ─────────────────────────────────────────────────────────────

// translateRequest holds the parsed skill parameters.
type translateRequest struct {
	sourceLang string // "auto" (default) or an explicit source language
	targetLang string // "q" (Quenya) or "s" (Sindarin)
	targetName string // "Quenya" or "Sindarin"
	sourceText string // the text to translate
}

var translateTargetKeywords = map[string]string{
	"quenya":   "q",
	"sindarin": "s",
}

// isTranslateRequest returns true when the message looks like a translation request.
func isTranslateRequest(msg *a2a.Message) bool {
	if msg == nil {
		return false
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text())
		b.WriteRune(' ')
	}
	text := strings.ToLower(strings.TrimSpace(b.String()))
	return strings.HasPrefix(text, "translate")
}

// parseTranslateRequest extracts the target language and source text.
// Handles formats like:
//
//	"translate 'elen síla' to English"
//	"translate to quenya: farewell my friend"
//	"translate the grey havens to sindarin"
func parseTranslateRequest(msg *a2a.Message) translateRequest {
	req := translateRequest{
		sourceLang: "auto",
		targetLang: "q",
		targetName: "Quenya",
	}
	if msg == nil {
		req.sourceText = ""
		return req
	}

	var b strings.Builder
	for _, p := range msg.Parts {
		if t := p.Text(); t != "" {
			b.WriteString(t)
			b.WriteRune(' ')
		}
	}
	raw := strings.TrimSpace(b.String())

	// Strip the "translate" trigger word.
	lower := strings.ToLower(raw)
	lower = strings.TrimPrefix(lower, "translate")
	raw = raw[len(raw)-len(lower):]
	raw = strings.TrimSpace(raw)

	// Strip connective words at the start ("to", "into", "in").
	for _, strip := range []string{"to ", "into ", "in "} {
		if strings.HasPrefix(strings.ToLower(raw), strip) {
			raw = raw[len(strip):]
			break
		}
	}

	// Find and extract language keyword plus any leading connector ("to", "into", "in").
	lowerRaw := strings.ToLower(raw)
	for kw, code := range translateTargetKeywords {
		if idx := strings.Index(lowerRaw, kw); idx >= 0 {
			req.targetLang = code
			if code == "q" {
				req.targetName = "Quenya"
			} else {
				req.targetName = "Sindarin"
			}
			// Also strip a connector word immediately before the language keyword.
			before := strings.TrimRight(raw[:idx], " ")
			for _, conn := range []string{" to", " into", " in"} {
				if strings.HasSuffix(strings.ToLower(before), conn) {
					before = before[:len(before)-len(conn)]
					break
				}
			}
			raw = strings.TrimSpace(before + raw[idx+len(kw):])
			raw = strings.Trim(raw, " :,-")
			break
		}
	}

	// Strip surrounding quotes if present.
	raw = strings.Trim(raw, `"'`)
	req.sourceText = strings.TrimSpace(raw)
	if req.sourceText == "" {
		req.sourceText = "(no text provided)"
	}
	return req
}

// ── Lexicon pre-fetch ─────────────────────────────────────────────────────────

// conceptsFromText tokenizes source text into candidate concept words,
// filtering English stop words and very short tokens.
func conceptsFromText(text string) []string {
	textStopWords := map[string]bool{
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
		if len(w) < 3 || textStopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) >= 5 { // cap to avoid flooding the prompt
			break
		}
	}
	return out
}

// lexiconContext searches for each concept word and returns a formatted string
// of lexicon candidates for inclusion in the LLM prompt.
func lexiconContext(concepts []string, lang string) ([]string, string) {
	var found []string
	var lines []string
	for _, concept := range concepts {
		results := lexiconIndex.SearchKeyword(concept, lang, "", "primary")
		if len(results) == 0 {
			results = lexiconIndex.SearchKeyword(concept, lang, "", "")
		}
		best := bestRoot(results)
		if best == nil {
			continue
		}
		gloss := best.Gloss
		if gloss == "" {
			gloss = best.NGloss
		}
		found = append(found, fmt.Sprintf("%s → %s (%s)", concept, best.Word, gloss))
		lines = append(lines, fmt.Sprintf("  %-20s  %s (%s, %s, %s)",
			concept, best.Word, gloss, best.Speech, best.Category))
	}
	return found, strings.Join(lines, "\n")
}

// ── The skill executor ────────────────────────────────────────────────────────

// runTranslate is the execute function for the translate skill.
// Phase 1 streams lexicon lookup results; Phase 2 streams Gemini output.
func runTranslate(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := parseTranslateRequest(execCtx.Message)
		log.Printf("[Skill:translate] user=%q target=%s source=%q",
			execCtx.User.Name, req.targetName, req.sourceText)

		// — Submitted ─────────────────────────────────────────────────────────
		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		// — Phase 1: lexicon pre-fetch ────────────────────────────────────────
		startMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf(
				"Searching %s lexicon for source concepts...", req.targetName)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, startMsg), nil) {
			return
		}

		concepts := conceptsFromText(req.sourceText)
		foundSummaries, contextBlock := lexiconContext(concepts, req.targetLang)
		if len(foundSummaries) > 0 {
			lexMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Lexicon candidates:\n"+strings.Join(foundSummaries, "\n")))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, lexMsg), nil) {
				return
			}
		}

		// — Phase 2: Gemini translation ───────────────────────────────────────
		client, err := getGenAIClient()
		if err != nil {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf("Failed to connect to Vertex AI: %v", err)))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		thinkingMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf(
				"Translating to %s with Gemini (%s)...", req.targetName, translateModel())))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, thinkingMsg), nil) {
			return
		}

		// Build the user prompt: source text + lexicon context.
		userPrompt := buildTranslatePrompt(req, contextBlock)

		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: translateSkillMD}},
			},
		}

		// Stream Gemini output — buffer into sentences to avoid yielding
		// hundreds of single-token events; yield when we hit a natural break.
		var full strings.Builder
		var buf strings.Builder

		flushBuf := func() bool {
			s := strings.TrimSpace(buf.String())
			if s == "" {
				return true
			}
			chunk := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(s))
			buf.Reset()
			return yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, chunk), nil)
		}

		for chunk, streamErr := range client.Models.GenerateContentStream(
			ctx, translateModel(), genai.Text(userPrompt), config) {
			if streamErr != nil {
				log.Printf("[Skill:translate] stream error: %v", streamErr)
				failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf("Generation error: %v", streamErr)))
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
				return
			}
			text := chunk.Text()
			full.WriteString(text)
			buf.WriteString(text)

			// Flush on sentence boundaries or when buffer is large enough.
			b := buf.String()
			if strings.ContainsAny(b, ".!?\n") || len(b) > 150 {
				if !flushBuf() {
					return
				}
			}
		}
		// Flush any remaining buffered text.
		if !flushBuf() {
			return
		}

		translation := strings.TrimSpace(full.String())
		if translation == "" {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Gemini returned an empty response. Try rephrasing the request."))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		// — Artifact: final translation ───────────────────────────────────────
		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(translation)), nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

// buildTranslatePrompt assembles the user-turn message for Gemini, combining
// the source text with the lexicon candidates found in Phase 1.
func buildTranslatePrompt(req translateRequest, contextBlock string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TRANSLATION REQUEST\n")
	fmt.Fprintf(&sb, "===================\n")
	fmt.Fprintf(&sb, "Source text:     %q\n", req.sourceText)
	fmt.Fprintf(&sb, "Target language: %s\n\n", req.targetName)
	if contextBlock != "" {
		fmt.Fprintf(&sb, "RELEVANT ELDAMO LEXICON ENTRIES\n")
		fmt.Fprintf(&sb, "================================\n")
		sb.WriteString(contextBlock)
		fmt.Fprintf(&sb, "\n\nUse the lexicon entries above as your primary vocabulary source.\n")
		fmt.Fprintf(&sb, "Follow the step-by-step workflow in your system instructions.\n")
		fmt.Fprintf(&sb, "Apply the correct inflections, case endings, and consonant mutations.\n")
	}
	return sb.String()
}
