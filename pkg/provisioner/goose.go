package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// Goose provisions the Goose (Block) MCP config.
// Transport: SSE (no bridge needed).
// Uses YAML format with "extensions" dictionary.
type Goose struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*Goose)(nil)

func newGoose() *Goose {
	return &Goose{
		name: "Goose",
		slug: "goose",
		paths: map[string]string{
			"darwin":  "~/.config/goose/config.yaml",
			"linux":   "~/.config/goose/config.yaml",
			"windows": "%APPDATA%\\Block\\goose\\config\\config.yaml",
		},
	}
}

func (g *Goose) Name() string     { return g.name }
func (g *Goose) Slug() string     { return g.slug }
func (g *Goose) NeedsBridge() bool { return false }

func (g *Goose) Detect() (string, bool) {
	path := configPathForPlatform(g.paths)
	if path == "" {
		return "", false
	}
	if fileExists(path) {
		return path, true
	}
	if dirExists(filepath.Dir(path)) {
		return path, true
	}
	return "", false
}

func (g *Goose) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, err := readYAMLFile(configPath)
	if err != nil {
		return false, err
	}
	extensions := getMap(data, "extensions")
	if extensions == nil {
		return false, nil
	}
	_, exists := extensions[serverName]
	return exists, nil
}

func (g *Goose) buildEntry(opts LinkOptions) map[string]any {
	return map[string]any{
		"name":    opts.ServerName,
		"type":    "sse",
		"enabled": true,
		"timeout": 300,
		"uri":     opts.GatewayURL,
	}
}

func (g *Goose) Link(configPath string, opts LinkOptions) error {
	data, err := readOrCreateYAMLFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	extensions := getOrCreateMap(data, "extensions")
	entry := g.buildEntry(opts)

	existing, exists := extensions[opts.ServerName]
	if exists && !opts.Force {
		existingMap, ok := toStringMap(existing)
		if ok && reflect.DeepEqual(normalizeMap(existingMap), normalizeMap(entry)) {
			return ErrAlreadyLinked
		}
		if !gooseLooksLikeGridctlEntry(existingMap) {
			return ErrConflict
		}
	}

	if opts.DryRun {
		return nil
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	extensions[opts.ServerName] = entry
	data["extensions"] = extensions

	return writeYAMLFile(configPath, data)
}

func (g *Goose) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, err := readYAMLFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	extensions := getMap(data, "extensions")
	if extensions == nil {
		return ErrNotLinked
	}

	if _, exists := extensions[serverName]; !exists {
		return ErrNotLinked
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	delete(extensions, serverName)
	data["extensions"] = extensions

	return writeYAMLFile(configPath, data)
}

// gooseLooksLikeGridctlEntry checks if an existing Goose extension entry
// was likely created by gridctl (has a localhost/127.0.0.1 URI).
func gooseLooksLikeGridctlEntry(entry map[string]any) bool {
	if entry == nil {
		return false
	}
	if v, ok := entry["uri"].(string); ok && v != "" {
		return strings.Contains(v, "localhost") || strings.Contains(v, "127.0.0.1")
	}
	return false
}

// toStringMap converts a YAML-deserialized value to map[string]any.
// YAML unmarshals maps as map[string]any, but this handles the type assertion.
func toStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// normalizeMap converts all numeric values to a common type for comparison.
// YAML may unmarshal integers as int, while Go map literals use int,
// so we normalize to ensure DeepEqual works correctly.
func normalizeMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case int:
			result[k] = val
		case int64:
			result[k] = int(val)
		case float64:
			if val == float64(int(val)) {
				result[k] = int(val)
			} else {
				result[k] = val
			}
		default:
			result[k] = v
		}
	}
	return result
}
