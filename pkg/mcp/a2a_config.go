package mcp

import "time"

// A2AClientConfig carries resolved outbound A2A settings, never status data.
type A2AClientConfig struct {
	Card     string
	Endpoint string
	Dialect  string
	Profile  string
	Include  []string
	Timeout  time.Duration
	Token    string
}
