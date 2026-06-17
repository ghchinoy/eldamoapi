package index

import (
	"testing"
)

var sampleJSONL = []byte(`{"id":"101","word":"elen","language":"q","speech":"noun","gloss":"star","category":"primary"}
{"id":"102","word":"elendil","language":"q","speech":"noun","gloss":"star-lover","category":"primary","refs":["101"]}
{"id":"103","word":"silme","language":"q","speech":"noun","gloss":"starlight","notes_cleaned":"light of the silmaril star","category":"neo"}
{"id":"104","word":"ala","language":"s","speech":"verb","gloss":"grow","category":"primary"}
`)

func TestTriePrefixSearch(t *testing.T) {
	idx, err := NewIndex(sampleJSONL)
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// 1. Test exact prefix search
	matches := idx.SearchPrefix("elen", "", "", "")
	if len(matches) != 2 {
		t.Errorf("Expected 2 prefix matches for 'elen', got %d", len(matches))
	}

	// 2. Test casing insensitivity
	matches = idx.SearchPrefix("ELEN", "", "", "")
	if len(matches) != 2 {
		t.Errorf("Expected casing insensitivity to match 2, got %d", len(matches))
	}

	// 3. Test filtering by language
	matches = idx.SearchPrefix("elen", "q", "", "")
	if len(matches) != 2 {
		t.Errorf("Expected 2 matching 'q' language, got %d", len(matches))
	}
	matches = idx.SearchPrefix("elen", "s", "", "")
	if len(matches) != 0 {
		t.Errorf("Expected 0 matching 's' language, got %d", len(matches))
	}

	// 4. Test exact single match
	matches = idx.SearchPrefix("elend", "", "", "")
	if len(matches) != 1 || matches[0].Word != "elendil" {
		t.Errorf("Expected matching 'elendil', got %v", matches)
	}
}

func TestInvertedKeywordSearch(t *testing.T) {
	idx, err := NewIndex(sampleJSONL)
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// 1. Match in gloss
	matches := idx.SearchKeyword("grow", "", "", "")
	if len(matches) != 1 || matches[0].Word != "ala" {
		t.Errorf("Expected 'grow' to match 'ala', got %v", matches)
	}

	// 2. Multi-word intersection
	matches = idx.SearchKeyword("star lover", "", "", "")
	if len(matches) != 1 || matches[0].Word != "elendil" {
		t.Errorf("Expected 'star lover' to intersect match 'elendil', got %v", matches)
	}

	// 3. Match in cleaned notes
	matches = idx.SearchKeyword("silmaril", "", "", "")
	if len(matches) != 1 || matches[0].Word != "silme" {
		t.Errorf("Expected 'silmaril' in notes to match 'silme', got %v", matches)
	}

	// 4. Filtering on category
	matches = idx.SearchKeyword("star", "", "", "neo")
	if len(matches) != 1 || matches[0].Word != "silme" {
		t.Errorf("Expected 'star' category 'neo' filter to match 'silme', got %v", matches)
	}
}

func TestDerivations(t *testing.T) {
	idx, err := NewIndex(sampleJSONL)
	if err != nil {
		t.Fatalf("Failed to build index: %v", err)
	}

	// 102 (elendil) refs 101 (elen). That means 101 is the ancestor of 102, and 102 is the descendant of 101.
	ancestors := idx.GetDerivations("102", "ancestors")
	if len(ancestors) != 1 || ancestors[0].ID != "101" {
		t.Errorf("Expected ancestor of '102' to be '101', got %v", ancestors)
	}

	descendants := idx.GetDerivations("101", "descendants")
	if len(descendants) != 1 || descendants[0].ID != "102" {
		t.Errorf("Expected descendant of '101' to be '102', got %v", descendants)
	}
}
