package provisioner

import "path/filepath"

// ClaudeCode provisions the Claude Code CLI MCP config.
// Transport: native HTTP (no bridge needed).
type ClaudeCode struct{ mcpServersProvisioner }

var _ ClientProvisioner = (*ClaudeCode)(nil)

func newClaudeCode() *ClaudeCode {
	c := &ClaudeCode{}
	c.name = "Claude Code"
	c.slug = "claude-code"
	c.bridge = false
	c.paths = map[string]string{
		"darwin":  "~/.claude.json",
		"linux":   "~/.claude.json",
		"windows": "%USERPROFILE%\\.claude.json",
	}
	c.buildEntry = func(opts LinkOptions) map[string]any {
		url := opts.GatewayURL
		if opts.Port > 0 {
			url = GatewayHTTPURL(opts.Port)
		}
		return httpConfig(url, "http")
	}
	return c
}

// Detect overrides mcpServersProvisioner.Detect() because the config file
// ~/.claude.json has ~ as its parent, which always exists. Instead, check
// for the ~/.claude/ directory as the installation indicator.
func (c *ClaudeCode) Detect() (string, bool) {
	path := configPathForPlatform(c.paths)
	if path == "" {
		return "", false
	}
	if fileExists(path) {
		return path, true
	}
	// Check for ~/.claude/ directory (installation indicator)
	home := filepath.Dir(path)
	claudeDir := filepath.Join(home, ".claude")
	if dirExists(claudeDir) {
		return path, true
	}
	return "", false
}
