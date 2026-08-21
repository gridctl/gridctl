package provisioner

// LMStudio provisions the LM Studio desktop app MCP config (MCP host since
// 0.3.17). Transport: native streamable HTTP (no bridge needed). LM Studio
// follows Cursor's mcp.json notation and its remote entries are url-only;
// the in-app editor strips unknown keys such as "type", so none is written.
// Servers load lazily since 0.4.0, so a written entry connects on first use.
type LMStudio struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*LMStudio)(nil)

func newLMStudio() *LMStudio {
	c := &LMStudio{}
	c.name = "LM Studio"
	c.slug = "lmstudio"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/.lmstudio/mcp.json",
		"windows": "%USERPROFILE%\\.lmstudio\\mcp.json",
		"linux":   "~/.lmstudio/mcp.json",
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

// PostLinkNotes returns the client-specific guidance shown after a
// successful link (CLI) and on the Connections detail pane.
func (c *LMStudio) PostLinkNotes() []string {
	return []string{
		"Requires LM Studio 0.3.17 or newer. If the app is open, check the Program tab; reopen the chat or restart the app if the gridctl server does not appear (servers load lazily since 0.4.0).",
		"LM Studio asks for confirmation on each tool call the first time.",
		"This links LM Studio's chat as an MCP host; it does not configure the OpenAI-compatible API on port 1234.",
		"Local models struggle with large tool lists: prefer 'gridctl link lmstudio --group <name>' or gateway code_mode.",
	}
}
