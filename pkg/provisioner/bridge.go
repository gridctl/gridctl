package provisioner

import (
	"os/exec"
)

// NpxAvailable checks if npx is available in PATH.
// Exported as a variable to allow test overrides.
var NpxAvailable = func() bool {
	_, err := exec.LookPath("npx")
	return err == nil
}

// bridgeConfig returns the mcp-remote bridge configuration for stdio-only clients.
func bridgeConfig(gatewayURL string) map[string]any {
	return map[string]any{
		"command": "npx",
		"args":    []any{"-y", "mcp-remote", gatewayURL, "--allow-http"},
	}
}

// sseConfig returns the native SSE configuration for SSE-capable clients.
// The key name varies by client (serverUrl, url, etc.) so callers specify it.
func sseConfig(urlKey, gatewayURL string) map[string]any {
	return map[string]any{
		urlKey: gatewayURL,
	}
}
