package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// ContinueDev provisions the Continue.dev extension MCP config.
// Transport: native SSE (no bridge needed).
// Config structure is different: experimental.mcpServers is an array of objects.
type ContinueDev struct {
	name  string
	slug  string
	paths map[string]string
}

var _ ClientProvisioner = (*ContinueDev)(nil)

func newContinueDev() *ContinueDev {
	return &ContinueDev{
		name: "Continue",
		slug: "continue",
		paths: map[string]string{
			"darwin":  "~/.continue/config.json",
			"windows": "%USERPROFILE%\\.continue\\config.json",
			"linux":   "~/.continue/config.json",
		},
	}
}

func (c *ContinueDev) Name() string      { return c.name }
func (c *ContinueDev) Slug() string      { return c.slug }
func (c *ContinueDev) NeedsBridge() bool  { return false }

func (c *ContinueDev) Detect() (string, bool) {
	path := configPathForPlatform(c.paths)
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

func (c *ContinueDev) buildEntry(opts LinkOptions) map[string]any {
	return map[string]any{
		"name": opts.ServerName,
		"transport": map[string]any{
			"type": "sse",
			"url":  opts.GatewayURL,
		},
	}
}

func (c *ContinueDev) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	servers := c.getMCPServers(data)
	for _, s := range servers {
		m, ok := s.(map[string]any)
		if ok && m["name"] == serverName {
			return true, nil
		}
	}
	return false, nil
}

func (c *ContinueDev) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	experimental := getOrCreateMap(data, "experimental")
	servers := c.getMCPServersFromMap(experimental)
	entry := c.buildEntry(opts)

	// Check for existing entry
	for i, s := range servers {
		m, ok := s.(map[string]any)
		if !ok || m["name"] != opts.ServerName {
			continue
		}
		if !opts.Force {
			if reflect.DeepEqual(m, entry) {
				return ErrAlreadyLinked
			}
			// Check if it looks like a gridctl entry
			transport, _ := m["transport"].(map[string]any)
			if transport == nil || transport["type"] != "sse" {
				return ErrConflict
			}
		}
		// Update in place
		if opts.DryRun {
			return nil
		}
		if _, err := createBackup(configPath); err != nil {
			return fmt.Errorf("creating backup: %w", err)
		}
		servers[i] = entry
		experimental["mcpServers"] = servers
		data["experimental"] = experimental
		return writeJSONFile(configPath, data)
	}

	// Not found, append
	if opts.DryRun {
		return nil
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	servers = append(servers, entry)
	experimental["mcpServers"] = servers
	data["experimental"] = experimental
	return writeJSONFile(configPath, data)
}

func (c *ContinueDev) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	experimental := getMap(data, "experimental")
	if experimental == nil {
		return ErrNotLinked
	}

	servers := c.getMCPServersFromMap(experimental)
	found := false
	var filtered []any
	for _, s := range servers {
		m, ok := s.(map[string]any)
		if ok && m["name"] == serverName {
			found = true
			continue
		}
		filtered = append(filtered, s)
	}

	if !found {
		return ErrNotLinked
	}

	if _, err := createBackup(configPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	experimental["mcpServers"] = filtered
	data["experimental"] = experimental
	return writeJSONFile(configPath, data)
}

func (c *ContinueDev) getMCPServers(data map[string]any) []any {
	experimental := getMap(data, "experimental")
	if experimental == nil {
		return nil
	}
	return c.getMCPServersFromMap(experimental)
}

func (c *ContinueDev) getMCPServersFromMap(experimental map[string]any) []any {
	v, ok := experimental["mcpServers"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	return arr
}
