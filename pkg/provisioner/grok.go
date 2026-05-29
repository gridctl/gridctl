package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// GrokBuild provisions the Grok Build (xAI `grok` CLI) MCP config.
// Transport: native streamable HTTP (no bridge needed).
// Uses TOML format with an "mcp_servers" table keyed by server name.
type GrokBuild struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*GrokBuild)(nil)

func newGrokBuild() *GrokBuild {
	return &GrokBuild{
		name: "Grok Build",
		slug: "grok",
		paths: map[string]string{
			"darwin":  "~/.grok/config.toml",
			"linux":   "~/.grok/config.toml",
			"windows": "%USERPROFILE%\\.grok\\config.toml",
		},
	}
}

func (g *GrokBuild) Name() string      { return g.name }
func (g *GrokBuild) Slug() string      { return g.slug }
func (g *GrokBuild) NeedsBridge() bool { return false }

func (g *GrokBuild) Detect() (string, bool) {
	path := configPathForPlatform(g.paths)
	if path == "" {
		return "", false
	}
	if fileExists(path) {
		return path, true
	}
	// Check if ~/.grok/ exists (CLI installed but no config yet).
	if dirExists(filepath.Dir(path)) {
		return path, true
	}
	return "", false
}

func (g *GrokBuild) buildEntry(opts LinkOptions) map[string]any {
	url := opts.GatewayURL
	if opts.Port > 0 {
		url = gatewayHTTPURLForOpts(opts)
	}
	return map[string]any{
		"url":     url,
		"type":    "http",
		"enabled": true,
	}
}

func (g *GrokBuild) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, err := readTOMLFile(configPath)
	if err != nil {
		return false, err
	}
	servers := getMap(data, "mcp_servers")
	if servers == nil {
		return false, nil
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (g *GrokBuild) Link(configPath string, opts LinkOptions) error {
	data, err := readOrCreateTOMLFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getOrCreateMap(data, "mcp_servers")
	entry := g.buildEntry(opts)

	existing, exists := servers[opts.ServerName]
	if exists && !opts.Force {
		existingMap, ok := toStringMap(existing)
		if ok && reflect.DeepEqual(normalizeMap(existingMap), normalizeMap(entry)) {
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
	data["mcp_servers"] = servers

	return writeTOMLFile(configPath, data)
}

func (g *GrokBuild) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, err := readTOMLFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getMap(data, "mcp_servers")
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
	data["mcp_servers"] = servers

	return writeTOMLFile(configPath, data)
}
