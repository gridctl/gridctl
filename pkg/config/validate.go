package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// maxReplicas is the sanity cap on MCPServer.Replicas. Values above this
// are almost certainly a config error; the cap also bounds per-server
// fan-out costs for things like health checking and least-connections scans.
const maxReplicas = 32

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

// Validate checks the stack configuration for errors.
func Validate(s *Stack) error {
	var errs ValidationErrors

	// Stack-level validation
	if s.Name == "" {
		errs = append(errs, ValidationError{"stack.name", "is required"})
	}

	// Gateway code_mode validation
	if s.Gateway != nil && s.Gateway.CodeMode != "" {
		validModes := map[string]bool{"off": true, "on": true}
		if !validModes[s.Gateway.CodeMode] {
			errs = append(errs, ValidationError{"gateway.code_mode", "must be 'off' or 'on'"})
		}
		if s.Gateway.CodeModeTimeout < 0 {
			errs = append(errs, ValidationError{"gateway.code_mode_timeout", "must be a positive integer"})
		}
	}

	// Gateway output_format validation
	validOutputFormats := map[string]bool{"json": true, "toon": true, "csv": true, "text": true}
	if s.Gateway != nil && s.Gateway.OutputFormat != "" {
		if !validOutputFormats[s.Gateway.OutputFormat] {
			errs = append(errs, ValidationError{"gateway.output_format", "must be one of: json, toon, csv, text"})
		}
	}

	// Gateway maxToolResultBytes validation
	if s.Gateway != nil && s.Gateway.MaxToolResultBytes < 0 {
		errs = append(errs, ValidationError{"gateway.maxToolResultBytes", "must be a non-negative integer"})
	}

	// Gateway auth validation
	if s.Gateway != nil && s.Gateway.Auth != nil {
		auth := s.Gateway.Auth
		authPrefix := "gateway.auth"
		if auth.Type == "" {
			errs = append(errs, ValidationError{authPrefix + ".type", "is required"})
		} else if auth.Type != "bearer" && auth.Type != "api_key" {
			errs = append(errs, ValidationError{authPrefix + ".type", "must be 'bearer' or 'api_key'"})
		}
		if auth.Token == "" {
			errs = append(errs, ValidationError{authPrefix + ".token", "is required"})
		}
		if auth.Header != "" && auth.Type != "api_key" {
			errs = append(errs, ValidationError{authPrefix + ".header", "only applicable when type is 'api_key'"})
		}
	}

	// Network mode validation
	hasNetwork := s.Network.Name != ""
	hasNetworks := len(s.Networks) > 0

	if hasNetwork && hasNetworks {
		errs = append(errs, ValidationError{"stack", "cannot have both 'network' and 'networks' - use one or the other"})
	}

	// Build network name set for advanced mode validation
	networkNames := make(map[string]bool)
	if hasNetworks {
		// Validate each network in the networks list
		for i, net := range s.Networks {
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
		if s.Network.Name == "" {
			errs = append(errs, ValidationError{"stack.network.name", "is required"})
		}
		if s.Network.Driver != "" && s.Network.Driver != "bridge" && s.Network.Driver != "host" && s.Network.Driver != "none" {
			errs = append(errs, ValidationError{"stack.network.driver", "must be 'bridge', 'host', or 'none'"})
		}
	}

	// MCP server validation
	serverNames := make(map[string]bool)
	for i, server := range s.MCPServers {
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
		hasOpenAPI := server.OpenAPI != nil

		// Mutual exclusivity: must have exactly one of image, source, url, command (local process), ssh, or openapi
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
		if hasOpenAPI {
			count++
		}

		if count == 0 {
			errs = append(errs, ValidationError{prefix, "must have 'image', 'source', 'url', 'command', 'ssh' with 'command', or 'openapi'"})
		} else if count > 1 {
			errs = append(errs, ValidationError{prefix, "can only have one of 'image', 'source', 'url', 'command', 'ssh', or 'openapi'"})
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
			if server.SSH.KnownHostsFile != "" {
				if _, err := os.Stat(server.SSH.KnownHostsFile); err != nil {
					errs = append(errs, ValidationError{sshPrefix + ".knownHostsFile", fmt.Sprintf("file not found or not readable: %s", server.SSH.KnownHostsFile)})
				}
			}
			if server.SSH.JumpHost != "" {
				if strings.ContainsAny(server.SSH.JumpHost, " \t\n;|&$`") {
					errs = append(errs, ValidationError{sshPrefix + ".jumpHost", "invalid format"})
				}
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
		} else if server.IsOpenAPI() {
			// OpenAPI server validation
			openapiPrefix := prefix + ".openapi"
			if server.OpenAPI.Spec == "" {
				errs = append(errs, ValidationError{openapiPrefix + ".spec", "is required"})
			}
			// Auth validation
			if server.OpenAPI.Auth != nil {
				authPrefix := openapiPrefix + ".auth"
				validAuthTypes := map[string]bool{"bearer": true, "header": true, "query": true, "oauth2": true, "basic": true}
				if server.OpenAPI.Auth.Type != "" && !validAuthTypes[server.OpenAPI.Auth.Type] {
					errs = append(errs, ValidationError{authPrefix + ".type", "must be 'bearer', 'header', 'query', 'oauth2', or 'basic'"})
				}
				switch server.OpenAPI.Auth.Type {
				case "bearer":
					if server.OpenAPI.Auth.TokenEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".tokenEnv", "required when type is 'bearer'"})
					}
				case "header":
					if server.OpenAPI.Auth.Header == "" {
						errs = append(errs, ValidationError{authPrefix + ".header", "required when type is 'header'"})
					}
					if server.OpenAPI.Auth.ValueEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".valueEnv", "required when type is 'header'"})
					}
				case "query":
					if server.OpenAPI.Auth.ParamName == "" {
						errs = append(errs, ValidationError{authPrefix + ".paramName", "required when type is 'query'"})
					}
					if server.OpenAPI.Auth.ValueEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".valueEnv", "required when type is 'query'"})
					}
				case "oauth2":
					if server.OpenAPI.Auth.ClientIdEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".clientIdEnv", "required when type is 'oauth2'"})
					}
					if server.OpenAPI.Auth.ClientSecretEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".clientSecretEnv", "required when type is 'oauth2'"})
					}
					if server.OpenAPI.Auth.TokenUrl == "" {
						errs = append(errs, ValidationError{authPrefix + ".tokenUrl", "required when type is 'oauth2'"})
					}
				case "basic":
					if server.OpenAPI.Auth.UsernameEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".usernameEnv", "required when type is 'basic'"})
					}
					if server.OpenAPI.Auth.PasswordEnv == "" {
						errs = append(errs, ValidationError{authPrefix + ".passwordEnv", "required when type is 'basic'"})
					}
				}
			}
			// TLS validation
			if server.OpenAPI.TLS != nil {
				tlsPrefix := openapiPrefix + ".tls"
				if server.OpenAPI.TLS.CertFile != "" && server.OpenAPI.TLS.KeyFile == "" {
					errs = append(errs, ValidationError{tlsPrefix + ".keyFile", "required when certFile is set"})
				}
				if server.OpenAPI.TLS.KeyFile != "" && server.OpenAPI.TLS.CertFile == "" {
					errs = append(errs, ValidationError{tlsPrefix + ".certFile", "required when keyFile is set"})
				}
				}
			// Operations filter validation
			if server.OpenAPI.Operations != nil {
				if len(server.OpenAPI.Operations.Include) > 0 && len(server.OpenAPI.Operations.Exclude) > 0 {
					errs = append(errs, ValidationError{openapiPrefix + ".operations", "cannot use both 'include' and 'exclude'"})
				}
			}
			// Transport is not applicable for OpenAPI servers (uses HTTP internally)
			if server.Transport != "" {
				errs = append(errs, ValidationError{prefix + ".transport", "not applicable for OpenAPI servers"})
			}
			// Port is not applicable for OpenAPI servers
			if server.Port != 0 {
				errs = append(errs, ValidationError{prefix + ".port", "not applicable for OpenAPI servers"})
			}
			// Network is not applicable for OpenAPI servers
			if server.Network != "" {
				errs = append(errs, ValidationError{prefix + ".network", "not applicable for OpenAPI servers"})
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
		// Per-server output_format validation
		if server.OutputFormat != "" && !validOutputFormats[server.OutputFormat] {
			errs = append(errs, ValidationError{prefix + ".output_format", "must be one of: json, toon, csv, text"})
		}

		// ready_timeout validation: must parse as a duration and be non-negative.
		// Only meaningful for container-based HTTP/SSE servers; accepted (but unused)
		// on other types to avoid a cliff when templates share fields.
		if server.ReadyTimeout != "" {
			d, err := time.ParseDuration(server.ReadyTimeout)
			if err != nil {
				errs = append(errs, ValidationError{prefix + ".ready_timeout", fmt.Sprintf("invalid duration %q (expected e.g. \"60s\" or \"2m\")", server.ReadyTimeout)})
			} else if d < 0 {
				errs = append(errs, ValidationError{prefix + ".ready_timeout", "must be non-negative"})
			}
		}

		// Replica validation.
		// Zero is accepted as "unspecified" and defaulted to 1 by Stack.SetDefaults;
		// only reject truly invalid values here.
		if server.Replicas < 0 {
			errs = append(errs, ValidationError{prefix + ".replicas", "must be >= 0"})
		} else if server.Replicas > maxReplicas {
			errs = append(errs, ValidationError{prefix + ".replicas", fmt.Sprintf("must be <= %d", maxReplicas)})
		}
		if server.ReplicaPolicy != "" &&
			server.ReplicaPolicy != "round-robin" &&
			server.ReplicaPolicy != "least-connections" {
			errs = append(errs, ValidationError{prefix + ".replica_policy", "must be 'round-robin' or 'least-connections'"})
		}
		if server.Replicas > 1 && (server.IsExternal() || server.IsOpenAPI()) {
			errs = append(errs, ValidationError{prefix + ".replicas", "not supported for external URL or OpenAPI servers (already external/stateless — scale them at the HTTP tier)"})
		}

		// Autoscale validation.
		if server.Autoscale != nil {
			errs = append(errs, validateAutoscale(server, prefix)...)
		}

		// In simple mode, server.Network is ignored (per design decision)
	}

	// Resource validation
	resourceNames := make(map[string]bool)
	for i, resource := range s.Resources {
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

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateAutoscale validates the autoscale block on one MCP server. prefix is
// the YAML path of the server entry (e.g. "mcp-servers[2]") so every error
// surfaces the full dotted path for CI / --format json consumers.
func validateAutoscale(server MCPServer, prefix string) ValidationErrors {
	var errs ValidationErrors
	a := server.Autoscale
	asPrefix := prefix + ".autoscale"

	// Mutually exclusive with replicas.
	if server.Replicas > 0 {
		errs = append(errs, ValidationError{prefix, "cannot set both 'replicas' and 'autoscale' on the same server"})
	}

	// Not supported on external / openapi, matching the existing replicas rule.
	if server.IsExternal() || server.IsOpenAPI() {
		errs = append(errs, ValidationError{asPrefix, "not supported for external URL or OpenAPI servers (already external/stateless — scale them at the HTTP tier)"})
		return errs
	}

	// Bounds on required fields.
	if a.Min < 0 {
		errs = append(errs, ValidationError{asPrefix + ".min", "must be >= 0"})
	}
	if a.Max < 1 {
		errs = append(errs, ValidationError{asPrefix + ".max", "must be >= 1"})
	}
	if a.Max > maxReplicas {
		errs = append(errs, ValidationError{asPrefix + ".max", fmt.Sprintf("must be <= %d", maxReplicas)})
	}
	if a.Max >= 1 && a.Max < a.Min {
		errs = append(errs, ValidationError{asPrefix + ".max", "must be >= min"})
	}
	if !a.IdleToZero && a.Min < 1 {
		errs = append(errs, ValidationError{asPrefix + ".min", "must be >= 1 unless idle_to_zero is true"})
	}
	if a.TargetInFlight < 1 {
		errs = append(errs, ValidationError{asPrefix + ".target_in_flight", "must be >= 1"})
	}

	// Timings.
	if a.ScaleUpAfter != "" {
		d, err := time.ParseDuration(a.ScaleUpAfter)
		if err != nil {
			errs = append(errs, ValidationError{asPrefix + ".scale_up_after", fmt.Sprintf("invalid duration %q (expected e.g. \"30s\")", a.ScaleUpAfter)})
		} else if d < 10*time.Second {
			errs = append(errs, ValidationError{asPrefix + ".scale_up_after", "must be >= 10s"})
		}
	}
	if a.ScaleDownAfter != "" {
		d, err := time.ParseDuration(a.ScaleDownAfter)
		if err != nil {
			errs = append(errs, ValidationError{asPrefix + ".scale_down_after", fmt.Sprintf("invalid duration %q (expected e.g. \"5m\")", a.ScaleDownAfter)})
		} else if d < time.Minute {
			errs = append(errs, ValidationError{asPrefix + ".scale_down_after", "must be >= 1m"})
		}
	}

	// Warm pool constraints.
	if a.WarmPool < 0 {
		errs = append(errs, ValidationError{asPrefix + ".warm_pool", "must be >= 0"})
	}
	if a.Max >= 1 && a.Min+a.WarmPool > a.Max {
		errs = append(errs, ValidationError{asPrefix + ".warm_pool", "min + warm_pool must be <= max"})
	}

	return errs
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
