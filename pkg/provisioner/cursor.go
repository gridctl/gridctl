package provisioner

// Cursor provisions the Cursor editor MCP config.
// Transport: native streamable HTTP (no bridge needed). Cursor's remote
// entries are url-only; the client infers the transport from the endpoint.
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
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = gatewayHTTPURLForOpts(opts)
		}
		return urlConfig("url", url)
	}
	return c
}
