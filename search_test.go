package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ghchinoy/eldamoapi/data"
	"github.com/ghchinoy/eldamoapi/index"
)

func TestSearchForWords(t *testing.T) {
	rawBytes, err := data.GetJSONL()
	if err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}
	idx, err := index.NewIndex(rawBytes)
	if err != nil {
		t.Fatalf("Failed to init: %v", err)
	}

	targets := []string{"shaft", "spear", "arrow", "rod", "stem", "pole"}

	fmt.Println("--- START SEARCH RESULTS ---")
	for id, w := range idx.Words {
		match := false
		for _, target := range targets {
			if strings.Contains(strings.ToLower(w.Word), strings.ToLower(target)) ||
				strings.Contains(strings.ToLower(w.Gloss), strings.ToLower(target)) {
				match = true
				break
			}
		}
		if match {
			wBytes, _ := json.MarshalIndent(w, "", "  ")
			fmt.Printf("ID: %s | Word: %s (%s, %s)\n%s\n\n", id, w.Word, w.Language, w.Speech, string(wBytes))
		}
	}
	fmt.Println("--- END SEARCH RESULTS ---")
}
