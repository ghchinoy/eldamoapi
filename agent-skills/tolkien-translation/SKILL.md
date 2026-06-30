---
name: tolkien-translation
description: Translates English phrases into Tolkien's languages (Quenya, Sindarin, Adûnaic). Use when translating text, composing Elvish poetry, or validating grammar. Invokes eldamo-remote tools to find dictionary words, historical roots, and morphological details.
compatibility: Requires eldamo-remote MCP server tools to search and retrieve lexicon entries.
---

# Tolkien Elvish Translation & Composition Skill

Use this skill to translate English text into Quenya or Sindarin, compose grammatical Elvish phrases/poetry, or validate the structure of proposed Elvish writing. It leverages the connected `eldamo-remote` MCP server tools to search Paul Strack's lexicon Compilation with absolute accuracy.

---

## 📖 Translation Workflow

Follow this multi-step workflow to produce grammatically valid, Tolkien-accurate translations:

### Step 1: Lexicon Sourcing
Identify the core semantic concepts in the English phrase and use `eldamo-remote_enquire_lexicon` to find candidate Elvish words.
* Filter by language (`q` for Quenya, `s` for Sindarin) to prevent blending dialects.
* Look for primary words in Tolkien's later writings (marked with `category: "primary"`), prioritizing them over early Gnomish or Qenya lexicon words unless no later alternative exists.

### Step 2: Part-of-Speech & Inflection Analysis
Determine the required part-of-speech and grammatical inflections:
* **Quenya Case System:** Check if you need to apply noun cases:
  * **Genitive (`-o`):** "of", e.g., *Vardo* (of Varda).
  * **Possessive (`-va`):** "possessing", e.g., *omentielvo* (of our meeting).
  * **Allative (`-nna`):** "to/upon", e.g., *lúmenna* (upon the hour).
  * **Locative (`-ssë`):** "in/at", e.g., *sillumessë* (at this hour).
  * **Ablative (`-llo`):** "from", e.g., *sillumello* (from this hour).
* **Quenya Plurals:** Standard plural `-r` (for vowel endings) or `-i` (for consonant endings). Partitive plural `-lli`.

### Step 3: Consonant Mutation Handling (Critical for Sindarin)
If translating to **Sindarin**, you must apply consonant mutations on initial letters following prepositions, articles, or adjective-noun boundaries:
* **Soft Mutation (Lenition):** Triggered by singular article *i*, prepositions *na*, *di*, and adjective-noun compounds:
  * `p` -> `b` | `t` -> `d` | `c` -> `g`
  * `b` -> `v` | `d` -> `dh` | `g` -> *(disappears)*
  * `lh` -> `th` | `rh` -> `thr` | `m` -> `v` | `s` -> `h`
  * *Example:* *i* + *parf* (book) -> *i barf* (the book).
* **Nasal Mutation:** Triggered by plural article *in*:
  * `p` -> `ph` | `t` -> `th` | `c` -> `ch`
  * `b` -> `m` | `d` -> `n` | `g` -> `ng`
  * *Example:* *in* + *periand* (halflings) -> *i pheiriand* (the halflings).

---

## 📝 Examples of Translations

### Example 1: "A star shines on the hour of our meeting" (Quenya)
1. **Source Vocabulary:**
   * "star" -> *elen*
   * "shine" -> *síla* (present tense)
   * "hour" -> *lúmë*
   * "meeting" -> *omentië*
2. **Apply Inflections:**
   * "on the hour" (Allative) -> *lúme* + *-nna* -> *lúmenna*
   * "of our meeting" (Genitive + possessive inclusive suffix *-lva*) -> *omentie* + *-lva* + *-o* -> *omentielvo*
3. **Assemble:**
   > *Elen síla lúmenn’ omentielvo* (Tolkien's famous greeting).

### Example 2: "The Grey Havens" (Sindarin)
1. **Source Vocabulary:**
   * "grey" -> *mith*
   * "havens" -> *lond* (plural: *lynd*)
2. **Apply Mutation:**
   * Compound adjective prefix or standard plural lenition: *mith* + *lynd* -> *Mithlynd* (or *Mithlond*).
