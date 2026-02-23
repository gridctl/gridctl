package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// OpenCode provisions the OpenCode AI coding assistant MCP config.
// Transport: native HTTP (no bridge needed).
// Uses "mcp" key with { "type": "remote", "url": "..." } entries.
type OpenCode struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*OpenCode)(nil)

func newOpenCode() *OpenCode {
	return &OpenCode{
		name: "OpenCode",
		slug: "opencode",
		paths: map[string]string{
			"darwin":  "~/.config/opencode/opencode.json",
			"linux":   "~/.config/opencode/opencode.json",
			"windows": "%APPDATA%\\opencode\\opencode.json",
		},
	}
}

func (o *OpenCode) Name() string      { return o.name }
func (o *OpenCode) Slug() string      { return o.slug }
func (o *OpenCode) NeedsBridge() bool  { return false }

func (o *OpenCode) Detect() (string, bool) {
	path := configPathForPlatform(o.paths)
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

func (o *OpenCode) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	servers := getMap(data, "mcp")
	if servers == nil {
		return false, nil
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (o *OpenCode) buildEntry(opts LinkOptions) map[string]any {
	url := opts.GatewayURL
	if opts.Port > 0 {
		url = GatewayHTTPURL(opts.Port)
	}
	return httpConfig(url, "remote")
}

func (o *OpenCode) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getOrCreateMap(data, "mcp")
	entry := o.buildEntry(opts)

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
	data["mcp"] = servers

	return writeJSONFile(configPath, data)
}

func (o *OpenCode) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getMap(data, "mcp")
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
	data["mcp"] = servers

	return writeJSONFile(configPath, data)
}
