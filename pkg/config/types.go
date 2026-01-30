package config

import "gopkg.in/yaml.v3"

// Stack represents the complete gridctl configuration.
type Stack struct {
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
	Command   []string          `yaml:"command,omitempty"`   // Override container command or remote command for SSH
	Env       map[string]string `yaml:"env,omitempty"`
	BuildArgs map[string]string `yaml:"build_args,omitempty"`
	Network   string            `yaml:"network,omitempty"`   // Network to join (for multi-network mode)
	SSH       *SSHConfig        `yaml:"ssh,omitempty"`       // SSH connection config for remote servers
	OpenAPI   *OpenAPIConfig    `yaml:"openapi,omitempty"`   // OpenAPI spec config for API-backed servers
	Tools     []string          `yaml:"tools,omitempty"`     // Tool whitelist (empty = all tools exposed)
}

// OpenAPIConfig defines an MCP server backed by an OpenAPI specification.
// The spec is parsed and each operation becomes an MCP tool.
type OpenAPIConfig struct {
	Spec       string            `yaml:"spec"`                 // URL or local file path to OpenAPI spec (JSON or YAML)
	BaseURL    string            `yaml:"baseUrl,omitempty"`    // Override the server URL from the spec
	Auth       *OpenAPIAuth      `yaml:"auth,omitempty"`       // Authentication configuration
	Operations *OperationsFilter `yaml:"operations,omitempty"` // Filter which operations become tools
}

// OpenAPIAuth defines authentication for OpenAPI HTTP requests.
type OpenAPIAuth struct {
	Type     string `yaml:"type"`               // "bearer" or "header"
	TokenEnv string `yaml:"tokenEnv,omitempty"` // Env var name containing bearer token (for type: bearer)
	Header   string `yaml:"header,omitempty"`   // Header name (for type: header, e.g., "X-API-Key")
	ValueEnv string `yaml:"valueEnv,omitempty"` // Env var name containing header value (for type: header)
}

// OperationsFilter defines which OpenAPI operations to include or exclude.
// Only one of Include or Exclude should be specified.
type OperationsFilter struct {
	Include []string `yaml:"include,omitempty"` // Operation IDs to include (whitelist)
	Exclude []string `yaml:"exclude,omitempty"` // Operation IDs to exclude (blacklist)
}

// SSHConfig defines SSH connection parameters for remote MCP servers.
type SSHConfig struct {
	Host         string `yaml:"host"`                    // Required: hostname or IP address
	User         string `yaml:"user"`                    // Required: SSH username
	Port         int    `yaml:"port,omitempty"`          // Optional: SSH port (default 22)
	IdentityFile string `yaml:"identityFile,omitempty"`  // Optional: path to SSH private key
}

// IsExternal returns true if this is an external MCP server (URL-only, no container).
func (s *MCPServer) IsExternal() bool {
	return s.URL != "" && s.Image == "" && s.Source == nil
}

// IsLocalProcess returns true if this is a local process MCP server (command-only, no container).
func (s *MCPServer) IsLocalProcess() bool {
	return len(s.Command) > 0 && s.Image == "" && s.Source == nil && s.URL == "" && s.SSH == nil
}

// IsSSH returns true if this is an SSH-based MCP server (ssh config with command).
func (s *MCPServer) IsSSH() bool {
	return s.SSH != nil && len(s.Command) > 0 && s.Image == "" && s.Source == nil && s.URL == ""
}

// IsOpenAPI returns true if this is an OpenAPI-based MCP server.
func (s *MCPServer) IsOpenAPI() bool {
	return s.OpenAPI != nil && s.Image == "" && s.Source == nil && s.URL == "" && s.SSH == nil
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

// ToolSelector specifies which tools an agent can access from an MCP server.
// Supports both string format (server name only) and object format (server + tools).
type ToolSelector struct {
	Server string   `yaml:"server" json:"server"`                   // MCP server or A2A agent name
	Tools  []string `yaml:"tools,omitempty" json:"tools,omitempty"` // Tool whitelist (empty = all tools from this server)
}

// Agent defines an active agent container that consumes MCP tools.
type Agent struct {
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image,omitempty"`
	Source         *Source           `yaml:"source,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	Capabilities   []string          `yaml:"capabilities,omitempty"`
	Uses           []ToolSelector    `yaml:"uses"`                      // References mcp-servers or agents by name
	EquippedSkills []ToolSelector    `yaml:"equipped_skills,omitempty"` // Alias for Uses (merged during load)
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

// SetDefaults applies default values to the stack.
func (s *Stack) SetDefaults() {
	if s.Version == "" {
		s.Version = "1"
	}

	// Progressive network defaults:
	// - If networks[] is defined (advanced mode), don't apply single network defaults
	// - If networks[] is not defined (simple mode), apply single network defaults
	if len(s.Networks) == 0 {
		// Simple mode: use single network
		if s.Network.Driver == "" {
			s.Network.Driver = "bridge"
		}
		if s.Network.Name == "" && s.Name != "" {
			s.Network.Name = s.Name + "-net"
		}
	} else {
		// Advanced mode: set default driver for each network if not specified
		for i := range s.Networks {
			if s.Networks[i].Driver == "" {
				s.Networks[i].Driver = "bridge"
			}
		}
	}

	for i := range s.MCPServers {
		if s.MCPServers[i].Source != nil {
			if s.MCPServers[i].Source.Dockerfile == "" {
				s.MCPServers[i].Source.Dockerfile = "Dockerfile"
			}
			if s.MCPServers[i].Source.Type == "git" && s.MCPServers[i].Source.Ref == "" {
				s.MCPServers[i].Source.Ref = "main"
			}
		}
	}

	for i := range s.Agents {
		if s.Agents[i].Source != nil {
			if s.Agents[i].Source.Dockerfile == "" {
				s.Agents[i].Source.Dockerfile = "Dockerfile"
			}
			if s.Agents[i].Source.Type == "git" && s.Agents[i].Source.Ref == "" {
				s.Agents[i].Source.Ref = "main"
			}
		}
	}
}

// UnmarshalYAML implements custom YAML unmarshaling for ToolSelector.
// This allows both string format (legacy) and object format (new).
//
// String format (legacy):
//
//	uses:
//	  - server-name
//
// Object format (new):
//
//	uses:
//	  - server: server-name
//	    tools: ["tool1", "tool2"]
func (ts *ToolSelector) UnmarshalYAML(node *yaml.Node) error {
	// Try string format first (legacy)
	if node.Kind == yaml.ScalarNode {
		var serverName string
		if err := node.Decode(&serverName); err != nil {
			return err
		}
		ts.Server = serverName
		ts.Tools = nil // Empty means all tools
		return nil
	}

	// Try object format
	type toolSelectorAlias ToolSelector
	var alias toolSelectorAlias
	if err := node.Decode(&alias); err != nil {
		return err
	}
	*ts = ToolSelector(alias)
	return nil
}

// ServerNames returns a slice of server names from a slice of ToolSelectors.
// This is useful for backward compatibility with code that expects []string.
func ServerNames(selectors []ToolSelector) []string {
	names := make([]string, len(selectors))
	for i, ts := range selectors {
		names[i] = ts.Server
	}
	return names
}
