package provisioner

import "testing"

func TestAppendClientParam(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		clientID string
		want     string
	}{
		{"empty client id is a no-op", "http://localhost:8180/sse", "", "http://localhost:8180/sse"},
		{"appends to bare url", "http://localhost:8180/sse", "cursor", "http://localhost:8180/sse?client=cursor"},
		{"appends to http url", "http://localhost:8180/mcp", "team-bot", "http://localhost:8180/mcp?client=team-bot"},
		{"replaces existing client param", "http://localhost:8180/mcp?client=old", "new", "http://localhost:8180/mcp?client=new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppendClientParam(tt.url, tt.clientID); got != tt.want {
				t.Errorf("AppendClientParam(%q, %q) = %q, want %q", tt.url, tt.clientID, got, tt.want)
			}
		})
	}
}

// TestHTTPNativeBuildEntry_EmbedsClientID covers the HTTP-native provisioner
// path that rebuilds the URL from the port: the client identifier must survive.
func TestHTTPNativeBuildEntry_EmbedsClientID(t *testing.T) {
	c := newClaudeCode()
	entry := c.buildEntry(LinkOptions{Port: 8180, ClientID: "team-bot"})
	url, _ := entry["url"].(string)
	if url != "http://localhost:8180/mcp?client=team-bot" {
		t.Errorf("buildEntry url = %q, want embedded client param", url)
	}

	// No client id: legacy URL, unchanged behavior.
	entry = c.buildEntry(LinkOptions{Port: 8180})
	if url, _ := entry["url"].(string); url != "http://localhost:8180/mcp" {
		t.Errorf("buildEntry url = %q, want bare /mcp", url)
	}
}
