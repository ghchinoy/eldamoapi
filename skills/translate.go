package skills

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// ── Input parsing ─────────────────────────────────────────────────────────────

type translateRequest struct {
	sourceLang string
	targetLang string
	targetName string
	sourceText string
}

// IsTranslateRequest returns true when the message triggers the translate skill.
func IsTranslateRequest(msg *a2a.Message) bool {
	if msg == nil {
		return false
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text())
		b.WriteRune(' ')
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(b.String())), "translate")
}

// ParseTranslateRequest extracts the target language and source text.
func ParseTranslateRequest(msg *a2a.Message) translateRequest {
	req := translateRequest{sourceLang: "auto", targetLang: "q", targetName: "Quenya"}
	if msg == nil {
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

	// Strip "translate" trigger.
	lower := strings.ToLower(raw)
	lower = strings.TrimPrefix(lower, "translate")
	raw = raw[len(raw)-len(lower):]
	raw = strings.TrimSpace(raw)

	// Strip leading connector.
	for _, strip := range []string{"to ", "into ", "in "} {
		if strings.HasPrefix(strings.ToLower(raw), strip) {
			raw = raw[len(strip):]
			break
		}
	}

	// Extract language keyword + strip preceding connector.
	lowerRaw := strings.ToLower(raw)
	for kw, code := range TargetLanguageKeywords {
		if idx := strings.Index(lowerRaw, kw); idx >= 0 {
			req.targetLang = code
			if code == "q" {
				req.targetName = "Quenya"
			} else {
				req.targetName = "Sindarin"
			}
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
	raw = strings.Trim(raw, `"'`)
	req.sourceText = strings.TrimSpace(raw)
	if req.sourceText == "" {
		req.sourceText = "(no text provided)"
	}
	return req
}

// ── Lexicon pre-fetch ─────────────────────────────────────────────────────────

// lexiconContext searches each concept and returns a formatted context block
// for inclusion in the LLM prompt.
func lexiconContext(concepts []string, lang string, idx LexiconSearcher) ([]string, string) {
	var found []string
	var lines []string
	for _, concept := range concepts {
		results := idx.SearchKeyword(concept, lang, "", "primary")
		if len(results) == 0 {
			results = idx.SearchKeyword(concept, lang, "", "")
		}
		best := BestRoot(results)
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

// ── Executor ──────────────────────────────────────────────────────────────────

// RunTranslate is the translation skill executor.
// Phase 1 streams lexicon candidates; Phase 2 streams the configured LLM's output.
func RunTranslate(ctx context.Context, execCtx *a2asrv.ExecutorContext, deps *Deps) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := ParseTranslateRequest(execCtx.Message)
		log.Printf("[Skill:translate] user=%q target=%s source=%q",
			execCtx.User.Name, req.targetName, req.sourceText)

		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		startMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Searching %s lexicon for source concepts...", req.targetName)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, startMsg), nil) {
			return
		}

		concepts := ConceptsFromText(req.sourceText)
		foundSummaries, contextBlock := lexiconContext(concepts, req.targetLang, deps.Index)
		if len(foundSummaries) > 0 {
			lexMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Lexicon candidates:\n"+strings.Join(foundSummaries, "\n")))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, lexMsg), nil) {
				return
			}
		}

		thinkingMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Translating to %s (model: %s)...", req.targetName, deps.ModelName)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, thinkingMsg), nil) {
			return
		}

		userPrompt := buildTranslatePrompt(req, contextBlock)

		var full strings.Builder
		var buf strings.Builder
		var usage *Usage
		start := time.Now()

		flushBuf := func() bool {
			s := strings.TrimSpace(buf.String())
			if s == "" {
				return true
			}
			chunk := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, a2a.NewTextPart(s))
			buf.Reset()
			return yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, chunk), nil)
		}

		for chunk, streamErr := range deps.LLM.GenerateContentStream(
			ctx, deps.ModelName, deps.TranslateMD, userPrompt) {
			if streamErr != nil {
				LogUsage("translate", execCtx.User.Name, usage, time.Since(start), streamErr)
				log.Printf("[Skill:translate] stream error: %v", streamErr)
				failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf("Generation error: %v", streamErr)))
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
				return
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			text := chunk.Text
			full.WriteString(text)
			buf.WriteString(text)
			b := buf.String()
			if strings.ContainsAny(b, ".!?\n") || len(b) > 150 {
				if !flushBuf() {
					return
				}
			}
		}
		if !flushBuf() {
			return
		}

		translation := strings.TrimSpace(full.String())
		if translation == "" {
			LogUsage("translate", execCtx.User.Name, usage, time.Since(start), errors.New("empty response from LLM backend"))
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("The LLM backend returned an empty response."))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}
		LogUsage("translate", execCtx.User.Name, usage, time.Since(start), nil)

		if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(translation)), nil) {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func buildTranslatePrompt(req translateRequest, contextBlock string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "TRANSLATION REQUEST\n===================\n")
	fmt.Fprintf(&sb, "Source text:     %q\n", req.sourceText)
	fmt.Fprintf(&sb, "Target language: %s\n\n", req.targetName)
	if contextBlock != "" {
		fmt.Fprintf(&sb, "RELEVANT ELDAMO LEXICON ENTRIES\n================================\n")
		sb.WriteString(contextBlock)
		fmt.Fprintf(&sb, "\n\nUse the lexicon entries above as your primary vocabulary source.\n")
		fmt.Fprintf(&sb, "Follow the step-by-step workflow in your system instructions.\n")
		fmt.Fprintf(&sb, "Apply the correct inflections, case endings, and consonant mutations.\n")
	}
	return sb.String()
}
