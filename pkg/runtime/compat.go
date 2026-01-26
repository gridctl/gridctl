package runtime

// This file contains backward-compatible type aliases and helpers.

import "github.com/gridctl/gridctl/pkg/config"

// Runtime is an alias for Orchestrator for backward compatibility.
// Deprecated: Use Orchestrator instead.
type Runtime = Orchestrator

// MCPServerInfo is provided for backward compatibility.
// Deprecated: Use MCPServerResult instead.
type MCPServerInfo struct {
	Name            string
	ContainerID     string   // Empty for external/local process/SSH servers
	ContainerName   string   // Empty for external/local process/SSH servers
	ContainerPort   int      // 0 for external/local process/SSH servers
	HostPort        int      // 0 for external/local process/SSH servers
	External        bool     // True if external server (no container)
	LocalProcess    bool     // True if local process server (no container)
	SSH             bool     // True if SSH server (remote process over SSH)
	URL             string   // Full URL for external servers
	Command         []string // Command for local process or SSH servers
	SSHHost         string   // SSH hostname (for SSH servers)
	SSHUser         string   // SSH username (for SSH servers)
	SSHPort         int      // SSH port (for SSH servers, 0 = default 22)
	SSHIdentityFile string   // SSH identity file path (for SSH servers)
}

// AgentInfo is provided for backward compatibility.
// Deprecated: Use AgentResult instead.
type AgentInfo struct {
	Name          string
	ContainerID   string
	ContainerName string
	Uses          []config.ToolSelector // MCP servers this agent depends on
}

// ContainerStatus holds status information for a container.
// Deprecated: Use WorkloadStatus instead.
type ContainerStatus struct {
	ID            string
	Name          string
	Image         string
	State         string
	Status        string
	Type          string // "mcp-server", "resource", or "agent"
	MCPServerName string // Name of the MCP server, resource, or agent
	Topology      string
}

// LegacyUpResult provides backward-compatible result format.
type LegacyUpResult struct {
	MCPServers []MCPServerInfo
	Agents     []AgentInfo
}

// ToLegacyResult converts UpResult to LegacyUpResult for backward compatibility.
func (r *UpResult) ToLegacyResult() *LegacyUpResult {
	legacy := &LegacyUpResult{
		MCPServers: make([]MCPServerInfo, len(r.MCPServers)),
		Agents:     make([]AgentInfo, len(r.Agents)),
	}

	for i, s := range r.MCPServers {
		legacy.MCPServers[i] = MCPServerInfo{
			Name:            s.Name,
			ContainerID:     string(s.WorkloadID),
			ContainerName:   s.Name,
			HostPort:        s.HostPort,
			External:        s.External,
			LocalProcess:    s.LocalProcess,
			SSH:             s.SSH,
			URL:             s.URL,
			Command:         s.Command,
			SSHHost:         s.SSHHost,
			SSHUser:         s.SSHUser,
			SSHPort:         s.SSHPort,
			SSHIdentityFile: s.SSHIdentityFile,
		}
	}

	for i, a := range r.Agents {
		legacy.Agents[i] = AgentInfo{
			Name:        a.Name,
			ContainerID: string(a.WorkloadID),
			Uses:        a.Uses,
		}
	}

	return legacy
}

// ToLegacyStatuses converts []WorkloadStatus to []ContainerStatus for backward compatibility.
func ToLegacyStatuses(statuses []WorkloadStatus) []ContainerStatus {
	legacy := make([]ContainerStatus, len(statuses))
	for i, s := range statuses {
		// Handle short IDs
		id := string(s.ID)
		if len(id) > 12 {
			id = id[:12]
		}
		legacy[i] = ContainerStatus{
			ID:       id,
			Name:     s.Name,
			Image:    s.Image,
			State:    string(s.State),
			Status:   s.Message,
			Type:     string(s.Type),
			Topology: s.Topology,
		}
		// Set MCPServerName based on type
		if s.Labels != nil {
			if name, ok := s.Labels["gridctl.mcp-server"]; ok {
				legacy[i].MCPServerName = name
			} else if name, ok := s.Labels["gridctl.resource"]; ok {
				legacy[i].MCPServerName = name
			} else if name, ok := s.Labels["gridctl.agent"]; ok {
				legacy[i].MCPServerName = name
			}
		}
	}
	return legacy
}

// Label constants re-exported for backward compatibility.
const (
	LabelManaged   = "gridctl.managed"
	LabelTopology  = "gridctl.topology"
	LabelMCPServer = "gridctl.mcp-server"
	LabelResource  = "gridctl.resource"
	LabelAgent     = "gridctl.agent"
)
