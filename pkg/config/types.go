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
}

// Network defines the Docker network configuration.
type Network struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver"`
}

// MCPServer defines an MCP server container.
type MCPServer struct {
	Name      string            `yaml:"name"`
	Image     string            `yaml:"image,omitempty"`
	Source    *Source           `yaml:"source,omitempty"`
	Port      int               `yaml:"port,omitempty"`      // For HTTP transport
	Transport string            `yaml:"transport,omitempty"` // "http" (default) or "stdio"
	Command   []string          `yaml:"command,omitempty"`   // Override container command
	Env       map[string]string `yaml:"env,omitempty"`
	BuildArgs map[string]string `yaml:"build_args,omitempty"`
	Network   string            `yaml:"network,omitempty"`   // Network to join (for multi-network mode)
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
	Name         string            `yaml:"name"`
	Image        string            `yaml:"image,omitempty"`
	Source       *Source           `yaml:"source,omitempty"`
	Description  string            `yaml:"description,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty"`
	Uses         []string          `yaml:"uses"`                    // References mcp-servers by name
	Env          map[string]string `yaml:"env,omitempty"`
	BuildArgs    map[string]string `yaml:"build_args,omitempty"`
	Network      string            `yaml:"network,omitempty"`       // Network to join (for multi-network mode)
	Runtime      string            `yaml:"runtime,omitempty"`       // Headless runtime (e.g., "claude-code")
	Prompt       string            `yaml:"prompt,omitempty"`        // System prompt for headless agents
}

// IsHeadless returns true if the agent uses a headless runtime.
func (a *Agent) IsHeadless() bool {
	return a.Runtime != ""
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
