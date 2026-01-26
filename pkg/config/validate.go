package config

import (
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return "validation errors:\n  - " + strings.Join(msgs, "\n  - ")
}

// Validate checks the topology configuration for errors.
func Validate(t *Topology) error {
	var errs ValidationErrors

	// Topology-level validation
	if t.Name == "" {
		errs = append(errs, ValidationError{"topology.name", "is required"})
	}

	// Network mode validation
	hasNetwork := t.Network.Name != ""
	hasNetworks := len(t.Networks) > 0

	if hasNetwork && hasNetworks {
		errs = append(errs, ValidationError{"topology", "cannot have both 'network' and 'networks' - use one or the other"})
	}

	// Build network name set for advanced mode validation
	networkNames := make(map[string]bool)
	if hasNetworks {
		// Validate each network in the networks list
		for i, net := range t.Networks {
			prefix := fmt.Sprintf("networks[%d]", i)
			if net.Name == "" {
				errs = append(errs, ValidationError{prefix + ".name", "is required"})
			} else if networkNames[net.Name] {
				errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("duplicate network name '%s'", net.Name)})
			} else {
				networkNames[net.Name] = true
			}
			if net.Driver != "" && net.Driver != "bridge" && net.Driver != "host" && net.Driver != "none" {
				errs = append(errs, ValidationError{prefix + ".driver", "must be 'bridge', 'host', or 'none'"})
			}
		}
	} else {
		// Simple mode: validate single network
		if t.Network.Name == "" {
			errs = append(errs, ValidationError{"topology.network.name", "is required"})
		}
		if t.Network.Driver != "" && t.Network.Driver != "bridge" && t.Network.Driver != "host" && t.Network.Driver != "none" {
			errs = append(errs, ValidationError{"topology.network.driver", "must be 'bridge', 'host', or 'none'"})
		}
	}

	// MCP server validation
	serverNames := make(map[string]bool)
	for i, server := range t.MCPServers {
		prefix := fmt.Sprintf("mcp-servers[%d]", i)

		if server.Name == "" {
			errs = append(errs, ValidationError{prefix + ".name", "is required"})
		} else if serverNames[server.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("duplicate MCP server name '%s'", server.Name)})
		} else {
			serverNames[server.Name] = true
		}

		// Determine server type
		hasImage := server.Image != ""
		hasSource := server.Source != nil
		hasURL := server.URL != ""
		hasSSH := server.SSH != nil && len(server.Command) > 0
		hasCommand := len(server.Command) > 0 && !hasImage && !hasSource && !hasURL && !hasSSH // command-only = local process

		// Mutual exclusivity: must have exactly one of image, source, url, command (local process), or ssh
		count := 0
		if hasImage {
			count++
		}
		if hasSource {
			count++
		}
		if hasURL {
			count++
		}
		if hasCommand {
			count++
		}
		if hasSSH {
			count++
		}

		if count == 0 {
			errs = append(errs, ValidationError{prefix, "must have 'image', 'source', 'url', 'command', or 'ssh' with 'command'"})
		} else if count > 1 {
			errs = append(errs, ValidationError{prefix, "can only have one of 'image', 'source', 'url', 'command', or 'ssh'"})
		}

		// External server validation (URL-only)
		if server.IsExternal() {
			// Transport must be http or sse for external servers
			if server.Transport == "stdio" {
				errs = append(errs, ValidationError{prefix + ".transport", "stdio not valid for external URL servers"})
			}
			// Validate transport is known
			if server.Transport != "" && server.Transport != "http" && server.Transport != "sse" {
				errs = append(errs, ValidationError{prefix + ".transport", "must be 'http' or 'sse' for external servers"})
			}
			// Port is not required for URL servers (URL includes the endpoint)
			if server.Port != 0 {
				errs = append(errs, ValidationError{prefix + ".port", "should not be set for external URL servers (use url instead)"})
			}
			// Network is not applicable for external servers
			if server.Network != "" {
				errs = append(errs, ValidationError{prefix + ".network", "not applicable for external URL servers"})
			}
		} else if server.IsLocalProcess() {
			// Local process server validation (command-only)
			// Transport must be stdio for local process servers
			if server.Transport != "" && server.Transport != "stdio" {
				errs = append(errs, ValidationError{prefix + ".transport", "must be 'stdio' for local process servers"})
			}
			// Port is not applicable for local process servers (they use stdio)
			if server.Port != 0 {
				errs = append(errs, ValidationError{prefix + ".port", "should not be set for local process servers (use stdio transport)"})
			}
			// Network is not applicable for local process servers
			if server.Network != "" {
				errs = append(errs, ValidationError{prefix + ".network", "not applicable for local process servers"})
			}
		} else if server.IsSSH() {
			// SSH server validation
			sshPrefix := prefix + ".ssh"
			if server.SSH.Host == "" {
				errs = append(errs, ValidationError{sshPrefix + ".host", "is required"})
			}
			if server.SSH.User == "" {
				errs = append(errs, ValidationError{sshPrefix + ".user", "is required"})
			}
			if server.SSH.Port < 0 || server.SSH.Port > 65535 {
				errs = append(errs, ValidationError{sshPrefix + ".port", "must be between 0 and 65535"})
			}
			// Transport must be stdio for SSH servers (they use stdin/stdout over SSH)
			if server.Transport != "" && server.Transport != "stdio" {
				errs = append(errs, ValidationError{prefix + ".transport", "must be 'stdio' for SSH servers"})
			}
			// Port is not applicable for SSH servers (use ssh.port for SSH port)
			if server.Port != 0 {
				errs = append(errs, ValidationError{prefix + ".port", "should not be set for SSH servers (use ssh.port for SSH port)"})
			}
			// Network is not applicable for SSH servers
			if server.Network != "" {
				errs = append(errs, ValidationError{prefix + ".network", "not applicable for SSH servers"})
			}
		} else {
			// Container-based server validation (existing logic)
			// Source validation
			if server.Source != nil {
				errs = append(errs, validateSource(server.Source, prefix+".source")...)
			}

			// Transport validation
			if server.Transport != "" && server.Transport != "http" && server.Transport != "sse" && server.Transport != "stdio" {
				errs = append(errs, ValidationError{prefix + ".transport", "must be 'http', 'sse', or 'stdio'"})
			}

			// Port validation (only required for HTTP/SSE transport)
			if server.Transport != "stdio" {
				if server.Port <= 0 {
					errs = append(errs, ValidationError{prefix + ".port", "must be a positive integer"})
				}
				if server.Port > 65535 {
					errs = append(errs, ValidationError{prefix + ".port", "must be <= 65535"})
				}
			}

			// Network validation (only in advanced mode for container servers)
			if hasNetworks {
				if server.Network == "" {
					errs = append(errs, ValidationError{prefix + ".network", "required when 'networks' is defined"})
				} else if !networkNames[server.Network] {
					errs = append(errs, ValidationError{prefix + ".network", fmt.Sprintf("network '%s' not found in networks list", server.Network)})
				}
			}
		}
		// In simple mode, server.Network is ignored (per design decision)
	}

	// Resource validation
	resourceNames := make(map[string]bool)
	for i, resource := range t.Resources {
		prefix := fmt.Sprintf("resources[%d]", i)

		if resource.Name == "" {
			errs = append(errs, ValidationError{prefix + ".name", "is required"})
		} else if resourceNames[resource.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("duplicate resource name '%s'", resource.Name)})
		} else if serverNames[resource.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("name '%s' conflicts with an MCP server", resource.Name)})
		} else {
			resourceNames[resource.Name] = true
		}

		if resource.Image == "" {
			errs = append(errs, ValidationError{prefix + ".image", "is required"})
		}

		// Network validation (only in advanced mode)
		if hasNetworks {
			if resource.Network == "" {
				errs = append(errs, ValidationError{prefix + ".network", "required when 'networks' is defined"})
			} else if !networkNames[resource.Network] {
				errs = append(errs, ValidationError{prefix + ".network", fmt.Sprintf("network '%s' not found in networks list", resource.Network)})
			}
		}
		// In simple mode, resource.Network is ignored (per design decision)
	}

	// Agent validation - first pass: collect names and A2A status
	agentNames := make(map[string]bool)
	a2aEnabledAgents := make(map[string]bool)
	for i, agent := range t.Agents {
		prefix := fmt.Sprintf("agents[%d]", i)

		if agent.Name == "" {
			errs = append(errs, ValidationError{prefix + ".name", "is required"})
		} else if agentNames[agent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("duplicate agent name '%s'", agent.Name)})
		} else if serverNames[agent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("name '%s' conflicts with an MCP server", agent.Name)})
		} else if resourceNames[agent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("name '%s' conflicts with a resource", agent.Name)})
		} else {
			agentNames[agent.Name] = true
			if agent.IsA2AEnabled() {
				a2aEnabledAgents[agent.Name] = true
			}
		}
	}

	// Agent validation - second pass: validate structure and dependencies
	for i, agent := range t.Agents {
		prefix := fmt.Sprintf("agents[%d]", i)

		// Determine agent mode: container-based or headless
		hasImage := agent.Image != ""
		hasSource := agent.Source != nil
		hasRuntime := agent.Runtime != ""

		if hasRuntime {
			// Headless agent validation
			if hasImage {
				errs = append(errs, ValidationError{prefix, "cannot have both 'runtime' and 'image'"})
			}
			if hasSource {
				errs = append(errs, ValidationError{prefix, "cannot have both 'runtime' and 'source'"})
			}
			if agent.Prompt == "" {
				errs = append(errs, ValidationError{prefix + ".prompt", "is required when 'runtime' is set"})
			}
		} else {
			// Container-based agent validation
			if !hasImage && !hasSource {
				errs = append(errs, ValidationError{prefix, "must have either 'image', 'source', or 'runtime'"})
			}
			if hasImage && hasSource {
				errs = append(errs, ValidationError{prefix, "cannot have both 'image' and 'source'"})
			}
		}

		// Source validation
		if agent.Source != nil {
			errs = append(errs, validateSource(agent.Source, prefix+".source")...)
		}

		// Validate 'uses' dependencies exist in mcp-servers or A2A-enabled agents
		for j, selector := range agent.Uses {
			dep := selector.Server
			isValidServer := serverNames[dep]
			isValidAgent := a2aEnabledAgents[dep] && dep != agent.Name // Can't reference self
			if !isValidServer && !isValidAgent {
				if dep == agent.Name {
					errs = append(errs, ValidationError{
						fmt.Sprintf("%s.uses[%d]", prefix, j),
						"agent cannot reference itself",
					})
				} else if agentNames[dep] && !a2aEnabledAgents[dep] {
					errs = append(errs, ValidationError{
						fmt.Sprintf("%s.uses[%d]", prefix, j),
						fmt.Sprintf("agent '%s' must have A2A enabled to be used as a skill", dep),
					})
				} else {
					errs = append(errs, ValidationError{
						fmt.Sprintf("%s.uses[%d]", prefix, j),
						fmt.Sprintf("'%s' not found in mcp-servers or A2A-enabled agents", dep),
					})
				}
			}
		}

		// Network validation (only in advanced mode)
		if hasNetworks {
			if agent.Network == "" {
				errs = append(errs, ValidationError{prefix + ".network", "required when 'networks' is defined"})
			} else if !networkNames[agent.Network] {
				errs = append(errs, ValidationError{prefix + ".network", fmt.Sprintf("network '%s' not found in networks list", agent.Network)})
			}
		}
		// In simple mode, agent.Network is ignored (per design decision)

		// A2A validation (if a2a block is present)
		if agent.A2A != nil {
			a2aPrefix := prefix + ".a2a"

			// Skills validation
			skillIDs := make(map[string]bool)
			for j, skill := range agent.A2A.Skills {
				skillPrefix := fmt.Sprintf("%s.skills[%d]", a2aPrefix, j)

				if skill.ID == "" {
					errs = append(errs, ValidationError{skillPrefix + ".id", "is required"})
				} else if skillIDs[skill.ID] {
					errs = append(errs, ValidationError{skillPrefix + ".id", fmt.Sprintf("duplicate skill ID '%s'", skill.ID)})
				} else {
					skillIDs[skill.ID] = true
				}

				if skill.Name == "" {
					errs = append(errs, ValidationError{skillPrefix + ".name", "is required"})
				}
			}
		}
	}

	// A2A agent validation
	a2aAgentNames := make(map[string]bool)
	for i, a2aAgent := range t.A2AAgents {
		prefix := fmt.Sprintf("a2a-agents[%d]", i)

		if a2aAgent.Name == "" {
			errs = append(errs, ValidationError{prefix + ".name", "is required"})
		} else if a2aAgentNames[a2aAgent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("duplicate A2A agent name '%s'", a2aAgent.Name)})
		} else if agentNames[a2aAgent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("name '%s' conflicts with a local agent", a2aAgent.Name)})
		} else if serverNames[a2aAgent.Name] {
			errs = append(errs, ValidationError{prefix + ".name", fmt.Sprintf("name '%s' conflicts with an MCP server", a2aAgent.Name)})
		} else {
			a2aAgentNames[a2aAgent.Name] = true
		}

		if a2aAgent.URL == "" {
			errs = append(errs, ValidationError{prefix + ".url", "is required"})
		}

		if a2aAgent.Auth != nil {
			authPrefix := prefix + ".auth"
			validAuthTypes := map[string]bool{"bearer": true, "api_key": true, "none": true, "": true}
			if !validAuthTypes[a2aAgent.Auth.Type] {
				errs = append(errs, ValidationError{authPrefix + ".type", "must be 'bearer', 'api_key', or 'none'"})
			}
			if a2aAgent.Auth.Type != "" && a2aAgent.Auth.Type != "none" && a2aAgent.Auth.TokenEnv == "" {
				errs = append(errs, ValidationError{authPrefix + ".token_env", "is required when auth.type is set"})
			}
		}
	}

	// Check for circular dependencies between agents
	if cycleErr := detectAgentCycles(t, a2aEnabledAgents); cycleErr != nil {
		errs = append(errs, ValidationError{"agents", cycleErr.Error()})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// detectAgentCycles checks for circular dependencies in agent-to-agent relationships.
func detectAgentCycles(t *Topology, a2aEnabledAgents map[string]bool) error {
	// Build adjacency list for agent dependencies (only agent-to-agent, not agent-to-server)
	deps := make(map[string][]string)
	for _, agent := range t.Agents {
		var agentDeps []string
		for _, selector := range agent.Uses {
			if a2aEnabledAgents[selector.Server] {
				agentDeps = append(agentDeps, selector.Server)
			}
		}
		deps[agent.Name] = agentDeps
	}

	// DFS-based cycle detection
	const (
		white = iota // Unvisited
		gray         // Visiting (in current path)
		black        // Visited (finished)
	)

	color := make(map[string]int)
	var cycle []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, neighbor := range deps[node] {
			if color[neighbor] == gray {
				// Found a back edge - cycle detected
				cycle = append(cycle, neighbor, node)
				return true
			}
			if color[neighbor] == white {
				if dfs(neighbor) {
					if len(cycle) > 0 && cycle[0] != cycle[len(cycle)-1] {
						cycle = append(cycle, node)
					}
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	for name := range deps {
		if color[name] == white {
			if dfs(name) {
				// Reverse the cycle path for readable output
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
			}
		}
	}

	return nil
}

func validateSource(s *Source, prefix string) ValidationErrors {
	var errs ValidationErrors

	switch s.Type {
	case "git":
		if s.URL == "" {
			errs = append(errs, ValidationError{prefix + ".url", "is required for git source"})
		}
		if s.Path != "" {
			errs = append(errs, ValidationError{prefix + ".path", "should not be set for git source (use 'url' instead)"})
		}
	case "local":
		if s.Path == "" {
			errs = append(errs, ValidationError{prefix + ".path", "is required for local source"})
		}
		if s.URL != "" {
			errs = append(errs, ValidationError{prefix + ".url", "should not be set for local source (use 'path' instead)"})
		}
	case "":
		errs = append(errs, ValidationError{prefix + ".type", "is required (must be 'git' or 'local')"})
	default:
		errs = append(errs, ValidationError{prefix + ".type", "must be 'git' or 'local'"})
	}

	return errs
}
