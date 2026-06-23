# Eldamo MCP Server: Use Cases & Pronunciation Exercises

This document outlines practical ways to use the Eldamo MCP server, from linguistic exploration to auditory synthesis. Follow these exercises to master your control over Elvish neologisms and pronunciation.

## 1. Neologism Creation: Crafting "Fertilizer"
Elvish languages favor metaphorical description over clinical terminology. Instead of "soil-chemical-additive," we created terms based on function.

*   **Exercise:** Create a term for "Agriculture."
*   **Step 1:** Search for "field/growth" roots.
    *   *Action:* `@eldamo-remote enquire_lexicon(query="field")`
*   **Step 2:** Search for "activity" suffixes.
*   **Step 3:** Combine them.
    *   *Example:* *Ronya* (field) + *car-* (make) = *Ronyacar* (Agriculture).

## 2. Sentence Construction & Syntax
Once you have your neologism, test it within the context of Arda's history.

*   **Exercise:** Ask the agent to translate a request to tend to a specific location, like the base of Telperion.
*   **Syntax Tip:** Remember that Quenya cases (allative `-nna`, ablative `-llo`) provide directionality. Use them to place your actions:
    *   "I place the *yávetar* at the base of the tree."
    *   *Translation:* *Camin yávetar telmello alda-nna.*

## 3. Pronunciation Pipeline
Use the integrated TTS service to hear your creations.

*   **Exercise:** Refine your pronunciation for clarity.
    *   *Action:* `@eldamo-remote render_elvish_audio(text="[Your Sentence]", voice="emma", speed=0.8)`
*   **Refinement:** If the output is too fast, adjust the speed:
    *   *Action:* `@eldamo-remote render_elvish_audio(text="[Your Sentence]", voice="emma", speed=0.7)`


## 💡 Critical User Journeys (CUJs)

### CUJ: The Elvish Gardener
**Goal:** Create a specialized set of vocabulary for gardening in Valinor.
1.  **Enquire:** Search roots for "soil," "water," "growth," and "sunlight."
2.  **Neologize:** Propose names for "Gardener," "Fertilizer," and "Irrigation."
3.  **Synthesize:** Generate pronunciation for a list of these gardening tools.
4.  **Save:** Note your successful neologisms in a personal project documentation file.

### CUJ: The Poet's Assistant
**Goal:** Translate a modern English phrase into a poetic Elvish structure.
1.  **Analyze:** Break down the modern phrase into core concepts.
2.  **Map:** Use `enquire_lexicon` to map these to high-register (primary/neo) Elvish words.
3.  **Compose:** Ask the agent to construct a grammatical phrase.
4.  **Voice:** Use the TTS tool to hear the cadence of the finished poetry.

---

## 🧪 Appendix: Advanced Test Cases for Linguists & Maintainers

These "ground truth" test cases allow Tolkien linguists and lexicon database maintainers to verify that the Eldamo MCP Server is operating with perfect historical and structural fidelity (specifically validating that diachronic evolution is separated from conceptual precursors).

### Test Case 1: The Diachronic Boundary Test (Pruning Ghost-Links)
*   **Goal:** Verify that deleted early-period Gnomish/Early Qenya derivations do NOT contaminate late-period words.
*   **Query:** `get_derivations(id="444171573")` (for `S. calar` "lamp")
*   **Expected Behavior:** 
    *   The returned in-universe derivation chain must strictly map back to root `KAL`.
    *   The Gnomish precursor root `DṆTṆ` (which belonged only to the deleted 1917 Gnomish word `G. dant`) must **not** appear in the active derivations.
*   **Linguistic Significance:** Validates that the preprocessor's **Declarative Shield** is successfully preventing recursive XML-leakage.

### Test Case 2: The Conceptual Precursor Retrieval Test
*   **Goal:** Verify that a scholar can still trace Tolkien's external creative edits across different decades.
*   **Query:** `get_word_details(id="207957919")` (for `S. calardan` "lampwright")
*   **Expected Behavior:** 
    *   The returned details must contain an isolated `precursors` field listing `"n. calardan"` (representing its 1930s Noldorin draft equivalent).
*   **Linguistic Significance:** Confirms that external authorial revisions are preserved as isolated metadata, allowing rich exploration without corrupting active sound-shift modeling.

### Test Case 3: Recursive Root Anchor Resolution
*   **Goal:** Validate that proper names recursively derived from a highly productive root are traversed correctly.
*   **Query:** `get_root_anchors(id="2071154627")` (for root `LIK` "glide, slip, slide")
*   **Expected Behavior:**
    *   Returns recursively anchored proper names such as *Sirion* (the Great River of Beleriand) or *Siril* (the river of Númenor).
*   **Linguistic Significance:** Asserts the accuracy of the in-memory inverted etymological relationship graph.

### Test Case 4: Strict Dialect and Category Gating (Preventing Dialect Leakage)
*   **Goal:** Find all active nouns starting with a specific spelling, strictly isolated to primary or neo-languages to avoid historic phonetic bleeding.
*   **Query:** `enquire_lexicon(query="calma", language="q", speech="noun", category="primary")`
*   **Expected Behavior:**
    *   Returns Quenya `calma` ("lamp") or `calmatan` ("lampwright") while completely filtering out any Gnomish or Noldorin homophones.
*   **Linguistic Significance:** Validates multi-dimensional index constraint enforcement.

---

*Tip: Always use the "Auth Bypass" mode (if running locally) or a valid token to ensure your requests reach the synthesis engine.*
