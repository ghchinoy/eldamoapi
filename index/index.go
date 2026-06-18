package index

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// FlatWord represents a preprocessed Eldamo entry
type FlatWord struct {
	ID           string   `json:"id"`
	Word         string   `json:"word"`
	Language     string   `json:"language"`
	Speech       string   `json:"speech"`
	Gloss        string   `json:"gloss,omitempty"`
	NGloss       string   `json:"ngloss,omitempty"`
	Orthography  string   `json:"orthography,omitempty"`
	Stem         string   `json:"stem,omitempty"`
	Tengwar      string   `json:"tengwar,omitempty"`
	Category     string   `json:"category,omitempty"`
	Created      string   `json:"created,omitempty"`
	From         string   `json:"from,omitempty"`
	Mark         string   `json:"mark,omitempty"`
	NeoVersion   string   `json:"neo_version,omitempty"`
	Order        int      `json:"order,omitempty"`
	PhoneCol     int      `json:"phone_col,omitempty"`
	PhoneRow     int      `json:"phone_row,omitempty"`
	Rule         string   `json:"rule,omitempty"`
	Vetted       string   `json:"vetted,omitempty"`
	Notes        string   `json:"notes,omitempty"`
	NotesCleaned string   `json:"notes_cleaned,omitempty"`
	Refs         []string `json:"refs,omitempty"`
	Derivs       []string `json:"derivs,omitempty"`
}

// TrieNode represents a single node in the prefix tree
type TrieNode struct {
	Children map[rune]*TrieNode
	WordIDs  []string
}

// Insert inserts a word and its corresponding ID into the Trie
func (n *TrieNode) Insert(word string, wordID string) {
	curr := n
	normalized := strings.ToLower(word)
	for _, char := range normalized {
		if curr.Children == nil {
			curr.Children = make(map[rune]*TrieNode)
		}
		if _, ok := curr.Children[char]; !ok {
			curr.Children[char] = &TrieNode{}
		}
		curr = curr.Children[char]
	}
	curr.WordIDs = append(curr.WordIDs, wordID)
}

// CollectIDs recursively gathers all word IDs at and below this node
func (n *TrieNode) CollectIDs() []string {
	var ids []string
	ids = append(ids, n.WordIDs...)
	for _, child := range n.Children {
		ids = append(ids, child.CollectIDs()...)
	}
	return ids
}

// SearchPrefix searches the Trie for a prefix and returns matching word IDs
func (n *TrieNode) SearchPrefix(prefix string) []string {
	curr := n
	normalized := strings.ToLower(prefix)
	for _, char := range normalized {
		if curr.Children == nil {
			return nil
		}
		next, ok := curr.Children[char]
		if !ok {
			return nil
		}
		curr = next
	}
	return curr.CollectIDs()
}

// Index holds the fully-parsed lexicon and the in-memory indexes
type Index struct {
	Words        map[string]*FlatWord   // Fast lookup by word ID
	Trie         *TrieNode              // Prefix lookup for spellings
	InvertedMap  map[string][]string    // Keyword -> List of Word IDs
	DerivsSource map[string][]string    // Ancestor mapping: WordID -> List of source root IDs
	DerivsTarget map[string][]string    // Descendant mapping: WordID -> List of derived word IDs
}

var (
	nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)
	stopWords            = map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
		"of": true, "to": true, "in": true, "on": true, "at": true, "for": true,
		"with": true, "by": true, "from": true, "is": true, "it": true, "this": true,
	}
)

// tokenize normalizes and splits a string into search-friendly keywords
func tokenize(text string) []string {
	normalized := strings.ToLower(text)
	cleaned := nonAlphanumericRegex.ReplaceAllString(normalized, " ")
	words := strings.Fields(cleaned)
	var tokens []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) > 1 && !stopWords[w] {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// NewIndex reads and processes the preprocessed JSONL file
func NewIndex(jsonData []byte) (*Index, error) {
	idx := &Index{
		Words:        make(map[string]*FlatWord),
		Trie:         &TrieNode{},
		InvertedMap:  make(map[string][]string),
		DerivsSource: make(map[string][]string),
		DerivsTarget: make(map[string][]string),
	}

	reader := bufio.NewReader(bytes.NewReader(jsonData))
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var word FlatWord
		if err := json.Unmarshal(line, &word); err != nil {
			return nil, err
		}

		// 1. Map by ID
		idx.Words[word.ID] = &word

		// 2. Insert into Trie
		idx.Trie.Insert(word.Word, word.ID)

		// 3. Populate Derivations Maps
		// Eldamo flat schema captures refs and derivs. Let's record them.
		for _, refID := range word.Refs {
			idx.DerivsSource[word.ID] = append(idx.DerivsSource[word.ID], refID)
			idx.DerivsTarget[refID] = append(idx.DerivsTarget[refID], word.ID)
		}
		for _, derivID := range word.Derivs {
			idx.DerivsTarget[word.ID] = append(idx.DerivsTarget[word.ID], derivID)
			idx.DerivsSource[derivID] = append(idx.DerivsSource[derivID], word.ID)
		}

		// 4. Tokenize for Inverted Index
		// Index fields: word, gloss, ngloss, and cleaned notes
		tokenSource := word.Word + " " + word.Gloss + " " + word.NGloss + " " + word.NotesCleaned
		tokens := tokenize(tokenSource)

		// Deduplicate tokens per word to avoid duplicate reference overhead
		seenTokens := make(map[string]bool)
		for _, token := range tokens {
			if !seenTokens[token] {
				seenTokens[token] = true
				idx.InvertedMap[token] = append(idx.InvertedMap[token], word.ID)
			}
		}
	}

	return idx, nil
}

// SearchPrefix retrieves words whose spellings begin with the prefix, filtered optional constraints
func (idx *Index) SearchPrefix(prefix string, lang, speech, category string) []*FlatWord {
	wordIDs := idx.Trie.SearchPrefix(prefix)
	return idx.filterAndHydrate(wordIDs, lang, speech, category)
}

// SearchKeyword executes a full-text multi-word query across the inverted index
func (idx *Index) SearchKeyword(query string, lang, speech, category string) []*FlatWord {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	// Find the intersection of word IDs matching all query tokens
	var matchIDs []string
	first := true

	for _, token := range tokens {
		ids, found := idx.InvertedMap[token]
		if !found {
			// If one of our search keywords yields zero matches, the intersection is empty
			return nil
		}

		if first {
			matchIDs = make([]string, len(ids))
			copy(matchIDs, ids)
			first = false
		} else {
			matchIDs = intersect(matchIDs, ids)
		}

		if len(matchIDs) == 0 {
			break
		}
	}

	return idx.filterAndHydrate(matchIDs, lang, speech, category)
}

// GetWord retrieves a single word by its page-id
func (idx *Index) GetWord(id string) (*FlatWord, bool) {
	word, ok := idx.Words[id]
	return word, ok
}

// GetDerivations retrieves ancestors or descendants of a word
func (idx *Index) GetDerivations(id string, direction string) []*FlatWord {
	var targetIDs []string
	if strings.ToLower(direction) == "ancestors" {
		targetIDs = idx.DerivsSource[id]
	} else {
		targetIDs = idx.DerivsTarget[id]
	}

	var results []*FlatWord
	seen := make(map[string]bool)
	for _, targetID := range targetIDs {
		if seen[targetID] {
			continue
		}
		seen[targetID] = true
		if word, found := idx.Words[targetID]; found {
			results = append(results, word)
		}
	}
	return results
}

// GetRootAnchors recursively retrieves all descendants of a word/root and filters them for proper names or place names
func (idx *Index) GetRootAnchors(id string) []*FlatWord {
	var results []*FlatWord
	seen := make(map[string]bool)

	var traverse func(currID string)
	traverse = func(currID string) {
		targets := idx.DerivsTarget[currID]
		for _, tID := range targets {
			if seen[tID] {
				continue
			}
			seen[tID] = true
			if word, found := idx.Words[tID]; found {
				speechLower := strings.ToLower(word.Speech)
				if strings.Contains(speechLower, "name") {
					results = append(results, word)
				}
				traverse(tID)
			}
		}
	}

	traverse(id)
	return results
}

// filterAndHydrate filters a list of word IDs and converts them into FlatWord objects
func (idx *Index) filterAndHydrate(ids []string, lang, speech, category string) []*FlatWord {
	var results []*FlatWord
	seen := make(map[string]bool)

	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true

		word, found := idx.Words[id]
		if !found {
			continue
		}

		// Apply Filters
		if lang != "" && !strings.EqualFold(word.Language, lang) {
			continue
		}
		if speech != "" && !strings.EqualFold(word.Speech, speech) {
			continue
		}
		if category != "" && !strings.EqualFold(word.Category, category) {
			continue
		}

		results = append(results, word)
	}

	return results
}

// intersect returns the common elements between two string slices
func intersect(a, b []string) []string {
	m := make(map[string]bool)
	for _, item := range a {
		m[item] = true
	}

	var res []string
	for _, item := range b {
		if m[item] {
			res = append(res, item)
		}
	}
	return res
}
