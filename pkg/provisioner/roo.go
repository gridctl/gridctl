package provisioner

// RooCode provisions the Roo Code VS Code extension MCP config.
// Transport: native streamable HTTP (no bridge needed). Roo's schema requires
// an explicit "type" on url entries ("streamable-http" or legacy "sse"); the
// old "transportType" key is no longer in its schema.
// Adds Roo-specific fields: "disabled", "alwaysAllow".
type RooCode struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*RooCode)(nil)

func newRooCode() *RooCode {
	c := &RooCode{}
	c.name = "Roo Code"
	c.slug = "roo"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/Library/Application Support/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json",
		"windows": "%APPDATA%\\Code\\User\\globalStorage\\rooveterinaryinc.roo-cline\\settings\\mcp_settings.json",
		"linux":   "~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json",
	}
	c.extraKeys = map[string]any{
		"disabled":    false,
		"alwaysAllow": []any{},
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = gatewayHTTPURLForOpts(opts)
		}
		return map[string]any{
			"type": "streamable-http",
			"url":  url,
		}
	}
	return c
}
