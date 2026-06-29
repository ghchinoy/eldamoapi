package skills

// skills_test.go — unit tests for skill parsers and pure functions.
// Lives in package skills so it can access unexported struct fields
// (nameRequest.lang/gender/concepts, translateRequest.targetLang/sourceText).

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/ghchinoy/eldamoapi/index"
)

func msg(text string) *a2a.Message {
	return &a2a.Message{Parts: []*a2a.Part{a2a.NewTextPart(text)}}
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
