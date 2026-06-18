---
name: neologism-builder
description: Use when creating new Elvish words. Provides a structured choice between "functional/practical" neologisms and "poetic/metaphorical" neologisms.
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

## 2. Construction Workflow

1.  **Etymological Root Search:**
    *   Use `eldamo-remote_enquire_lexicon` to find the core roots (e.g., "fruit," "grow," "earth").
2.  **Linguistic Verification:**
    *   Ensure the compounding rules respect the target language's phonology (e.g., Quenya vocalic vs. consonantal stems, Sindarin consonant mutations).
3.  **The "Voice Test":**
    *   If available, generate a pronunciation using `render_elvish_audio` to see if it carries the right "weight" or tone for the concept.

## 3. Documentation
Always record your construction logic (e.g., "I used the root X for Y and the suffix Z for agentivity") so the user understands the linguistic pedigree of their new word.
