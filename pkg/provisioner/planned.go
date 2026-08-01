package provisioner

import "strings"

// EntryPlanner is implemented by provisioners that can report the entry
// value Link would write, without touching any file. The wiring
// ownership manager hashes the planned value to detect staleness and to
// adopt identical pre-existing entries silently.
type EntryPlanner interface {
	PlannedEntry(opts LinkOptions) map[string]any
}

// PlannedEntry returns the entry value prov's Link would write for opts,
// or nil when the provisioner cannot plan (every registered client can).
func PlannedEntry(prov ClientProvisioner, opts LinkOptions) map[string]any {
	if p, ok := prov.(EntryPlanner); ok {
		return p.PlannedEntry(opts)
	}
	return nil
}

// PlannedEntry implements EntryPlanner for the shared mcpServers
// clients, including the per-client extra keys Link merges in.
func (p *mcpServersProvisioner) PlannedEntry(opts LinkOptions) map[string]any {
	entry := p.buildEntry(opts)
	for k, v := range p.extraKeys {
		if _, exists := entry[k]; !exists {
			entry[k] = v
		}
	}
	return entry
}

// PlannedEntry implementations for the clients with bespoke Link logic.
func (v *VSCode) PlannedEntry(opts LinkOptions) map[string]any      { return v.buildEntry(opts) }
func (c *ContinueDev) PlannedEntry(opts LinkOptions) map[string]any { return c.buildEntry(opts) }
func (z *Zed) PlannedEntry(opts LinkOptions) map[string]any         { return z.buildEntry(opts) }
func (o *OpenCode) PlannedEntry(opts LinkOptions) map[string]any    { return o.buildEntry(opts) }
func (g *Goose) PlannedEntry(opts LinkOptions) map[string]any       { return g.buildEntry(opts) }
func (g *GrokBuild) PlannedEntry(opts LinkOptions) map[string]any   { return g.buildEntry(opts) }

// LooksLikeLegacyLink reports whether an unrecorded entry is plausibly a
// gridctl link written before ownership recording existed. Per Article
// XVI this heuristic assists one-time migration only: it feeds the
// "likely a pre-lockfile link; adopt it" remediation hint and is never a
// steady-state ownership check. It recognizes every shape gridctl has
// written: localhost URLs under the known URL keys (flat or nested in a
// Continue-style transport object) and npx-launched mcp-remote bridges.
func LooksLikeLegacyLink(raw map[string]any, needsBridge bool) bool {
	if raw == nil {
		return false
	}
	if needsBridge {
		cmd, _ := raw["command"].(string)
		return cmd == "npx"
	}
	if transport, ok := raw["transport"].(map[string]any); ok {
		raw = transport
	}
	for _, key := range []string{"url", "serverUrl", "uri"} {
		if v, ok := raw[key].(string); ok && v != "" {
			return strings.Contains(v, "localhost") || strings.Contains(v, "127.0.0.1")
		}
	}
	return false
}
