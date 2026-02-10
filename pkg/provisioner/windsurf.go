package provisioner

// Windsurf provisions the Windsurf (Codeium) editor MCP config.
// Transport: native SSE (no bridge needed).
type Windsurf struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*Windsurf)(nil)

func newWindsurf() *Windsurf {
	c := &Windsurf{}
	c.name = "Windsurf"
	c.slug = "windsurf"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/.codeium/windsurf/mcp_config.json",
		"windows": "%USERPROFILE%\\.codeium\\windsurf\\mcp_config.json",
		"linux":   "~/.codeium/windsurf/mcp_config.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		return sseConfig("serverUrl", opts.GatewayURL)
	}
	return c
}
