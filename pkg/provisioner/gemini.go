package provisioner

// GeminiCLI provisions the Gemini CLI MCP config.
// Transport: native streamable HTTP (no bridge needed).
type GeminiCLI struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*GeminiCLI)(nil)

func newGeminiCLI() *GeminiCLI {
	c := &GeminiCLI{}
	c.name = "Gemini CLI"
	c.slug = "gemini"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/.gemini/settings.json",
		"linux":   "~/.gemini/settings.json",
		"windows": "%USERPROFILE%\\.gemini\\settings.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = GatewayHTTPURL(opts.Port)
		}
		return httpConfig(url, "streamable-http")
	}
	return c
}
