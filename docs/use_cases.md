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

---

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

*Tip: Always use the "Auth Bypass" mode (if running locally) or a valid token to ensure your requests reach the synthesis engine.*
