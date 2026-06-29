package main

// skill_neologism.go — Phase 3c LLM-backed Elvish neologism-builder skill.
//
// Architecture: two-phase executor producing two named artifacts.
//
//  Phase 1 (deterministic, in-process):
//    Parse the concept and target language from the user's message.
//    Search lexiconIndex for relevant primitive roots.
//    Run GetRootAnchors for each root to surface attested proper-noun anchors —
//    these ground the phonetic and grammatical behaviour of the proposed word
//    (the Anchorage Protocol from the SKILL.md).
//    Stream all findings as Working status events.
//
//  Phase 2 (LLM, Vertex AI / Gemini):
//    Build a prompt with the SKILL.md as the system instruction (two-path
//    approach, acoustic iconicity rule, phonotactic constraints, scoring matrix).
//    Ask Gemini to respond with clearly labelled sections:
//      === PRACTICAL PATH === and === POETIC PATH ===
//    Split the response on those delimiters and emit two separate ArtifactEvents
//    so A2A clients can surface both paths as distinct artifacts.
//
// Trigger: message starts with "neologism", "coin", or "invent" (case-insensitive).
//
// Required env vars (shared with translate skill):
//   GEMINI_TRANSLATE_MODEL — Vertex AI model; skill self-hides when unset.
//
// Skill scope: skill:neologism

import (
	"context"
	"fmt"
	"iter"
	"log"
	"strings"
	_ "embed"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/genai"
)

//go:embed skills/neologism-builder/SKILL.md
var neologismSkillMD string

// Delimiters Gemini is instructed to use for the two-path response.
const (
	practicalDelim = "=== PRACTICAL PATH ==="
	poeticDelim    = "=== POETIC PATH ==="
)

// ── Input parsing ─────────────────────────────────────────────────────────────

// neologismRequest holds the parsed skill parameters.
type neologismRequest struct {
	lang     string // "q" (Quenya, default) or "s" (Sindarin)
	langName string
	concept  string // the concept to create a word for
}

var neologismTriggers = []string{"neologism ", "coin ", "invent "}

// isNeologismRequest returns true when the message is a neologism creation request.
func isNeologismRequest(msg *a2a.Message) bool {
	if msg == nil {
		return false
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text())
		b.WriteRune(' ')
	}
	text := strings.ToLower(strings.TrimSpace(b.String()))
	for _, trigger := range neologismTriggers {
		if strings.HasPrefix(text, trigger) {
			return true
		}
	}
	return false
}

// parseNeologismRequest extracts target language and concept from message text.
// Handles formats like:
//
//	"neologism hover-board quenya"
//	"coin a word for artificial intelligence sindarin"
//	"invent: blockchain in quenya"
func parseNeologismRequest(msg *a2a.Message) neologismRequest {
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

	// Strip trigger word.
	for _, trigger := range neologismTriggers {
		if strings.HasPrefix(raw, trigger) {
			raw = strings.TrimSpace(raw[len(trigger):])
			break
		}
	}
	// Strip connectives.
	for _, strip := range []string{"a word for ", "word for ", "the word for ", "for "} {
		if strings.HasPrefix(raw, strip) {
			raw = strings.TrimSpace(raw[len(strip):])
			break
		}
	}
	// Strip surrounding punctuation.
	raw = strings.Trim(raw, `: "'`)

	// Detect and remove language keyword.
	for kw, code := range translateTargetKeywords { // reuse translate's map
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

// anchorageContext searches for roots matching the concept, then calls
// GetRootAnchors for each to find attested proper-noun descendants.
// Returns a human-readable context block for the LLM prompt.
func anchorageContext(concept string, lang string) ([]string, string) {
	var summaries []string
	var lines []string

	concepts := conceptsFromText(concept) // reuse translate's helper
	for _, kw := range concepts {
		roots := lexiconIndex.SearchKeyword(kw, lang, "", "primary")
		if len(roots) == 0 {
			roots = lexiconIndex.SearchKeyword(kw, lang, "", "")
		}
		root := bestRoot(roots)
		if root == nil {
			continue
		}
		gloss := root.Gloss
		if gloss == "" {
			gloss = root.NGloss
		}
		summaries = append(summaries, fmt.Sprintf("'%s' → %s (%s)", kw, root.Word, gloss))

		// Anchorage: find attested proper-noun descendants.
		anchors := lexiconIndex.GetRootAnchors(root.ID)
		anchorNames := make([]string, 0, len(anchors))
		for _, a := range anchors {
			if len(anchorNames) >= 4 { // cap display
				break
			}
			anchorNames = append(anchorNames, fmt.Sprintf("%s (%s)", a.Word, a.Gloss))
		}
		anchorStr := "none found"
		if len(anchorNames) > 0 {
			anchorStr = strings.Join(anchorNames, ", ")
		}
		lines = append(lines, fmt.Sprintf(
			"  %-20s  %-20s  %s\n  anchors: %s",
			kw, root.Word+"("+gloss+")", root.Category, anchorStr))
	}
	return summaries, strings.Join(lines, "\n\n")
}

// ── The skill executor ────────────────────────────────────────────────────────

// runNeologism is the execute function for the neologism-builder skill.
// It streams the lexicon/anchor pre-fetch as Working events, calls Gemini,
// then splits the response into two separate ArtifactEvents (Practical + Poetic).
func runNeologism(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := parseNeologismRequest(execCtx.Message)
		log.Printf("[Skill:neologism] user=%q lang=%s concept=%q",
			execCtx.User.Name, req.langName, req.concept)

		// — Submitted ─────────────────────────────────────────────────────────
		if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
			return
		}

		// — Phase 1: anchorage pre-fetch ──────────────────────────────────────
		startMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf(
				"Searching %s lexicon and running Anchorage Protocol for: %q",
				req.langName, req.concept)))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, startMsg), nil) {
			return
		}

		summaries, contextBlock := anchorageContext(req.concept, req.lang)
		if len(summaries) > 0 {
			anchMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Roots and anchors found:\n"+strings.Join(summaries, "\n")))
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, anchMsg), nil) {
				return
			}
		}

		// — Phase 2: Gemini two-path generation ───────────────────────────────
		client, err := getGenAIClient() // shared with translate
		if err != nil {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart(fmt.Sprintf("Failed to connect to Vertex AI: %v", err)))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		thinkMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
			a2a.NewTextPart(fmt.Sprintf(
				"Building two-path neologism with Gemini (%s)...", translateModel())))
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, thinkMsg), nil) {
			return
		}

		userPrompt := buildNeologismPrompt(req, contextBlock)
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: neologismSkillMD}},
			},
		}

		// Accumulate the full response (both paths), then split.
		var full strings.Builder
		for chunk, streamErr := range client.Models.GenerateContentStream(
			ctx, translateModel(), genai.Text(userPrompt), config) {
			if streamErr != nil {
				failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
					a2a.NewTextPart(fmt.Sprintf("Generation error: %v", streamErr)))
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
				return
			}
			full.WriteString(chunk.Text())
		}

		response := strings.TrimSpace(full.String())
		if response == "" {
			failMsg := a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx,
				a2a.NewTextPart("Gemini returned an empty response."))
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, failMsg), nil)
			return
		}

		// — Split into Practical and Poetic artifacts ─────────────────────────
		practical, poetic := splitNeologismPaths(response)

		if practical != "" {
			practArtifact := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(practical))
			practArtifact.Artifact.Name = "Practical Path"
			if !yield(practArtifact, nil) {
				return
			}
		}
		if poetic != "" {
			poetArtifact := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(poetic))
			poetArtifact.Artifact.Name = "Poetic Path"
			if !yield(poetArtifact, nil) {
				return
			}
		}
		// If Gemini didn't use the delimiters, emit the full response as one artifact.
		if practical == "" && poetic == "" {
			single := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(response))
			single.Artifact.Name = "Neologism"
			if !yield(single, nil) {
				return
			}
		}

		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
	}
}

// splitNeologismPaths parses the delimited LLM response into the two path texts.
func splitNeologismPaths(response string) (practical, poetic string) {
	practIdx := strings.Index(response, practicalDelim)
	poetIdx := strings.Index(response, poeticDelim)

	if practIdx < 0 && poetIdx < 0 {
		return "", "" // no delimiters found — caller emits as single artifact
	}
	if practIdx >= 0 && poetIdx >= 0 {
		if practIdx < poetIdx {
			practical = strings.TrimSpace(response[practIdx+len(practicalDelim) : poetIdx])
			poetic = strings.TrimSpace(response[poetIdx+len(poeticDelim):])
		} else {
			poetic = strings.TrimSpace(response[poetIdx+len(poeticDelim) : practIdx])
			practical = strings.TrimSpace(response[practIdx+len(practicalDelim):])
		}
		return
	}
	if practIdx >= 0 {
		practical = strings.TrimSpace(response[practIdx+len(practicalDelim):])
	}
	if poetIdx >= 0 {
		poetic = strings.TrimSpace(response[poetIdx+len(poeticDelim):])
	}
	return
}

// buildNeologismPrompt assembles the LLM user-turn message.
func buildNeologismPrompt(req neologismRequest, contextBlock string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "NEOLOGISM REQUEST\n")
	fmt.Fprintf(&sb, "=================\n")
	fmt.Fprintf(&sb, "Concept to name: %q\n", req.concept)
	fmt.Fprintf(&sb, "Target language: %s\n\n", req.langName)

	if contextBlock != "" {
		fmt.Fprintf(&sb, "ELDAMO LEXICON & ANCHORAGE CONTEXT\n")
		fmt.Fprintf(&sb, "====================================\n")
		sb.WriteString(contextBlock)
		fmt.Fprintf(&sb, "\n\n")
	}

	fmt.Fprintf(&sb, "INSTRUCTIONS\n")
	fmt.Fprintf(&sb, "============\n")
	fmt.Fprintf(&sb, "Follow the two-path approach from your system instructions.\n")
	fmt.Fprintf(&sb, "Structure your response with EXACTLY these section headers:\n\n")
	fmt.Fprintf(&sb, "%s\n", practicalDelim)
	fmt.Fprintf(&sb, "(your Practical/Functional path analysis and proposed word)\n\n")
	fmt.Fprintf(&sb, "%s\n", poeticDelim)
	fmt.Fprintf(&sb, "(your Poetic/Metaphorical path analysis and proposed word)\n\n")
	fmt.Fprintf(&sb, "For each path:\n")
	fmt.Fprintf(&sb, "- Show the root → primitive → modern form derivation\n")
	fmt.Fprintf(&sb, "- Apply the phonotactic constraints for %s\n", req.langName)
	fmt.Fprintf(&sb, "- Apply the 100-point scoring matrix\n")
	fmt.Fprintf(&sb, "- Expand into noun, verb, and adjective forms if possible\n")
	return sb.String()
}
