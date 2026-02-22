package provisioner

// Cline provisions the Cline VS Code extension MCP config.
// Transport: stdio only (requires mcp-remote bridge).
// Adds Cline-specific fields: "disabled" and "alwaysAllow".
type Cline struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*Cline)(nil)

func newCline() *Cline {
	c := &Cline{}
	c.name = "Cline"
	c.slug = "cline"
	c.bridge = true
	c.paths = map[string]string{
		"darwin":  "~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json",
		"windows": "%APPDATA%\\Code\\User\\globalStorage\\saoudrizwan.claude-dev\\settings\\cline_mcp_settings.json",
		"linux":   "~/.config/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json",
	}
	c.extraKeys = map[string]any{
		"disabled":    false,
		"alwaysAllow": []any{},
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
