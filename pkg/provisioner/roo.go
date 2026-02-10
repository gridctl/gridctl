package provisioner

// RooCode provisions the Roo Code VS Code extension MCP config.
// Transport: native SSE and Streamable HTTP (no bridge needed).
// Adds Roo-specific fields: "transportType", "disabled", "alwaysAllow".
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
		return map[string]any{
			"url":           opts.GatewayURL,
			"transportType": "sse",
		}
	}
	return c
}
