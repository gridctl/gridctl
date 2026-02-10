package provisioner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// readJSONFile reads a JSON or JSONC file into a map.
// Returns (data, hasComments, error). hasComments is true if comments or
// trailing commas were found and will be lost on write.
func readJSONFile(path string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}

	return parseJSON(raw)
}

// parseJSON parses JSON or JSONC bytes into a map.
func parseJSON(raw []byte) (map[string]any, bool, error) {
	// Try hujson to detect and handle comments/trailing commas
	ast, err := hujson.Parse(raw)
	if err != nil {
		return nil, false, fmt.Errorf("parsing JSON: %w", err)
	}

	// Check if comments exist before standardizing
	hasComments := bytes.Contains(raw, []byte("//")) || bytes.Contains(raw, []byte("/*"))

	// Standardize strips comments and trailing commas
	ast.Standardize()
	standardized := ast.Pack()

	var data map[string]any
	if err := json.Unmarshal(standardized, &data); err != nil {
		return nil, false, fmt.Errorf("unmarshaling JSON: %w", err)
	}

	return data, hasComments, nil
}

// writeJSONFile atomically writes a map as pretty-printed JSON.
func writeJSONFile(path string, data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	out = append(out, '\n')

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// formatJSON returns pretty-printed JSON for display purposes.
func formatJSON(data map[string]any) string {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(out)
}

// readOrCreateJSONFile reads an existing JSON file or returns an empty map if
// the file doesn't exist. Creates parent directories if needed.
func readOrCreateJSONFile(path string) (map[string]any, bool, error) {
	if fileExists(path) {
		return readJSONFile(path)
	}
	return make(map[string]any), false, nil
}
