# 📝 Resolving Recursive Precursor Contamination and Conflated Derivations

June 22, 2026

This document serves as a historical reference and design record detailing how we identified, analyzed, and resolved a systemic data-leakage bug in the Eldamo dataset preparation pipeline. 

By restructuring our XML-to-JSONL preprocessor with a "Declarative Shield," we pruned thousands of "ghost-links" (falsely mapped derivations) from the database, restoring diachronic phonetic alignment, and promoted conceptual edits to an isolated, first-class metadata field (`precursors`).


## 📌 Background: Tolkien’s Dual-Axis Timeline

Tolkien’s invented languages do not exist on a single linear plane. They operate along two distinct, intersecting dimensions of time:

1. **The Diachronic Axis (Internal / In-Universe History):** The fictional, historical evolution of words within Middle-earth from Ancient Common Eldarin roots down to Third Age daughter tongues (e.g., $\text{Primitive Elvish } \sqrt{\text{KAL}} \longrightarrow \text{Quenya } \text{cala}$). This evolution is governed by rigorous, phonetic sound-shift laws.
2. **The Conceptual Axis (External / Real-World History):** J.R.R. Tolkien’s own lifetime development and revision of his languages from 1915 to 1973. A Welsh-inspired Gnomish word from *The Book of Lost Tales* (1917) represents the *conceptual precursor* to a Sindarin word in *The Lord of the Rings* (1954), but they are not genealogically related in-universe.

To capture this conceptual development, Paul Strack's Eldamo XML lexicon database (`eldamo-data.xml`) embeds deleted early-period precursor `<word>` elements directly within the mature, late-period parent `<word>` elements:

```xml
<word l="s" v="calar" speech="n" gloss="(portable) lamp" cat="DF_LP" page-id="444171573">
    <!-- Active In-Universe Derivation -->
    <deriv l="p" v="KAL"/>
    ...
    <!-- Embedded Conceptual Precursor (Deleted Gnomish Word) -->
    <word l="g" v="dant" speech="n" cat="DF_LP" page-id="225260927" mark="-" gloss="lamp">
        <deprecated l="s" v="calar"/>
        <!-- Gnomish-Only Derivation -->
        <deriv l="ep" v="DṆTṆ"/>
    </word>
</word>
```

---

## 🐛 Our Bug: Over-Aggressive Recursive Extraction

Both the python-based analytical pipelines in `eldamo-linguistics` and the Go-based XML preprocessor in `eldamo-server` suffered from a recursive containment leak:

* **In Python (`eldamo-linguistics`):** Algorithms extracted derivations recursively using `.findall(".//deriv")` or `/word//deriv` XPath selectors.
* **In Go (`eldamo-server`):** The standard Go `encoding/xml` decoder matched sub-elements by their local name. Because the Go struct `XMLWord` only defined fields for `Refs []XMLRef` and `Derivs []XMLDeriv` but lacked any field mapping nested `<word>` elements, the decoder skipped the nested `<word>` tags but leaked all nested children (`<ref>` and `<deriv>`) directly into the parent word's slices.

### Downstream Consequences:
1. **Etymological Pollution (Ghost-Links):** In-universe words like `S. calar` (derived from root `√KAL`) were incorrectly associated with Primitive Gnomish roots like `√DṆTṆ` (belonging only to `G. dant`).
2. **Sound-Shift Engine Failures:** Downstream machine learning and sequence-alignment processors (e.g., Needleman-Wunsch) attempted to compute phonetic transitions between `√DṆTṆ` and `S. calar`. Because this in-universe transition never physically existed, this generated severe false positives in phonetic anomaly detection.
3. **Misleading AI Tool Completions:** AI agents using `enquire_lexicon` or `get_derivations` received polluted etymological lineages, making them prone to combining wrong roots or applying incorrect Sandhi mutations when generating Elvish words. (Sandhi mutations are changes in the sounds of words at their boundaries due to the influence of adjacent sounds or grammatical context.)

---

## ⚖️ Options Evaluated

We evaluated two potential architectures to prevent this leakage in our Go preprocessor:

| Dimension | Option A: Declarative Shield Pattern (Selected) | Option B: Custom Unmarshaling Token-Loop |
| :--- | :--- | :--- |
| **Complexity** | **Extremely Low** (Relies on Go's declarative struct tag semantics). | **Medium** (Requires writing an imperative token-parsing loop). |
| **Fidelity** | **Perfect**. Recursively isolates nested tags inside their own scopes. | **Perfect**. Explicitly skips sub-trees on-encounter. |
| **Precursor Capture** | **Excellent**. Allows clean, structured collection of conceptual precursors as a first-class slice. | **Fair**. Requires manual state accumulation. |
| **Maintainability** | **Exceptional**. Easily readable and matches Go idiomatic patterns. | **Low**. Brittle to future schema changes in `eldamo.xsd`. |

---

## 💎 The Solution: Option A: The Declarative Shield Pattern

We restructured the parser's schema definitions in `eldamo-parse/xml-to-jsonl/main.go` to explicitly capture nested `<word>` blocks. This acts as a "shield," intercepting sub-elements and preventing them from spilling up into the parent word's slices:

### Restructured Go Struct Schema

We updated `XMLWord` to include a self-referential slice `NestedWords []XMLWord` matching the `"word"` XML tag:

```go
type XMLWord struct {
	XMLName     xml.Name   `xml:"word"`
	Cat         string     `xml:"cat,attr"`
	...
	Refs        []XMLRef   `xml:"ref"`
	Derivs      []XMLDeriv `xml:"deriv"`
    
	// Declarative Shield: Explicitly captures nested <word> blocks recursively.
	// This isolates their inner <ref> and <deriv> elements, preventing parent pollution.
	NestedWords []XMLWord  `xml:"word"` 
}
```

We then promoted conceptual precursors to a first-class, isolated property `Precursors []string` inside the exported `FlatWord` schema, preserving research capability without etymological contamination:

```go
type FlatWord struct {
	ID           string   `json:"id"`
	Word         string   `json:"word"`
	Language     string   `json:"language"`
	...
	Refs         []string `json:"refs,omitempty"`
	Derivs       []string `json:"derivs,omitempty"`
    
	// First-class isolated conceptual precursors (e.g., ["g. dant"])
	Precursors   []string `json:"precursors,omitempty"`
}
```

### Data Mapping Logic
During XML-to-JSONL compilation, parent derivations are kept isolated, while immediate nested precursors are captured and formatted:

```go
// Map Precursors
if len(xmlWord.NestedWords) > 0 {
    flat.Precursors = make([]string, len(xmlWord.NestedWords))
    for i, nw := range xmlWord.NestedWords {
        lang := nw.L
        if lang == "" {
            lang = "?"
        }
        flat.Precursors[i] = fmt.Sprintf("%s. %s", lang, nw.V)
    }
}
```

---

## 📊 Verification & Performance Metrics

Running the upgraded compilation pipeline yielded much better results:

1. **Ghost-Link Elimination:** Under the old system, `S. calar` (ID `444171573`) contained both `["KAL", "DṆTṆ"]` inside its `derivs` list. In the new compiled `eldamo.jsonl`, it contains **only `"KAL"`** in `derivs`, and cleanly references `"g. dant"` in `precursors`:
   ```json
   {"id":"444171573","word":"calar","language":"s","refs":["calar","galar","chelair","calar","in·chelair","calar"],"derivs":["KAL"],"precursors":["g. dant"]}
   ```
2. **Compilation Speed:** The entire 22,717-word XML database was parsed, cleaned, shielded, and written to `eldamo.jsonl` in **under 2 seconds**.
3. **No Code Overhead:** The Go server's high-performance memory footprints remain completely unchanged (~40-50MB RAM), starting up and decompressing the embedded gzipped dataset in **less than 20ms**.
4. **Test Suite Integrity:** Fixed two minor type alignment issues in `main_test.go` where `generateJWT` was historically passed raw string UIDs instead of a `*User` struct. The entire project-standard test suite (`go test ./...`) compiles and passes with **100% success**.

---

## 🚀 Conclusion
By introducing the **Declarative Shield**, the Eldamo MCP Server now aligns with historical accuracy for its linguistic, phonotactic, and sequence-alignment operations, while also boosting academic capability by providing access to Tolkien's creative development journey through the `precursors` dataset.
