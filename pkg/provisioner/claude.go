package provisioner

// ClaudeDesktop provisions the Claude Desktop MCP config.
// Transport: stdio only (requires mcp-remote bridge).
type ClaudeDesktop struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*ClaudeDesktop)(nil)

func newClaudeDesktop() *ClaudeDesktop {
	c := &ClaudeDesktop{}
	c.name = "Claude Desktop"
	c.slug = "claude"
	c.bridge = true
	c.paths = map[string]string{
		"darwin":  "~/Library/Application Support/Claude/claude_desktop_config.json",
		"windows": "%APPDATA%\\Claude\\claude_desktop_config.json",
		"linux":   "~/.config/Claude/claude_desktop_config.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = GatewayHTTPURL(opts.Port)
		}
		return bridgeConfig(url)
	}
	return c
}
