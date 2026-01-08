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

		// Must have either image or source, not both
		hasImage := server.Image != ""
		hasSource := server.Source != nil

		if !hasImage && !hasSource {
			errs = append(errs, ValidationError{prefix, "must have either 'image' or 'source'"})
		}
		if hasImage && hasSource {
			errs = append(errs, ValidationError{prefix, "cannot have both 'image' and 'source'"})
		}

		// Source validation
		if server.Source != nil {
			errs = append(errs, validateSource(server.Source, prefix+".source")...)
		}

		// Port validation (only required for HTTP transport)
		if server.Transport != "stdio" {
			if server.Port <= 0 {
				errs = append(errs, ValidationError{prefix + ".port", "must be a positive integer"})
			}
			if server.Port > 65535 {
				errs = append(errs, ValidationError{prefix + ".port", "must be <= 65535"})
			}
		}

		// Network validation (only in advanced mode)
		if hasNetworks {
			if server.Network == "" {
				errs = append(errs, ValidationError{prefix + ".network", "required when 'networks' is defined"})
			} else if !networkNames[server.Network] {
				errs = append(errs, ValidationError{prefix + ".network", fmt.Sprintf("network '%s' not found in networks list", server.Network)})
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

	// Agent validation
	agentNames := make(map[string]bool)
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
		}

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

		// Validate 'uses' dependencies exist in mcp-servers
		for j, dep := range agent.Uses {
			if !serverNames[dep] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.uses[%d]", prefix, j),
					fmt.Sprintf("MCP server '%s' not found in mcp-servers", dep),
				})
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

	if len(errs) > 0 {
		return errs
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
