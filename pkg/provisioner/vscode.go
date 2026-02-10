package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// VSCode provisions the VS Code / GitHub Copilot MCP config.
// Transport: native SSE (no bridge needed).
// Uses "servers" key (not "mcpServers") and requires a "type" field.
type VSCode struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*VSCode)(nil)

func newVSCode() *VSCode {
	return &VSCode{
		name: "VS Code",
		slug: "vscode",
		paths: map[string]string{
			"darwin":  "~/.vscode/mcp.json",
			"windows": "%USERPROFILE%\\.vscode\\mcp.json",
			"linux":   "~/.vscode/mcp.json",
		},
	}
}

func (v *VSCode) Name() string      { return v.name }
func (v *VSCode) Slug() string      { return v.slug }
func (v *VSCode) NeedsBridge() bool  { return false }

func (v *VSCode) Detect() (string, bool) {
	path := configPathForPlatform(v.paths)
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

func (v *VSCode) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	servers := getMap(data, "servers")
	if servers == nil {
		return false, nil
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (v *VSCode) buildEntry(opts LinkOptions) map[string]any {
	return map[string]any{
		"type": "sse",
		"url":  opts.GatewayURL,
	}
}

func (v *VSCode) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getOrCreateMap(data, "servers")
	entry := v.buildEntry(opts)

	existing, exists := servers[opts.ServerName]
	if exists && !opts.Force {
		existingMap, ok := existing.(map[string]any)
		if ok && reflect.DeepEqual(existingMap, entry) {
			return ErrAlreadyLinked
		}
		if !looksLikeGridctlEntry(existingMap, opts.GatewayURL, false) {
			return ErrConflict
		}
	}

	if opts.DryRun {
		return nil
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	servers[opts.ServerName] = entry
	data["servers"] = servers

	return writeJSONFile(configPath, data)
}

func (v *VSCode) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getMap(data, "servers")
	if servers == nil {
		return ErrNotLinked
	}

	if _, exists := servers[serverName]; !exists {
		return ErrNotLinked
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	delete(servers, serverName)
	data["servers"] = servers

	return writeJSONFile(configPath, data)
}
