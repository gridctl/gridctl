package config

// Topology represents the complete agentlab configuration.
type Topology struct {
	Version    string      `yaml:"version"`
	Name       string      `yaml:"name"`
	Network    Network     `yaml:"network"`              // Single network (simple mode)
	Networks   []Network   `yaml:"networks,omitempty"`   // Multiple networks (advanced mode)
	MCPServers []MCPServer `yaml:"mcp-servers"`
	Agents     []Agent     `yaml:"agents,omitempty"`     // Active agents that consume MCP tools
	Resources  []Resource  `yaml:"resources,omitempty"`
	A2AAgents  []A2AAgent  `yaml:"a2a-agents,omitempty"` // External A2A agents for agent-to-agent communication
}

// Network defines the Docker network configuration.
type Network struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver"`
}

// MCPServer defines an MCP server (container-based or external).
type MCPServer struct {
	Name      string            `yaml:"name"`
	Image     string            `yaml:"image,omitempty"`
	Source    *Source           `yaml:"source,omitempty"`
	URL       string            `yaml:"url,omitempty"`       // External server URL (no container)
	Port      int               `yaml:"port,omitempty"`      // For HTTP transport (container-based)
	Transport string            `yaml:"transport,omitempty"` // "http" (default), "stdio", or "sse"
	Command   []string          `yaml:"command,omitempty"`   // Override container command
	Env       map[string]string `yaml:"env,omitempty"`
	BuildArgs map[string]string `yaml:"build_args,omitempty"`
	Network   string            `yaml:"network,omitempty"`   // Network to join (for multi-network mode)
}

// IsExternal returns true if this is an external MCP server (URL-only, no container).
func (s *MCPServer) IsExternal() bool {
	return s.URL != "" && s.Image == "" && s.Source == nil
}

// IsLocalProcess returns true if this is a local process MCP server (command-only, no container).
func (s *MCPServer) IsLocalProcess() bool {
	return len(s.Command) > 0 && s.Image == "" && s.Source == nil && s.URL == ""
}

// Source defines how to build an MCP server from source code.
type Source struct {
	Type       string `yaml:"type"` // "git" or "local"
	URL        string `yaml:"url,omitempty"`
	Ref        string `yaml:"ref,omitempty"`
	Path       string `yaml:"path,omitempty"`
	Dockerfile string `yaml:"dockerfile,omitempty"`
}

// Resource defines a supporting container (database, cache, etc).
type Resource struct {
	Name    string            `yaml:"name"`
	Image   string            `yaml:"image"`
	Env     map[string]string `yaml:"env,omitempty"`
	Ports   []string          `yaml:"ports,omitempty"`
	Volumes []string          `yaml:"volumes,omitempty"`
	Network string            `yaml:"network,omitempty"` // Network to join (for multi-network mode)
}

// Agent defines an active agent container that consumes MCP tools.
type Agent struct {
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image,omitempty"`
	Source         *Source           `yaml:"source,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	Capabilities   []string          `yaml:"capabilities,omitempty"`
	Uses           []string          `yaml:"uses"`                      // References mcp-servers or agents by name
	EquippedSkills []string          `yaml:"equipped_skills,omitempty"` // Alias for Uses (merged during load)
	Env            map[string]string `yaml:"env,omitempty"`
	BuildArgs      map[string]string `yaml:"build_args,omitempty"`
	Network        string            `yaml:"network,omitempty"`         // Network to join (for multi-network mode)
	Command        []string          `yaml:"command,omitempty"`         // Override container entrypoint
	Runtime        string            `yaml:"runtime,omitempty"`         // Headless runtime (e.g., "claude-code")
	Prompt         string            `yaml:"prompt,omitempty"`          // System prompt for headless agents
	A2A            *A2AConfig        `yaml:"a2a,omitempty"`             // A2A protocol configuration
}

// A2AConfig defines A2A protocol settings for exposing an agent via A2A.
type A2AConfig struct {
	Enabled  bool       `yaml:"enabled,omitempty"`  // Enable A2A exposure (default: true when block present)
	Version  string     `yaml:"version,omitempty"`  // Agent version (default: "1.0.0")
	Skills   []A2ASkill `yaml:"skills,omitempty"`   // Skills this agent exposes
}

// A2ASkill represents a capability the agent can perform.
type A2ASkill struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

// A2AAgent defines an external A2A agent reference.
type A2AAgent struct {
	Name string    `yaml:"name"`               // Local alias for this remote agent
	URL  string    `yaml:"url"`                // Base URL for the remote agent's A2A endpoint
	Auth *A2AAuth  `yaml:"auth,omitempty"`     // Authentication configuration
}

// A2AAuth contains authentication configuration for A2A connections.
type A2AAuth struct {
	Type       string `yaml:"type,omitempty"`        // "bearer", "api_key", or "none"
	TokenEnv   string `yaml:"token_env,omitempty"`   // Environment variable containing the token
	HeaderName string `yaml:"header_name,omitempty"` // Header name for API key auth (default: "Authorization")
}

// IsHeadless returns true if the agent uses a headless runtime.
func (a *Agent) IsHeadless() bool {
	return a.Runtime != ""
}

// IsA2AEnabled returns true if the agent is exposed via A2A protocol.
func (a *Agent) IsA2AEnabled() bool {
	if a.A2A == nil {
		return false
	}
	// If A2A block is present, it's enabled unless explicitly disabled
	return a.A2A.Enabled || len(a.A2A.Skills) > 0
}

// SetDefaults applies default values to the topology.
func (t *Topology) SetDefaults() {
	if t.Version == "" {
		t.Version = "1"
	}

	// Progressive network defaults:
	// - If networks[] is defined (advanced mode), don't apply single network defaults
	// - If networks[] is not defined (simple mode), apply single network defaults
	if len(t.Networks) == 0 {
		// Simple mode: use single network
		if t.Network.Driver == "" {
			t.Network.Driver = "bridge"
		}
		if t.Network.Name == "" && t.Name != "" {
			t.Network.Name = t.Name + "-net"
		}
	} else {
		// Advanced mode: set default driver for each network if not specified
		for i := range t.Networks {
			if t.Networks[i].Driver == "" {
				t.Networks[i].Driver = "bridge"
			}
		}
	}

	for i := range t.MCPServers {
		if t.MCPServers[i].Source != nil {
			if t.MCPServers[i].Source.Dockerfile == "" {
				t.MCPServers[i].Source.Dockerfile = "Dockerfile"
			}
			if t.MCPServers[i].Source.Type == "git" && t.MCPServers[i].Source.Ref == "" {
				t.MCPServers[i].Source.Ref = "main"
			}
		}
	}

	for i := range t.Agents {
		if t.Agents[i].Source != nil {
			if t.Agents[i].Source.Dockerfile == "" {
				t.Agents[i].Source.Dockerfile = "Dockerfile"
			}
			if t.Agents[i].Source.Type == "git" && t.Agents[i].Source.Ref == "" {
				t.Agents[i].Source.Ref = "main"
			}
		}
	}
}
