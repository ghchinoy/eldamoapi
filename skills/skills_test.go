package skills

// skills_test.go — unit tests for skill parsers and pure functions.
// Lives in package skills so it can access unexported struct fields
// (nameRequest.lang/gender/concepts, translateRequest.targetLang/sourceText).

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"log"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/ghchinoy/eldamoapi/index"
)

func msg(text string) *a2a.Message {
	return &a2a.Message{Parts: []*a2a.Part{a2a.NewTextPart(text)}}
}

// ── LLMClient test doubles ──────────────────────────────────────────────────

// emptyIndex is a LexiconSearcher stub returning no results, so
// RunTranslate/RunNeologism exercise the LLM path without needing real
// lexicon data.
type emptyIndex struct{}

func (emptyIndex) SearchKeyword(_, _, _, _ string) []*index.FlatWord { return nil }
func (emptyIndex) GetRootAnchors(_ string) []*index.FlatWord         { return nil }

// fakeLLMClient is a scripted LLMClient test double: it yields chunks/errors
// exactly as configured, so tests can assert that skill executors correctly
// consume the LLMClient interface (Deps.LLM) end-to-end without depending on
// any concrete backend SDK.
type fakeLLMClient struct {
	chunks []GenChunk
	err    error // yielded after all chunks, if non-nil

	// gotModel/gotSystem/gotPrompt capture the last call's arguments.
	gotModel  string
	gotSystem string
	gotPrompt string
}

func (f *fakeLLMClient) GenerateContentStream(_ context.Context, model, systemInstruction, prompt string) iter.Seq2[GenChunk, error] {
	f.gotModel = model
	f.gotSystem = systemInstruction
	f.gotPrompt = prompt
	return func(yield func(GenChunk, error) bool) {
		for _, c := range f.chunks {
			if !yield(c, nil) {
				return
			}
		}
		if f.err != nil {
			yield(GenChunk{}, f.err)
		}
	}
}

// collectEvents drains an iter.Seq2[a2a.Event, error], failing the test on
// any yielded error.
func collectEvents(t *testing.T, seq iter.Seq2[a2a.Event, error]) []a2a.Event {
	t.Helper()
	var events []a2a.Event
	for e, err := range seq {
		if err != nil {
			t.Fatalf("unexpected error event: %v", err)
		}
		events = append(events, e)
	}
	return events
}

// artifactTexts extracts the text content of every TaskArtifactUpdateEvent
// among events.
func artifactTexts(events []a2a.Event) []string {
	var out []string
	for _, e := range events {
		art, ok := e.(*a2a.TaskArtifactUpdateEvent)
		if !ok || art.Artifact == nil {
			continue
		}
		var b strings.Builder
		for _, p := range art.Artifact.Parts {
			b.WriteString(p.Text())
		}
		out = append(out, b.String())
	}
	return out
}

// ── TestParseNameRequest ──────────────────────────────────────────────────────

func TestParseNameRequest(t *testing.T) {
	t.Run("defaults: quenya masculine single concept", func(t *testing.T) {
		req := ParseNameRequest(msg("name star"))
		if req.lang != "q" {
			t.Errorf("lang: want q, got %s", req.lang)
		}
		if req.langName != "Quenya" {
			t.Errorf("langName: want Quenya, got %s", req.langName)
		}
		if req.gender != "masculine" {
			t.Errorf("gender: want masculine, got %s", req.gender)
		}
		if len(req.concepts) != 1 || req.concepts[0] != "star" {
			t.Errorf("concepts: want [star], got %v", req.concepts)
		}
	})

	t.Run("sindarin + feminine + two concepts", func(t *testing.T) {
		req := ParseNameRequest(msg("name ocean wisdom feminine sindarin"))
		if req.lang != "s" {
			t.Errorf("lang: want s, got %s", req.lang)
		}
		if req.gender != "feminine" {
			t.Errorf("gender: want feminine, got %s", req.gender)
		}
		if len(req.concepts) != 2 {
			t.Errorf("concepts: want 2, got %v", req.concepts)
		}
	})

	t.Run("caps at 3 concepts", func(t *testing.T) {
		req := ParseNameRequest(msg("name star silver moon fire dragon"))
		if len(req.concepts) != 3 {
			t.Errorf("expected 3 concepts (cap), got %d: %v", len(req.concepts), req.concepts)
		}
	})

	t.Run("strips 'name ' trigger prefix", func(t *testing.T) {
		req := ParseNameRequest(msg("name silver quenya"))
		for _, c := range req.concepts {
			if c == "name" {
				t.Error("'name' trigger word leaked into concepts")
			}
		}
	})

	t.Run("nil message gives sensible defaults", func(t *testing.T) {
		req := ParseNameRequest(nil)
		if req.lang != "q" {
			t.Errorf("nil: want default lang=q, got %s", req.lang)
		}
		if len(req.concepts) == 0 {
			t.Error("nil: want at least one default concept")
		}
	})

	t.Run("gender keyword aliases", func(t *testing.T) {
		for _, word := range []string{"feminine", "female", "woman", "maiden", "daughter", "mother"} {
			req := ParseNameRequest(msg("name star " + word))
			if req.gender != "feminine" {
				t.Errorf("%q: expected feminine, got %s", word, req.gender)
			}
		}
		for _, word := range []string{"masculine", "male", "man", "father", "son"} {
			req := ParseNameRequest(msg("name star " + word))
			if req.gender != "masculine" {
				t.Errorf("%q: expected masculine, got %s", word, req.gender)
			}
		}
	})
}

// ── TestParseTranslateRequest ─────────────────────────────────────────────────

func TestParseTranslateRequest(t *testing.T) {
	t.Run("default quenya target", func(t *testing.T) {
		req := ParseTranslateRequest(msg("translate hello"))
		if req.targetLang != "q" {
			t.Errorf("want lang=q, got %s", req.targetLang)
		}
		if req.targetName != "Quenya" {
			t.Errorf("want Quenya, got %s", req.targetName)
		}
	})

	t.Run("sindarin keyword detected", func(t *testing.T) {
		req := ParseTranslateRequest(msg("translate to sindarin: the grey havens"))
		if req.targetLang != "s" {
			t.Errorf("want lang=s, got %s", req.targetLang)
		}
		if req.sourceText != "the grey havens" {
			t.Errorf("sourceText: want %q, got %q", "the grey havens", req.sourceText)
		}
	})

	t.Run("connector 'to' stripped before language keyword", func(t *testing.T) {
		req := ParseTranslateRequest(msg("translate farewell my friend to quenya"))
		if strings.Contains(req.sourceText, "quenya") {
			t.Errorf("language keyword leaked into sourceText: %q", req.sourceText)
		}
		if strings.HasSuffix(strings.ToLower(req.sourceText), " to") {
			t.Errorf("connector 'to' leaked: %q", req.sourceText)
		}
		if req.sourceText != "farewell my friend" {
			t.Errorf("sourceText: want %q, got %q", "farewell my friend", req.sourceText)
		}
	})

	t.Run("quenya colon-style", func(t *testing.T) {
		req := ParseTranslateRequest(msg("translate to quenya: a star shines"))
		if req.targetLang != "q" {
			t.Errorf("want lang=q, got %s", req.targetLang)
		}
		if req.sourceText != "a star shines" {
			t.Errorf("sourceText: want %q, got %q", "a star shines", req.sourceText)
		}
	})

	t.Run("nil message returns defaults", func(t *testing.T) {
		req := ParseTranslateRequest(nil)
		if req.targetLang != "q" {
			t.Errorf("nil: want default q, got %s", req.targetLang)
		}
	})
}

// ── TestIsUsableWord ──────────────────────────────────────────────────────────

func TestIsUsableWord(t *testing.T) {
	cases := []struct {
		word *index.FlatWord
		want bool
	}{
		{&index.FlatWord{Word: "elen"}, true},
		{&index.FlatWord{Word: "mith"}, true},
		{&index.FlatWord{Word: "active participle"}, false},
		{&index.FlatWord{Word: "verb form"}, false},
		{&index.FlatWord{Word: ""}, false},
		{&index.FlatWord{Word: "naur"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.word.Word, func(t *testing.T) {
			if got := IsUsableWord(tc.word); got != tc.want {
				t.Errorf("IsUsableWord(%q) = %v, want %v", tc.word.Word, got, tc.want)
			}
		})
	}
}

// ── TestCompoundingRules ──────────────────────────────────────────────────────

func TestCompoundingRules(t *testing.T) {
	t.Run("basic concatenation", func(t *testing.T) {
		if got := JoinRoots("elen", "dur"); got != "elendur" {
			t.Errorf("JoinRoots(elen, dur) = %q, want elendur", got)
		}
	})
	t.Run("vowel elision: drop one trailing vowel", func(t *testing.T) {
		if got := JoinRoots("moria", "elen"); got != "morielen" {
			t.Errorf("JoinRoots(moria, elen) = %q, want morielen", got)
		}
	})
	t.Run("n+l → ll", func(t *testing.T) {
		if got := JoinRoots("elen", "lote"); got != "elellote" {
			t.Errorf("JoinRoots(elen, lote) = %q, want elellote", got)
		}
	})
	t.Run("r+l → ll", func(t *testing.T) {
		if got := JoinRoots("celebr", "lasse"); got != "celebllasse" {
			t.Errorf("JoinRoots(celebr, lasse) = %q, want celebllasse", got)
		}
	})
	t.Run("t+l → ld", func(t *testing.T) {
		if got := JoinRoots("arat", "lasse"); got != "araldasse" {
			t.Errorf("JoinRoots(arat, lasse) = %q, want araldasse", got)
		}
	})
	t.Run("empty first", func(t *testing.T) {
		if got := JoinRoots("", "elen"); got != "elen" {
			t.Errorf("JoinRoots('', elen) = %q, want elen", got)
		}
	})
	t.Run("empty second", func(t *testing.T) {
		if got := JoinRoots("elen", ""); got != "elen" {
			t.Errorf("JoinRoots(elen, '') = %q, want elen", got)
		}
	})
	t.Run("AppendSuffix consonant+consonant no elision", func(t *testing.T) {
		if got := AppendSuffix("celebr", "ndil"); got != "celebrndil" {
			t.Errorf("AppendSuffix(celebr, ndil) = %q, want celebrndil", got)
		}
	})
	t.Run("AppendSuffix vowel+vowel elision", func(t *testing.T) {
		if got := AppendSuffix("arda", "iel"); got != "ardiel" {
			t.Errorf("AppendSuffix(arda, iel) = %q, want ardiel", got)
		}
	})
	t.Run("Capitalize", func(t *testing.T) {
		cases := []struct{ in, want string }{
			{"elen", "Elen"}, {"", ""}, {"Elen", "Elen"}, {"élf", "Élf"},
		}
		for _, tc := range cases {
			if got := Capitalize(tc.in); got != tc.want {
				t.Errorf("Capitalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	})
}

// ── TestIsNameRequest ─────────────────────────────────────────────────────────

func TestIsNameRequest(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"name star silver quenya", true},
		{"name grey flame", true},
		{"ocean wisdom sindarin", true},
		{"quenya", true},
		{"sindarin", true},
		{"translate this phrase", false},
		{"Namarie", false},
		{"hello", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := IsNameRequest(msg(tc.text))
			if got != tc.want {
				t.Errorf("IsNameRequest(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
	t.Run("nil", func(t *testing.T) {
		if IsNameRequest(nil) {
			t.Error("IsNameRequest(nil) should be false")
		}
	})
}

// ── TestIsTranslateRequest ────────────────────────────────────────────────────

func TestIsTranslateRequest(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"translate farewell to quenya", true},
		{"translate to sindarin: the grey havens", true},
		{"Translate This Phrase", true},
		{"translation of namarie", false},
		{"name star silver quenya", false},
		{"Namarie", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := IsTranslateRequest(msg(tc.text))
			if got != tc.want {
				t.Errorf("IsTranslateRequest(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
	t.Run("nil", func(t *testing.T) {
		if IsTranslateRequest(nil) {
			t.Error("IsTranslateRequest(nil) should be false")
		}
	})
}

// ── TestConceptsFromText ──────────────────────────────────────────────────────

func TestConceptsFromText(t *testing.T) {
	t.Run("basic extraction", func(t *testing.T) {
		got := ConceptsFromText("a star shines on the hour")
		for _, c := range got {
			if len(c) < 3 {
				t.Errorf("short token %q should be filtered", c)
			}
		}
		found := false
		for _, c := range got {
			if c == "star" {
				found = true
			}
		}
		if !found {
			t.Errorf("'star' should be in concepts, got %v", got)
		}
	})
	t.Run("caps at 5", func(t *testing.T) {
		got := ConceptsFromText("star silver moon fire dragon warrior eagle throne")
		if len(got) > 5 {
			t.Errorf("expected cap at 5, got %d: %v", len(got), got)
		}
	})
	t.Run("deduplicates", func(t *testing.T) {
		got := ConceptsFromText("star star star fire fire")
		seen := map[string]int{}
		for _, c := range got {
			seen[c]++
			if seen[c] > 1 {
				t.Errorf("duplicate concept %q", c)
			}
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := ConceptsFromText(""); len(got) != 0 {
			t.Errorf("expected empty, got %v", got)
		}
	})
}

// ── TestRunTranslate ──────────────────────────────────────────────────────────

// TestRunTranslate verifies RunTranslate consumes Deps.LLM through the
// skills.LLMClient interface (not a concrete SDK type): the fake backend's
// streamed chunks should be assembled into the final translation artifact,
// and the model/system-instruction/prompt passed through unchanged.
func TestRunTranslate(t *testing.T) {
	fake := &fakeLLMClient{chunks: []GenChunk{
		{Text: "Elen "}, {Text: "síla "}, {Text: "lúmenn'."},
	}}
	deps := &Deps{
		Index:       emptyIndex{},
		LLM:         fake,
		ModelName:   "test-model",
		TranslateMD: "system instructions for translate",
	}
	execCtx := &a2asrv.ExecutorContext{
		User:    &a2asrv.User{Name: "test-user"},
		Message: msg("translate to quenya: a star shines"),
	}

	events := collectEvents(t, RunTranslate(context.Background(), execCtx, deps))

	texts := artifactTexts(events)
	if len(texts) != 1 {
		t.Fatalf("expected exactly one artifact, got %d: %v", len(texts), texts)
	}
	if want := "Elen síla lúmenn'."; texts[0] != want {
		t.Errorf("artifact text: want %q, got %q", want, texts[0])
	}
	if fake.gotModel != "test-model" {
		t.Errorf("model passed to LLM: want test-model, got %q", fake.gotModel)
	}
	if fake.gotSystem != deps.TranslateMD {
		t.Errorf("systemInstruction passed to LLM: want %q, got %q", deps.TranslateMD, fake.gotSystem)
	}
}

// TestRunTranslate_LLMError verifies a streaming error from Deps.LLM surfaces
// as a failed task status update rather than a panic or silent drop.
func TestRunTranslate_LLMError(t *testing.T) {
	fake := &fakeLLMClient{err: errors.New("backend unavailable")}
	deps := &Deps{Index: emptyIndex{}, LLM: fake, ModelName: "test-model"}
	execCtx := &a2asrv.ExecutorContext{
		User:    &a2asrv.User{Name: "test-user"},
		Message: msg("translate to quenya: hello"),
	}

	events := collectEvents(t, RunTranslate(context.Background(), execCtx, deps))

	var sawFailed bool
	for _, e := range events {
		if su, ok := e.(*a2a.TaskStatusUpdateEvent); ok && su.Status.State == a2a.TaskStateFailed {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("expected a TaskStateFailed status update on LLM error")
	}
}

// ── TestRunNeologism ──────────────────────────────────────────────────────────

// TestRunNeologism verifies RunNeologism consumes Deps.LLM through the
// skills.LLMClient interface and splits the practical/poetic paths from the
// assembled streamed response.
func TestRunNeologism(t *testing.T) {
	response := PracticalDelim + "\npractical answer" + "\n\n" + PoeticDelim + "\npoetic answer"
	fake := &fakeLLMClient{chunks: []GenChunk{{Text: response}}}
	deps := &Deps{
		Index:       emptyIndex{},
		LLM:         fake,
		ModelName:   "test-model",
		NeologismMD: "system instructions for neologism",
	}
	execCtx := &a2asrv.ExecutorContext{
		User:    &a2asrv.User{Name: "test-user"},
		Message: msg("neologism starlight"),
	}

	events := collectEvents(t, RunNeologism(context.Background(), execCtx, deps))

	texts := artifactTexts(events)
	if len(texts) != 2 {
		t.Fatalf("expected practical + poetic artifacts, got %d: %v", len(texts), texts)
	}
	if texts[0] != "practical answer" {
		t.Errorf("practical artifact: want %q, got %q", "practical answer", texts[0])
	}
	if texts[1] != "poetic answer" {
		t.Errorf("poetic artifact: want %q, got %q", "poetic answer", texts[1])
	}
	if fake.gotSystem != deps.NeologismMD {
		t.Errorf("systemInstruction passed to LLM: want %q, got %q", deps.NeologismMD, fake.gotSystem)
	}
}

// ── TestRunTranslate/RunNeologism usage logging (Phase 2) ───────────────────────

// captureLog temporarily redirects the standard logger to a buffer for the
// duration of fn, restoring the original writer afterward, and returns the
// captured output.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)
	fn()
	return buf.String()
}

// TestRunTranslate_LogsUsage verifies RunTranslate emits a [Usage] log line
// carrying the backend/model/token counts reported by the LLM backend's
// terminal usage chunk — this is what docs/model-evaluation.md's results
// table will eventually be populated from.
func TestRunTranslate_LogsUsage(t *testing.T) {
	fake := &fakeLLMClient{chunks: []GenChunk{
		{Text: "Elen síla."},
		{Usage: &Usage{Backend: "llama.cpp", Model: "test-model", PromptTokens: 42, CompletionTokens: 7, TotalTokens: 49}},
	}}
	deps := &Deps{Index: emptyIndex{}, LLM: fake, ModelName: "test-model", TranslateMD: "sys"}
	execCtx := &a2asrv.ExecutorContext{
		User:    &a2asrv.User{Name: "test-user"},
		Message: msg("translate to quenya: a star shines"),
	}

	logged := captureLog(t, func() {
		collectEvents(t, RunTranslate(context.Background(), execCtx, deps))
	})

	for _, want := range []string{
		"[Usage] skill=translate", `user="test-user"`, "status=ok",
		"backend=llama.cpp", "model=test-model",
		"prompt_tokens=42", "completion_tokens=7", "total_tokens=49",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log output missing %q; full log:\n%s", want, logged)
		}
	}
}

// TestRunNeologism_LogsUsageOnError verifies RunNeologism logs a [Usage]
// line with status=error when the backend fails mid-stream, even though no
// usage was ever reported.
func TestRunNeologism_LogsUsageOnError(t *testing.T) {
	fake := &fakeLLMClient{err: errors.New("backend unavailable")}
	deps := &Deps{Index: emptyIndex{}, LLM: fake, ModelName: "test-model", NeologismMD: "sys"}
	execCtx := &a2asrv.ExecutorContext{
		User:    &a2asrv.User{Name: "test-user"},
		Message: msg("neologism starlight"),
	}

	logged := captureLog(t, func() {
		collectEvents(t, RunNeologism(context.Background(), execCtx, deps))
	})

	for _, want := range []string{
		"[Usage] skill=neologism", `user="test-user"`, "status=error",
		"backend=?", "model=?", "err=backend unavailable",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log output missing %q; full log:\n%s", want, logged)
		}
	}
}
