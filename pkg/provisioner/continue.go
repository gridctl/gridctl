package provisioner

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// ContinueDev provisions the Continue.dev extension MCP config.
// Transport: native streamable HTTP (no bridge needed); Continue's token is
// "streamable-http". Config structure is different: the servers live in an
// array under experimental, keyed by continueMCPKey.
//
// That key was previously written as "mcpServers", which Continue does not
// read: its config.json schema defines experimental.modelContextProtocolServers,
// and because the schema does not restrict additional properties the wrong key
// was accepted and silently ignored — so linking reported success while the
// gateway never appeared. The legacy key is still read so entries written
// before the correction can be found and cleaned up.
//
// Note: Continue also supports config.yaml with a top-level mcpServers list.
// config.json remains in the current schema, so this provisioner stays on it
// until it grows a YAML target.
const (
	// continueMCPKey is the key Continue's config.json schema defines.
	continueMCPKey = "modelContextProtocolServers"
	// continueLegacyMCPKey is what gridctl wrote before the correction.
	// Read-only: entries found here are migrated or removed, never added.
	continueLegacyMCPKey = "mcpServers"
)

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
func (c *ContinueDev) NeedsBridge() bool { return false }

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
	url := opts.GatewayURL
	if opts.Port > 0 {
		url = gatewayHTTPURLForOpts(opts)
	}
	return map[string]any{
		"name": opts.ServerName,
		"transport": map[string]any{
			"type": "streamable-http",
			"url":  url,
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
		if !opts.Force && !opts.OwnershipResolved {
			// Identical content is only "already linked" when it also sits
			// under the key Continue reads. An entry left under the legacy
			// key is inert, so short-circuiting here would leave the user
			// permanently unlinked with no way to notice.
			_, underLegacyKey := experimental[continueLegacyMCPKey]
			if reflect.DeepEqual(m, entry) && !underLegacyKey {
				return ErrAlreadyLinked
			}
			// Check if it looks like a gridctl entry; legacy links wrote
			// "sse", current ones "streamable-http".
			transport, _ := m["transport"].(map[string]any)
			if transport == nil || (transport["type"] != "sse" && transport["type"] != "streamable-http") {
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
		setContinueServers(experimental, servers)
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
	setContinueServers(experimental, servers)
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

	setContinueServers(experimental, filtered)
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
	// The legacy key is consulted so a link written before the key was
	// corrected is still discoverable, which is what lets unlink clean it up.
	for _, key := range []string{continueMCPKey, continueLegacyMCPKey} {
		if arr, ok := experimental[key].([]any); ok {
			return arr
		}
	}
	return nil
}

// setContinueServers writes the server list under the correct key and drops
// the legacy one. An empty list removes both keys rather than leaving them
// behind: writing an empty slice marshals to null, and a stale null is what
// unlink used to leave in the file.
func setContinueServers(experimental map[string]any, servers []any) {
	delete(experimental, continueLegacyMCPKey)
	if len(servers) == 0 {
		delete(experimental, continueMCPKey)
		return
	}
	experimental[continueMCPKey] = servers
}

// ListServers enumerates Continue's experimental MCP ARRAY, the one
// registered client that stores a list of objects (keyed by a "name" field)
// rather than a map.
func (c *ContinueDev) ListServers(configPath string) ([]ServerEntry, error) {
	data, err := readJSONConfig(configPath)
	if err != nil || data == nil {
		return nil, err
	}
	experimental := getMap(data, "experimental")
	if experimental == nil {
		return nil, nil
	}
	// Same both-keys read as IsLinked/Unlink: a listing that missed the
	// key the writer uses is how unlink reports "no entry" for a server it
	// just wrote.
	list := c.getMCPServersFromMap(experimental)
	entries := make([]ServerEntry, 0, len(list))
	for _, v := range list {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		entries = append(entries, ServerEntry{Name: name, Raw: entry})
	}
	sortServerEntries(entries)
	return entries, nil
}
