package provisioner

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// readTOMLFile reads a TOML file into a map.
func readTOMLFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := toml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parsing TOML: %w", err)
	}
	if data == nil {
		data = make(map[string]any)
	}
	return data, nil
}

// writeTOMLFile atomically writes a map as TOML.
func writeTOMLFile(path string, data map[string]any) error {
	out, err := toml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling TOML: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

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

// readOrCreateTOMLFile reads an existing TOML file or returns an empty map.
func readOrCreateTOMLFile(path string) (map[string]any, error) {
	if fileExists(path) {
		return readTOMLFile(path)
	}
	return make(map[string]any), nil
}

// formatTOML returns a TOML string for display purposes.
func formatTOML(data map[string]any) string {
	out, err := toml.Marshal(data)
	if err != nil {
		return ""
	}
	return string(out)
}
