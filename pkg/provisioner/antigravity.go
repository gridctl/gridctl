package provisioner

import "path/filepath"

// Antigravity provisions the Google Antigravity IDE MCP config.
// Transport: native streamable HTTP (no bridge needed). Antigravity borrows
// Windsurf's `serverUrl` field name but speaks Streamable HTTP, so the entry
// points at the gateway's /mcp endpoint rather than /sse.
type Antigravity struct {
	mcpServersProvisioner
	legacyPaths map[string]string
}

var _ ClientProvisioner = (*Antigravity)(nil)

func newAntigravity() *Antigravity {
	c := &Antigravity{}
	c.name = "Antigravity"
	c.slug = "antigravity"
	c.bridge = false
	// Antigravity 2.0 shares one MCP config between the IDE and the CLI.
	c.paths = map[string]string{
		"darwin":  "~/.gemini/config/mcp_config.json",
		"linux":   "~/.gemini/config/mcp_config.json",
		"windows": "%USERPROFILE%\\.gemini\\config\\mcp_config.json",
	}
	// Pre-2.0 installs kept the IDE config under ~/.gemini/antigravity/.
	c.legacyPaths = map[string]string{
		"darwin":  "~/.gemini/antigravity/mcp_config.json",
		"linux":   "~/.gemini/antigravity/mcp_config.json",
		"windows": "%USERPROFILE%\\.gemini\\antigravity\\mcp_config.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = gatewayHTTPURLForOpts(opts)
		}
		return sseConfig("serverUrl", url)
	}
	return c
}

// Detect prefers the Antigravity 2.0 shared config path, falling back to the
// pre-2.0 IDE path so installs that predate the migration still link.
func (c *Antigravity) Detect() (string, bool) {
	if path, found := c.mcpServersProvisioner.Detect(); found {
		return path, true
	}
	legacy := configPathForPlatform(c.legacyPaths)
	if legacy == "" {
		return "", false
	}
	if fileExists(legacy) {
		return legacy, true
	}
	// Parent directory exists (app installed but no config yet).
	if dirExists(filepath.Dir(legacy)) {
		return legacy, true
	}
	return "", false
}
