package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// Zed provisions the Zed Editor MCP config.
// Transport: native SSE (no bridge needed).
// Uses "context_servers" key (not "mcpServers").
type Zed struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*Zed)(nil)

func newZed() *Zed {
	return &Zed{
		name: "Zed",
		slug: "zed",
		paths: map[string]string{
			"darwin":  "~/.config/zed/settings.json",
			"linux":   "~/.config/zed/settings.json",
			"windows": "%APPDATA%\\Zed\\settings.json",
		},
	}
}

func (z *Zed) Name() string     { return z.name }
func (z *Zed) Slug() string     { return z.slug }
func (z *Zed) NeedsBridge() bool { return false }

func (z *Zed) Detect() (string, bool) {
	path := configPathForPlatform(z.paths)
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

func (z *Zed) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	servers := getMap(data, "context_servers")
	if servers == nil {
		return false, nil
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (z *Zed) buildEntry(opts LinkOptions) map[string]any {
	return sseConfig("url", opts.GatewayURL)
}

func (z *Zed) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getOrCreateMap(data, "context_servers")
	entry := z.buildEntry(opts)

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
	data["context_servers"] = servers

	return writeJSONFile(configPath, data)
}

func (z *Zed) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getMap(data, "context_servers")
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
	data["context_servers"] = servers

	return writeJSONFile(configPath, data)
}
