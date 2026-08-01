package provisioner

// AnythingLLM provisions the AnythingLLM desktop MCP config.
// Transport: native streamable HTTP (no bridge needed); AnythingLLM's token
// is "streamable" ("streamable-http" is rejected, and omitting type means SSE).
// Config uses standard { "mcpServers": { "name": {...} } } structure.
type AnythingLLM struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*AnythingLLM)(nil)

func newAnythingLLM() *AnythingLLM {
	c := &AnythingLLM{}
	c.name = "AnythingLLM"
	c.slug = "anythingllm"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/Library/Application Support/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json",
		"windows": "%APPDATA%\\anythingllm-desktop\\storage\\plugins\\anythingllm_mcp_servers.json",
		"linux":   "~/.config/anythingllm-desktop/storage/plugins/anythingllm_mcp_servers.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = gatewayHTTPURLForOpts(opts)
		}
		return map[string]any{
			"type": "streamable",
			"url":  url,
		}
	}
	return c
}
