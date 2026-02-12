package provisioner

// AnythingLLM provisions the AnythingLLM desktop MCP config.
// Transport: native SSE (no bridge needed).
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
		return map[string]any{
			"type": "sse",
			"url":  opts.GatewayURL,
		}
	}
	return c
}
