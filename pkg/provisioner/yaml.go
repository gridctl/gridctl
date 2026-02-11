package provisioner

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// readYAMLFile reads a YAML file into a map.
func readYAMLFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	if data == nil {
		data = make(map[string]any)
	}
	return data, nil
}

// writeYAMLFile atomically writes a map as YAML.
func writeYAMLFile(path string, data map[string]any) error {
	out, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
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

// readOrCreateYAMLFile reads an existing YAML file or returns an empty map.
func readOrCreateYAMLFile(path string) (map[string]any, error) {
	if fileExists(path) {
		return readYAMLFile(path)
	}
	return make(map[string]any), nil
}

// formatYAML returns a YAML string for display purposes.
func formatYAML(data map[string]any) string {
	out, err := yaml.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(out)
}
