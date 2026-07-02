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

const (
	PracticalDelim = "=== PRACTICAL PATH ==="
	PoeticDelim    = "=== POETIC PATH ==="
)

// ── Input parsing ─────────────────────────────────────────────────────────────

type neologismRequest struct {
	lang     string
	langName string
	concept  string
}

// neologismPrefixes must appear at the start of the message (short action verbs
// that are ambiguous mid-sentence). neologismKeywords match anywhere — "neologism"
// is unambiguous and commonly appears mid-sentence in natural prompts like
// "I need a neologism for...".
var neologismPrefixes = []string{"coin ", "invent "}
var neologismKeywords = []string{"neologism"}

// IsNeologismRequest returns true when the message triggers the neologism skill.
func IsNeologismRequest(msg *a2a.Message) bool {
	if msg == nil {
		return false
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text())
		b.WriteRune(' ')
	}
	text := strings.ToLower(strings.TrimSpace(b.String()))
	for _, kw := range neologismKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	for _, prefix := range neologismPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// ParseNeologismRequest extracts the target language and concept.
func ParseNeologismRequest(msg *a2a.Message) neologismRequest {
	req := neologismRequest{lang: "q", langName: "Quenya"}
	if msg == nil {
		req.concept = "unknown concept"
		return req
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		if t := p.Text(); t != "" {
			b.WriteString(t)
			b.WriteRune(' ')
		}
	}
	raw := strings.TrimSpace(strings.ToLower(b.String()))

	// Strip any leading trigger prefix so the remainder is the concept.
	// Keywords (e.g. "neologism") may appear mid-sentence; strip them and
	// leading filler so the concept is as clean as possible.
	for _, kw := range neologismKeywords {
		if strings.HasPrefix(raw, kw+" ") {
			raw = strings.TrimSpace(raw[len(kw)+1:])
			break
		}
		if idx := strings.Index(raw, " "+kw+" "); idx >= 0 {
			raw = strings.TrimSpace(raw[idx+len(kw)+2:])
			break
		}
	}
	for _, prefix := range neologismPrefixes {
		if strings.HasPrefix(raw, prefix) {
			raw = strings.TrimSpace(raw[len(prefix):])
			break
		}
	}
	for _, strip := range []string{"a word for ", "word for ", "the word for ", "for "} {
		if strings.HasPrefix(raw, strip) {
			raw = strings.TrimSpace(raw[len(strip):])
			break
		}
	}
	raw = strings.Trim(raw, `: "'`)

	for kw, code := range TargetLanguageKeywords {
		if idx := strings.Index(raw, kw); idx >= 0 {
			req.lang = code
			if code == "q" {
				req.langName = "Quenya"
			} else {
				req.langName = "Sindarin"
			}
			before := strings.TrimRight(raw[:idx], " ")
			raw = strings.TrimSpace(before + raw[idx+len(kw):])
			raw = strings.Trim(raw, " :,-")
			break
		}
	}
	req.concept = strings.TrimSpace(raw)
	if req.concept == "" {
		req.concept = "unnamed concept"
	}
	return req
}

// ── Anchorage pre-fetch ───────────────────────────────────────────────────────

func anchorageContext(concept string, lang string, idx LexiconSearcher) ([]string, string) {
	var summaries []string
	var lines []string
	for _, kw := range ConceptsFromText(concept) {
		roots := idx.SearchKeyword(kw, lang, "", "primary")
		if len(roots) == 0 {
			roots = idx.SearchKeyword(kw, lang, "", "")
		}
		root := BestRoot(roots)
		if root == nil {
			continue
		}
		gloss := root.Gloss
		if gloss == "" {
			gloss = root.NGloss
		}
		summaries = append(summaries, fmt.Sprintf("'%s' → %s (%s)", kw, root.Word, gloss))

		anchors := idx.GetRootAnchors(root.ID)
		anchorNames := make([]string, 0, 4)
		for _, a := range anchors {
			if len(anchorNames) >= 4 {
				break
			}
			anchorNames = append(anchorNames, fmt.Sprintf("%s (%s)", a.Word, a.Gloss))
		}
		anchorStr := "none found"
		if len(anchorNames) > 0 {
			anchorStr = strings.Join(anchorNames, ", ")
		}
		lines = append(lines, fmt.Sprintf("  %-20s  %-20s  %s\n  anchors: %s",
			kw, root.Word+"("+gloss+")", root.Category, anchorStr))
	}
	return summaries, strings.Join(lines, "\n\n")
}

// SplitNeologismPaths parses the delimited LLM response.
func SplitNeologismPaths(response string) (practical, poetic string) {
	practIdx := strings.Index(response, PracticalDelim)
	poetIdx := strings.Index(response, PoeticDelim)
	if practIdx < 0 && poetIdx < 0 {
		return "", ""
	}
	if practIdx >= 0 && poetIdx >= 0 {
		if practIdx < poetIdx {
			practical = strings.TrimSpace(response[practIdx+len(PracticalDelim) : poetIdx])
			poetic = strings.TrimSpace(response[poetIdx+len(PoeticDelim):])
		} else {
			poetic = strings.TrimSpace(response[poetIdx+len(PoeticDelim) : practIdx])
			practical = strings.TrimSpace(response[practIdx+len(PracticalDelim):])
		}
		return
	}
	if practIdx >= 0 {
		practical = strings.TrimSpace(response[practIdx+len(PracticalDelim):])
	}
	if poetIdx >= 0 {
		poetic = strings.TrimSpace(response[poetIdx+len(PoeticDelim):])
	}
	return
}

// ── Executor ──────────────────────────────────────────────────────────────────

// RunNeologism is the neologism-builder skill executor.
// Streams anchorage findings as Working events, then yields two Artifacts.
func RunNeologism(ctx context.Context, execCtx *a2asrv.ExecutorContext, deps *Deps) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := ParseNeologismRequest(execCtx.Message)
		log.Printf("[Skill:neologism] user=%q lang=%s concept=%q",
			execCtx.User.Name, req.langName, req.concept)

		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		startMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Searching %s lexicon and running Anchorage Protocol for: %q",
				req.langName, req.concept)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, startMsg), nil) {
			return
		}

		summaries, contextBlock := anchorageContext(req.concept, req.lang, deps.Index)
		if len(summaries) > 0 {
			anchMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Roots and anchors found:\n"+strings.Join(summaries, "\n")))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, anchMsg), nil) {
				return
			}
		}

		thinkMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf("Building two-path neologism (model: %s)...", deps.ModelName)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, thinkMsg), nil) {
			return
		}

		userPrompt := buildNeologismPrompt(req, contextBlock)

		var full strings.Builder
		var usage *Usage
		start := time.Now()
		for chunk, streamErr := range deps.LLM.GenerateContentStream(
			ctx, deps.ModelName, deps.NeologismMD, userPrompt) {
			if streamErr != nil {
				LogUsage("neologism", execCtx.User.Name, usage, time.Since(start), streamErr)
				failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf("Generation error: %v", streamErr)))
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
				return
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			full.WriteString(chunk.Text)
		}

		response := strings.TrimSpace(full.String())
		if response == "" {
			LogUsage("neologism", execCtx.User.Name, usage, time.Since(start), errors.New("empty response from LLM backend"))
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("The LLM backend returned an empty response."))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}
		LogUsage("neologism", execCtx.User.Name, usage, time.Since(start), nil)

		practical, poetic := SplitNeologismPaths(response)
		if practical != "" {
			art := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(practical))
			art.Artifact.Name = "Practical Path"
			if !yield(art, nil) {
				return
			}
		}
		if poetic != "" {
			art := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(poetic))
			art.Artifact.Name = "Poetic Path"
			if !yield(art, nil) {
				return
			}
		}
		if practical == "" && poetic == "" {
			art := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(response))
			art.Artifact.Name = "Neologism"
			if !yield(art, nil) {
				return
			}
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

func buildNeologismPrompt(req neologismRequest, contextBlock string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "NEOLOGISM REQUEST\n=================\n")
	fmt.Fprintf(&sb, "Concept to name: %q\n", req.concept)
	fmt.Fprintf(&sb, "Target language: %s\n\n", req.langName)
	if contextBlock != "" {
		fmt.Fprintf(&sb, "ELDAMO LEXICON & ANCHORAGE CONTEXT\n====================================\n")
		sb.WriteString(contextBlock)
		fmt.Fprintf(&sb, "\n\n")
	}
	fmt.Fprintf(&sb, "INSTRUCTIONS\n============\n")
	fmt.Fprintf(&sb, "Follow the two-path approach from your system instructions.\n")
	fmt.Fprintf(&sb, "Structure your response with EXACTLY these section headers:\n\n")
	fmt.Fprintf(&sb, "%s\n(your Practical/Functional path analysis and proposed word)\n\n", PracticalDelim)
	fmt.Fprintf(&sb, "%s\n(your Poetic/Metaphorical path analysis and proposed word)\n\n", PoeticDelim)
	fmt.Fprintf(&sb, "For each path:\n")
	fmt.Fprintf(&sb, "- Show the root → primitive → modern form derivation\n")
	fmt.Fprintf(&sb, "- Apply the phonotactic constraints for %s\n", req.langName)
	fmt.Fprintf(&sb, "- Apply the 100-point scoring matrix\n")
	fmt.Fprintf(&sb, "- Expand into noun, verb, and adjective forms if possible\n")
	return sb.String()
}
