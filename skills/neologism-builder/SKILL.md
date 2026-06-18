---
name: neologism-builder
description: Use when creating new Elvish words. Provides a structured choice between "functional/practical" neologisms and "poetic/metaphorical" neologisms, guided by precise, diachronic phonological sound laws.
---

# Neologism Builder

This skill helps you construct linguistically authentic Elvish neologisms. When asked to name a new concept:

## 1. The Two-Path Approach

Always offer the user two distinct stylistic options:

### A. The Practical (Functional) Path
*   **Approach:** Deconstruct the concept into its literal functional components.
*   **Method:** Combine existing roots for the components + a standard agentive or instrumental suffix (like Quenya `-tar` for "maker").
*   **Result:** A word that feels like a tool name (e.g., *Yávetar* - "Fruit-maker").

### B. The Poetic (Metaphorical) Path
*   **Approach:** Map the concept to a metaphor or symbolic meaning.
*   **Method:** Use Tolkien's known naming conventions for high-concept names, looking for roots that imply the *outcome* or *essence* of the thing rather than its physical makeup. 
*   **Result:** A word that sounds like a title, personification, or mythic object (e.g., *Yaváno* - "Fruit-bringer").

---

## 2. Advanced Phonology & Sourcing Rules

To ensure utmost authenticity, apply these advanced structural protocols:

### A. The Principle of Acoustic Iconicity (The "Thud-vs-Spring" Rule)
*   **Rule:** Analyze the plosive/nasal weight of consonant clusters in potential roots.
*   **Application:** 
    *   *Heavy/Obstruction Clusters:* Clusters like `-mp-`, `-nt-`, or `-nk-` are phonesthemically weighted, denoting thudding, dragging, or physical difficulty (e.g., *lampa-* "hobble", *hampa-* "hop").
    *   *Light/Fluid Clusters:* Standard voiced plosives (`-b-`, `-d-`, `-g-`) or liquids (`-l-`, `-r-`) represent springiness, ease, or fluidity (e.g., *laba-* "hop").

### B. The Anchorage Protocol
*   **Rule:** Every proposed root must be anchored to attested names or proper nouns.
*   **Method:** Invoke the MCP tool `get_root_anchors` using the base root's unique page ID. Review character, place, or constellation names (e.g., Sador's nickname *Labadal* "Hopafoot") to justify the phonetic and grammatical behavior of the proposed root.

### C. The Resistance vs. Complexity Matrix
*   **Rule:** Audit the exact nature of the concept's difficulty:
    *   *External/Physical Resistance:* If the difficulty arises from physical stiffness or conditions, use roots derived from `√SRAG` (clumsy/stiff, e.g., *hraia*, *hranga*) or `√LAMP` (heavy footfall).
    *   *Internal/Abstract Complexity:* If the difficulty lies in the intellectual or procedural complexity of the task, use prefixes/adjectives derived from `√GUR` (cumbrous, arduous, e.g., *urda*, *ur(u)-*).

---

## 3. Linguistic Construction TL;DRs (from `eldamo-linguistics`)

To achieve true historical and phonotactic accuracy, apply these strict sound-engineering principles derived from Tolkien's historical grammar:

### A. Quenya Phonotactic Constraints
*   **Word-Final Rule:** Quenya *never* allows arbitrary consonants at the end of words. Word-final consonants are strictly restricted to the group: **`t, n, r, s, l`** (mnemonic: *TaN-RuSeL*). 
*   **Syllable Structure:** Quenya prefers open syllables (ending in a vowel) or simple coda structures. Ensure any newly constructed noun or verb respects this.

### B. Sindarin Compounding & Sandhi Synthesis
When joining words in Sindarin, you must handle consonant clashes and mutations:
1.  **Nasal Sandhi:** If the first element ends in a nasal `-n` (e.g., *aran* "king") and is followed by a consonant like `g-` (e.g., *gorn* "revered"), the nasal is dropped: `aran + gorn -> ara + gorn`.
2.  **Lenition (Soft Mutation):** The second element of a compound must undergo soft mutation (lenition) where phonologically required:
    *   `p, t, c` -> `b, d, g`
    *   `b, d, g` -> `v, dh, Ø` (vanishes or becomes a silent shift)
    *   `m, s` -> `v, h`
    *   *Result:* `ara + gorn -> Aragorn`.

### C. Diachronic sound laws & Vowel Shortening
*   **Polysyllabic Shortening (`p_v_length`):** Long vowels are shortened in final syllables of polysyllabic words.
    *   *Example:* Primitive `*arān` -> Sindarin `aran` (the final `-ā-` must shorten). Always adjust vowels in final syllables of compound neologisms accordingly.

---

## 4. Construction Workflow

1.  **Etymological Root Search & Historical Pathing:**
    *   Use `eldamo-remote_enquire_lexicon` to find core primitive roots (e.g., √YAB for "fruit", √KEM for "earth").
    *   Reconstruct step-by-step: `Root (e.g., √PAR) -> Primitive (e.g., *parmā) -> Quenya/Sindarin Child (e.g., parma/parf)`.
2.  **Word-Family Expansion:**
    *   Do not stop at a single noun. Proactively expand the neologism into its relevant word-family:
        *   **Noun:** (The thing/agent, e.g., *Yaváno*)
        *   **Verb:** (The action, e.g., *Yavanta-*)
        *   **Adjective:** (The quality, e.g., *Yavanya*)
3.  **Linguistic Verification:**
    *   Verify the Quenya final consonant filter, Sindarin mutations/sandhi, and final syllable vowel lengths.
4.  **The "Voice Test":**
    *   If available, generate a pronunciation using `render_elvish_audio` to see if it carries the right "weight" or tone for the concept.

---

## 5. Quick-Reference Suffix Guide

Use these highly attested, structurally sound suffixes to form the final neologisms:

| Language | Suffix | Function | Meaning / Example |
| :--- | :--- | :--- | :--- |
| **Quenya** | `-ma` | Instrumental | Tool/Device (e.g., *parma* "book") |
| **Quenya** | `-tar` / `-tur` | High Agent | Ruler/Master/Lord (e.g., *Valatar* "King-ruler") |
| **Quenya** | `-no` / `-në` | Simple Agent | Person/Bringer (e.g., *yaváno* "fruit-bringer") |
| **Sindarin** | `-weg` / `-deg` | Instrumental | Tool/Device (e.g., *gaunweg* "trap-maker") |
| **Sindarin** | `-or` / `-ron` | Masculine Agent | Person/Doer (e.g., *ortheron* "conqueror") |
| **Sindarin** | `-ril` | Feminine Agent | Person/Doer (e.g., *gloril* "golden lady") |

---

## 6. Concept Validity Tiers

Always classify your suggestions so the user understands their historical pedigree:
*   **Tier 1 (Late Conception - Preferred):** Based on J.R.R. Tolkien's 1950s-1960s writings (*The Lord of the Rings* and later papers).
*   **Tier 2 (Middle Conception):** Based on the 1930s writings (*The Etymologies*). Highly reliable and widely accepted in Neo-Elvish.
*   **Tier 3 (Early Conception):** Based on the 1910s-1920s writings (*The Book of Lost Tales*, Gnomish, Early Qenya). Use only as a fallback and mark with `⚠️ (Early Concept)`.

---

## 7. Documentation
Always record your construction logic (e.g., "I used the root X for Y, applied the sound shift Z, and added the suffix W for agentivity") so the user understands the linguistic pedigree of their new word.

---

## 8. Quantitative Evaluation (The 100-Point Scoring Matrix)

To maintain a perfect balance between rigid phonological science and poetic, aesthetic license, every proposed neologism must undergo a quantitative evaluation out of **100 points**. This prevents over-systematization and celebrates artistic expression within the linguistic framework.

### A. Phonotactic Rigor (30 Points — The "Hard Gate")
*   **What it measures:** Uncompromising adherence to the target language's structural and phonetic rules (e.g., Quenya's final consonant *TaN-RuSeL* restriction, Sindarin soft mutations, vowel shortening in final polysyllabic codas).
*   **Scoring:**
    *   *30 points:* Structurally and phonologically flawless.
    *   *0 points:* Phonologically invalid. **Disqualified.** (Poetic license cannot violate raw phonetic law).

### B. Morphological & Acoustic Resonance (25 Points)
*   **What it measures:** How naturally the word fits into the language's established vocabulary paradigms and verbal/adjectival families. Highlights *phonetic iconicity* (where the sound matches the sense, e.g., using the heavy nasal `-mp-` of `√LAMP` to convey a thudding hobbling gait).
*   **Scoring:**
    *   *20-25 points:* Matches surrounding vocabulary style and exhibits beautiful phonetic iconicity.
    *   *10-19 points:* Fits well structurally but lacks poetic or iconic resonance.
    *   *0-9 points:* Awkward compound or unusual fit.

### C. Historical Pedigree (25 Points)
*   **What it measures:** The chronological era and manuscript authority of the source root.
*   **Scoring:**
    *   *25 points:* **Tier 1** (Late Conception, 1950s-1970s).
    *   *15 points:* **Tier 2** (Middle Conception, 1930s *The Etymologies*).
    *   *5 points:* **Tier 3** (Early Conception, 1910s-1920s).

### D. Anchorage Bonus (20 Points)
*   **What it measures:** Direct or recursive proper-noun/place-name descendants via the `get_root_anchors` tool (e.g., Sador's nickname *Labadal* anchoring the root `√LOP` -> *laba-*).
*   **Scoring:**
    *   *+20 points:* Root possesses one or more direct/recursive proper noun or place-name anchors.
    *   *0 points:* No attested proper noun anchor exists. **This is NOT a disqualifier.** Poetic, structurally flawless words can still score a magnificent **80/100 (An Elite Grade)**.

---

### E. Grade Index
*   **90 - 100 (S):** Masterpiece of Elvish engineering; extremely authentic, phonetically evocative, and historically anchored.
*   **80 - 89 (A):** Elite Neologism; highly authentic, beautiful, and structural, though it may lack a direct historical proper-noun anchor.
*   **60 - 79 (B):** Standard Neo-Elvish; fully usable but relies on earlier/Middle conceptions or slightly awkward compounding.
*   **Below 60 (C/F):** Substandard; not recommended for active use.
