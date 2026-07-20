package provisioner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// mcpServersProvisioner provides shared Link/Unlink/IsLinked logic for clients
// that use the standard { "mcpServers": { "name": {...} } } config structure.
// Clients embed this and override buildEntry and configPaths.
type mcpServersProvisioner struct {
	name       string
	slug       string
	bridge     bool
	paths      map[string]string
	buildEntry func(opts LinkOptions) map[string]any
	extraKeys  map[string]any // Extra keys to merge into new entries (e.g., Cline's "disabled", "alwaysAllow")
}

func (p *mcpServersProvisioner) Name() string      { return p.name }
func (p *mcpServersProvisioner) Slug() string      { return p.slug }
func (p *mcpServersProvisioner) NeedsBridge() bool  { return p.bridge }

func (p *mcpServersProvisioner) Detect() (string, bool) {
	path := configPathForPlatform(p.paths)
	if path == "" {
		return "", false
	}
	if fileExists(path) {
		return path, true
	}
	// Check if parent directory exists (app is installed but no config yet)
	if dirExists(filepath.Dir(path)) {
		return path, true
	}
	return "", false
}

func (p *mcpServersProvisioner) IsLinked(configPath string, serverName string) (bool, error) {
	if !fileExists(configPath) {
		return false, nil
	}
	data, _, err := readJSONFile(configPath)
	if err != nil {
		return false, err
	}
	servers := getMap(data, "mcpServers")
	if servers == nil {
		return false, nil
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (p *mcpServersProvisioner) Link(configPath string, opts LinkOptions) error {
	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getOrCreateMap(data, "mcpServers")
	entry := p.buildEntry(opts)

	// Merge extra keys
	for k, v := range p.extraKeys {
		if _, exists := entry[k]; !exists {
			entry[k] = v
		}
	}

	existing, exists := servers[opts.ServerName]
	if exists && !opts.Force {
		existingMap, ok := existing.(map[string]any)
		if ok && reflect.DeepEqual(existingMap, entry) {
			return ErrAlreadyLinked
		}
		if !looksLikeGridctlEntry(existingMap, opts.GatewayURL, p.bridge) {
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
	data["mcpServers"] = servers

	return writeJSONFile(configPath, data)
}

func (p *mcpServersProvisioner) Unlink(configPath string, serverName string) error {
	if !fileExists(configPath) {
		return ErrNotLinked
	}

	data, _, err := readJSONFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	servers := getMap(data, "mcpServers")
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
	data["mcpServers"] = servers

	return writeJSONFile(configPath, data)
}

// HasComments checks if a config file contains comments that would be lost on
// write. The comment syntax depends on the file format: TOML and YAML use '#'
// line comments, while JSON/JSONC uses '//' and '/*'. Returns false if the file
// doesn't exist.
func HasComments(configPath string) bool {
	if !fileExists(configPath) {
		return false
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	switch strings.ToLower(filepath.Ext(configPath)) {
	case ".toml", ".yaml", ".yml":
		// '#' line comments. Avoids false positives on '//' inside URLs.
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				return true
			}
		}
		return false
	default:
		return strings.Contains(string(raw), "//") || strings.Contains(string(raw), "/*")
	}
}

// DryRunDiff returns the before/after config for a dry run.
// Supports both JSON and YAML formats based on the provisioner type.
func DryRunDiff(configPath string, prov ClientProvisioner, opts LinkOptions) (before, after string, err error) {
	if isYAMLProvisioner(prov) {
		data, err := readOrCreateYAMLFile(configPath)
		if err != nil {
			return "", "", err
		}
		before = formatYAML(data)
		dataCopy := deepCopyMap(data)
		simulateLink(dataCopy, prov, opts)
		after = formatYAML(dataCopy)
		return before, after, nil
	}

	if isTOMLProvisioner(prov) {
		data, err := readOrCreateTOMLFile(configPath)
		if err != nil {
			return "", "", err
		}
		before = formatTOML(data)
		dataCopy := deepCopyMap(data)
		simulateLink(dataCopy, prov, opts)
		after = formatTOML(dataCopy)
		return before, after, nil
	}

	data, _, err := readOrCreateJSONFile(configPath)
	if err != nil {
		return "", "", err
	}
	before = formatJSON(data)

	dataCopy := deepCopyMap(data)
	simulateLink(dataCopy, prov, opts)
	after = formatJSON(dataCopy)
	return before, after, nil
}

// isYAMLProvisioner returns true if the provisioner uses YAML config format.
func isYAMLProvisioner(prov ClientProvisioner) bool {
	_, ok := prov.(*Goose)
	return ok
}

// isTOMLProvisioner returns true if the provisioner uses TOML config format.
func isTOMLProvisioner(prov ClientProvisioner) bool {
	_, ok := prov.(*GrokBuild)
	return ok
}

// simulateLink applies the link operation to a data map without writing to disk.
func simulateLink(data map[string]any, prov ClientProvisioner, opts LinkOptions) {
	switch p := prov.(type) {
	case *VSCode:
		servers := getOrCreateMap(data, "servers")
		servers[opts.ServerName] = p.buildEntry(opts)
		data["servers"] = servers
	case *ContinueDev:
		experimental := getOrCreateMap(data, "experimental")
		entry := p.buildEntry(opts)
		servers, _ := experimental["mcpServers"].([]any)
		servers = append(servers, entry)
		experimental["mcpServers"] = servers
		data["experimental"] = experimental
	case *Zed:
		servers := getOrCreateMap(data, "context_servers")
		servers[opts.ServerName] = p.buildEntry(opts)
		data["context_servers"] = servers
	case *OpenCode:
		servers := getOrCreateMap(data, "mcp")
		servers[opts.ServerName] = p.buildEntry(opts)
		data["mcp"] = servers
	case *Goose:
		extensions := getOrCreateMap(data, "extensions")
		extensions[opts.ServerName] = p.buildEntry(opts)
		data["extensions"] = extensions
	case *GrokBuild:
		servers := getOrCreateMap(data, "mcp_servers")
		servers[opts.ServerName] = p.buildEntry(opts)
		data["mcp_servers"] = servers
	default:
		if mp, ok := getProvisionerBase(prov); ok {
			servers := getOrCreateMap(data, "mcpServers")
			entry := mp.buildEntry(opts)
			for k, v := range mp.extraKeys {
				if _, exists := entry[k]; !exists {
					entry[k] = v
				}
			}
			servers[opts.ServerName] = entry
			data["mcpServers"] = servers
		}
	}
}

// getProvisionerBase extracts the mcpServersProvisioner base from known types.
func getProvisionerBase(prov ClientProvisioner) (*mcpServersProvisioner, bool) {
	switch p := prov.(type) {
	case *ClaudeDesktop:
		return &p.mcpServersProvisioner, true
	case *Cursor:
		return &p.mcpServersProvisioner, true
	case *Windsurf:
		return &p.mcpServersProvisioner, true
	case *Cline:
		return &p.mcpServersProvisioner, true
	case *RooCode:
		return &p.mcpServersProvisioner, true
	case *ClaudeCode:
		return &p.mcpServersProvisioner, true
	case *GeminiCLI:
		return &p.mcpServersProvisioner, true
	case *Antigravity:
		return &p.mcpServersProvisioner, true
	case *AnythingLLM:
		return &p.mcpServersProvisioner, true
	default:
		_ = p
		return nil, false
	}
}

// looksLikeGridctlEntry checks if an existing entry was likely created by gridctl.
func looksLikeGridctlEntry(entry map[string]any, gatewayURL string, needsBridge bool) bool {
	if entry == nil {
		return false
	}
	if needsBridge {
		cmd, _ := entry["command"].(string)
		return cmd == "npx"
	}
	for _, key := range []string{"url", "serverUrl", "uri"} {
		if v, ok := entry[key].(string); ok && v != "" {
			return strings.Contains(v, "localhost") || strings.Contains(v, "127.0.0.1")
		}
	}
	return false
}

// ListServers implements the shared read-only enumeration for standard
// { "mcpServers": { name: {...} } } clients.
func (p *mcpServersProvisioner) ListServers(configPath string) ([]ServerEntry, error) {
	return listJSONServers(configPath, "mcpServers")
}

// listJSONServers reads a JSON/JSONC config and returns the entries under
// containerKey.
func listJSONServers(configPath, containerKey string) ([]ServerEntry, error) {
	data, err := readJSONConfig(configPath)
	if err != nil || data == nil {
		return nil, err
	}
	return listMapEntries(getMap(data, containerKey)), nil
}

// readJSONConfig reads a JSON/JSONC config for enumeration, tolerating a
// UTF-8 BOM. Missing and empty files yield (nil, nil): Detect reports a
// client as installed when only its config directory exists, so an absent
// file is the normal "nothing configured yet" state, not an error. This is
// the one place that invariant lives.
func readJSONConfig(configPath string) (map[string]any, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	raw = stripBOM(raw)
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	data, _, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// listMapEntries converts a container map of server entries into a
// name-sorted ServerEntry slice, skipping values that are not objects.
func listMapEntries(container map[string]any) []ServerEntry {
	if len(container) == 0 {
		return nil
	}
	entries := make([]ServerEntry, 0, len(container))
	for name, v := range container {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		entries = append(entries, ServerEntry{Name: name, Raw: entry})
	}
	sortServerEntries(entries)
	return entries
}

func sortServerEntries(entries []ServerEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
}

// stripBOM removes a UTF-8 byte-order mark; several clients' configs acquire
// one from Windows editors and strict JSON parsers reject it.
func stripBOM(raw []byte) []byte {
	return bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
}

// Helper functions for navigating JSON maps.

func getMap(data map[string]any, key string) map[string]any {
	v, ok := data[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func getOrCreateMap(data map[string]any, key string) map[string]any {
	if m := getMap(data, key); m != nil {
		return m
	}
	m := make(map[string]any)
	data[key] = m
	return m
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func deepCopyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(val)
		case []any:
			cp := make([]any, len(val))
			copy(cp, val)
			dst[k] = cp
		default:
			dst[k] = v
		}
	}
	return dst
}
