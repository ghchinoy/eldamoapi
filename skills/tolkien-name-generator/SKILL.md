---
name: tolkien-name-generator
description: Generates grammatically correct, historically authentic Tolkien Elvish names for people, places, weapons, or stars. Use when creating fictional names, compounding roots, or translating modern names into Elvish. Invokes eldamo-remote tools to find matching vocabulary roots and grammatical suffixes.
compatibility: Requires eldamo-remote MCP server tools to search and validate lexicon roots.
---

# Tolkien Elvish Name Generation & Compounding Skill

Use this skill to generate authentic, linguistically sound names in Quenya or Sindarin for characters, places, weapons, houses, or celestial bodies. It utilizes the `eldamo-remote` MCP server tools to discover precise linguistic roots and apply historical Elvish compounding rules.

---

## 🏛️ Elvish Naming Suffixes

Elvish names are constructed by compounding nouns or adjectives with specific agental or gendered suffixes:

### 1. Quenya Name Endings
* **Masculine Suffixes:**
  * **`-(n)dil`:** "friend, lover of", e.g., *Elendil* (Star-lover), *Valandil* (Valar-friend).
  * **`-(n)dur`:** "servant of, devoted to", e.g., *Isildur* (Devoted to the Moon), *Eärendur* (Sea-servant).
  * **`-ion`:** "son of", e.g., *Finarfin* -> *Findecáno* (or patronymic endings).
  * **`-macar` / `-car`:** "swordsman, doer", e.g., *Telumehtar* (Warrior of the Sky).
* **Feminine Suffixes:**
  * **`-ië`:** Abstract feminine indicator, e.g., *Silmarië* (Shining-gem).
  * **`-wen`:** "maiden", e.g., *Earwen* (Sea-maiden).
  * **`-më`:** Feminine ending, e.g., *Altariel* (Galadriel's Quenya name: *Alatáriel*).

### 2. Sindarin Name Endings
* **Masculine Suffixes:**
  * **`-on`:** Great/noble masculine ending, e.g., *Haldir* -> *Turgon* (Lord of Stone).
  * **`-dir` / `-nir`:** "man", e.g., *Celeborn* (Silver-tall man), *Elrond* (Star-dome man).
  * **`-bor`:** "trusty man, warrior", e.g., *Denethor*.
* **Feminine Suffixes:**
  * **`-eth`:** Feminine noun ending, e.g., *Gilraen* -> *Elbereth* (Star-queen).
  * **`-iel` / `-ril` / `-riel`:** "daughter, crowned maiden, brilliance", e.g., *Lúthien* (or *Tinúviel* "Daughter of Twilight"), *Galadriel* (Maiden crowned with a radiant garland).

---

## ⚔️ Root Compounding & Phonetic Rules

When joining two Elvish words to create a name, you must resolve vowel collisions and consonant shifts:

### Rule 1: Vowel Elision (Vowel-Vowel Collision)
If the first word ends in a vowel and the second word starts with a vowel, one vowel is dropped (elided).
* *Example:* *Elen* (star) + *Alda* (tree) + agental `-dil` -> *Elen* + *Aldandil* -> *Elendil* (Vowel collision resolves to *Eldandil* or *Elendil*).
* *Example:* *Miri* (jewel) + *Elen* (star) -> *Mirielen* (Jeweled star).

### Rule 2: Consonant Assimilation
When joining consonants at the compound boundary, they must assimilate to make pronunciation smooth:
* `t` + `l` -> `ld`, e.g., *ut-lunte* -> *ulunde* (flood).
* `r` + `l` -> `ll`, e.g., *Ar-* (royal) + *Lassë* (leaf) -> *Arlassë* -> *Allassë*.
* `n` + `l` -> `ll`, e.g., *Elen* (star) + *Lótë* (flower) -> *Elellótë* (or *Elellótë* "Star-flower").

---

## 🧭 Step-by-Step Name Creation Example

**Goal: Generate a Sindarin name for a sword meaning "Grey-flame"**

1. **Find Noun & Adjective Roots:**
   * Invoke `eldamo-remote_enquire_lexicon query='grey' language='s'`.
     * Candidates: *mith* (grey, pale grey), *thuin* (grey). Let's select **`mith`**.
   * Invoke `eldamo-remote_enquire_lexicon query='flame' language='s'`.
     * Candidates: *lacho* (to leap as flame), *naur* (flame, fire). Let's select **`naur`**.
2. **Compound the Roots:**
   * Compound: *Mith* + *naur*.
3. **Apply Phonetic Consonant Mutations:**
   * In Sindarin compounds, the second element undergoes **Soft Mutation (Lenition)**:
   * The initial `n` of *naur* remains `n` (it is stable under Soft mutation unless nasalized, but double `n` can occur).
   * Let's assemble: *Mithnaur* -> **`Mithnaur`**.
   * Let's try with *lacho* (leap-flame): *Mith* + *lacho* (Soft mutation: `l` -> `l` stays, but boundary vowel elisions occur). Compound: **`Mithlach`** (Grey-leap-flame, which is a highly authentic Tolkien name format!).
