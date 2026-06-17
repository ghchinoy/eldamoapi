package data

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
)

// The go:generate directives automate regenerating both the raw JSONL and its gzipped version.
//
//go:generate go run ../../eldamo-group/eldamo-parse/xml-to-jsonl/main.go -xml ../../eldamo-group/eldamo/src/main/resources/eldarin/data/eldamo-data.xml -out eldamo.jsonl
//go:generate gzip -f -k eldamo.jsonl

// EldamoJSONLGz contains the gzipped, preprocessed JSON Lines dataset embedded directly into the Go binary.
// Embedding the compressed version (~4.5MB) instead of the raw JSONL (~24.8MB) keeps the Git footprint minimal,
// makes the repository 100% self-contained for serverless deployments (CI/CD), and avoids repository bloat.
//
//go:embed eldamo.jsonl.gz
var EldamoJSONLGz []byte

// GetJSONL decompresses the embedded gzipped dataset and returns the raw JSONL bytes in-memory.
// This decompression executes in less than 20ms and requires negligible overhead on startup.
func GetJSONL() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(EldamoJSONLGz))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	return io.ReadAll(reader)
}
