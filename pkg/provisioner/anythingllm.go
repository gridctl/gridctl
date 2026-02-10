package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// AnythingLLM provisions the AnythingLLM desktop MCP config.
// Transport: stdio only (requires mcp-remote bridge).
// Config is a flat JSON map (no "mcpServers" wrapper).
type AnythingLLM struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*AnythingLLM)(nil)

func newAnythingLLM() *AnythingLLM {
	return &AnythingLLM{
		name: "AnythingLLM",
		slug: "anythingllm",
		paths: map[string]string{
			"darwin":  "~/Library/Application Support/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json",
			"windows": "%APPDATA%\\anythingllm-desktop\\storage\\plugins\\anythingllm_mcp_servers.json",
			"linux":   "~/.config/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json",
		},
	}
}

func (a *AnythingLLM) Name() string      { return a.name }
func (a *AnythingLLM) Slug() string      { return a.slug }
func (a *AnythingLLM) NeedsBridge() bool  { return true }

func (a *AnythingLLM) Detect() (string, bool) {
	path := configPathForPlatform(a.paths)
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

func (a *AnythingLLM) buildEntry(opts LinkOptions) map[string]any {
	return bridgeConfig(opts.GatewayURL)
}

func (a *AnythingLLM) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	_, exists := data[serverName]
	return exists, nil
}

func (a *AnythingLLM) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	entry := a.buildEntry(opts)

	existing, exists := data[opts.ServerName]
	if exists && !opts.Force {
		existingMap, ok := existing.(map[string]any)
		if ok && reflect.DeepEqual(existingMap, entry) {
			return ErrAlreadyLinked
		}
		if !looksLikeGridctlEntry(existingMap, opts.GatewayURL, true) {
			return ErrConflict
		}
	}

	if opts.DryRun {
		return nil
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	data[opts.ServerName] = entry
	return writeJSONFile(configPath, data)
}

func (a *AnythingLLM) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	if _, exists := data[serverName]; !exists {
		return ErrNotLinked
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	delete(data, serverName)
	return writeJSONFile(configPath, data)
}
