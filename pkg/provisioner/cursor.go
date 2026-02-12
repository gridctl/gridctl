package provisioner

// Cursor provisions the Cursor editor MCP config.
// Transport: native SSE (no bridge needed).
type Cursor struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*Cursor)(nil)

func newCursor() *Cursor {
	c := &Cursor{}
	c.name = "Cursor"
	c.slug = "cursor"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/.cursor/mcp.json",
		"windows": "%USERPROFILE%\\.cursor\\mcp.json",
		"linux":   "~/.cursor/mcp.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		return sseConfig("url", opts.GatewayURL)
	}
	return c
}
