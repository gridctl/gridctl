package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

// scanIgnoreCodeRe matches poisoning-scan finding codes ("P001"), case-insensitive
// to mirror the tolerant matching in pins.FilterFindings.
var scanIgnoreCodeRe = regexp.MustCompile(`(?i)^p[0-9]{3}$`)

// maxReplicas is the sanity cap on MCPServer.Replicas. Values above this
// are almost certainly a config error; the cap also bounds per-server
// fan-out costs for things like health checking and least-connections scans.
const maxReplicas = 32

// telemetryWarnBytesPerServer is the soft cap above which a stack-wide
// retention block triggers a warning. 5 GiB matches the prompt and is large
// enough that legitimate tuning rarely crosses it; users who explicitly
// configure higher retention still get a load-time warning rather than a
// hard reject.
const telemetryWarnBytesPerServer = 5 * 1024 * 1024 * 1024

// Hard upper bounds on retention values. These exist to defuse arithmetic
// overflow on the per-server-bytes math and to catch obvious typos
// (e.g. an extra zero). They are intentionally generous; the soft cap above
// will warn long before these trip.
const (
	telemetryMaxSizeMBHardCap  = 1 << 20 // 1 TiB per file
	telemetryMaxBackupsHardCap = 1024
	telemetryMaxAgeDaysHardCap = 365 * 10 // 10 years
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

	// Gateway schema pinning action validation. Unknown values must be
	// rejected: the gateway only honors "block", so a typo would silently
	// downgrade a security policy to warn.
	if s.Gateway != nil && s.Gateway.Security != nil && s.Gateway.Security.SchemaPinning != nil {
		sp := s.Gateway.Security.SchemaPinning
		if sp.Action != "" && sp.Action != "warn" && sp.Action != "block" {
			errs = append(errs, ValidationError{"gateway.security.schema_pinning.action", "must be one of: warn, block"})
		}
		// scan_ignore entries must look like finding codes: a typo would
		// silently suppress nothing while the user believes it suppresses
		// a heuristic.
		for _, code := range sp.ScanIgnore {
			if !scanIgnoreCodeRe.MatchString(code) {
				errs = append(errs, ValidationError{"gateway.security.schema_pinning.scan_ignore", fmt.Sprintf("invalid finding code %q: want the form P001", code)})
			}
		}
	}

	// Telemetry retention validation
	if s.Telemetry != nil && s.Telemetry.Retention != nil {
		errs = append(errs, validateTelemetryRetention(s.Telemetry.Retention)...)
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

		// Downstream auth only applies to external URL servers
		if server.Auth != nil && !server.IsExternal() {
			errs = append(errs, ValidationError{prefix + ".auth", "only valid for external URL servers"})
		}

		// External server validation (URL-only)
		if server.IsExternal() {
			if server.Auth != nil {
				errs = append(errs, validateServerAuth(server.Auth, prefix+".auth")...)
			}
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

		// ping_timeout validation: must parse as a duration and be non-negative.
		// Applies to every pingable transport (HTTP, SSE, stdio, local process,
		// SSH, OpenAPI). Empty is valid and falls back to DefaultPingTimeout.
		if server.PingTimeout != "" {
			d, err := time.ParseDuration(server.PingTimeout)
			if err != nil {
				errs = append(errs, ValidationError{prefix + ".ping_timeout", fmt.Sprintf("invalid duration %q (expected e.g. \"10s\")", server.PingTimeout)})
			} else if d < 0 {
				errs = append(errs, ValidationError{prefix + ".ping_timeout", "must be non-negative"})
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

	// Per-client access scoping validation
	errs = append(errs, validateClients(s, serverNames)...)

	// Budget and rate limit validation
	errs = append(errs, validateLimits(s, serverNames)...)

	// Tool group validation
	errs = append(errs, validateGroups(s, serverNames)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateClients checks the optional `clients:` access block. It fails when
// the default policy is invalid or a profile references an unknown server —
// directly or via a tool prefix — so a misconfigured allow-list surfaces as a
// clear error rather than silently granting or denying access. serverNames is
// the set of declared MCP server names.
//
// Tool references are validated structurally: each must be a prefixed
// "server__tool" name whose server is declared. The existence of the specific
// tool cannot be checked at load time (downstream tools are only known once
// servers are running), so an unknown tool on a known server is not a load-time
// error; it simply never appears in the client's effective scope.
func validateClients(s *Stack, serverNames map[string]bool) ValidationErrors {
	var errs ValidationErrors
	if s.Clients == nil {
		return errs
	}

	switch s.Clients.Default {
	case "", "deny", "allow":
		// valid
	default:
		errs = append(errs, ValidationError{"clients.default", "must be 'allow' or 'deny'"})
	}

	for name, profile := range s.Clients.Profiles {
		prefix := fmt.Sprintf("clients.profiles[%s]", name)
		if name == "" {
			errs = append(errs, ValidationError{"clients.profiles", "profile name must not be empty"})
		}
		for i, server := range profile.Servers {
			if !serverNames[server] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.servers[%d]", prefix, i),
					fmt.Sprintf("references unknown MCP server '%s'", server),
				})
			}
		}
		for i, tool := range profile.Tools {
			server, _, ok := splitPrefixedToolName(tool)
			if !ok {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.tools[%d]", prefix, i),
					fmt.Sprintf("tool '%s' must be a prefixed name (server__tool)", tool),
				})
				continue
			}
			if !serverNames[server] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.tools[%d]", prefix, i),
					fmt.Sprintf("tool '%s' references unknown MCP server '%s'", tool, server),
				})
			}
		}
	}
	return errs
}

// validateLimits checks the optional `limits:` block. Each entry must scope
// to exactly one of client/server/tool; server and tool scopes must reference
// declared servers (tool existence itself is a runtime property, mirroring
// validateClients); numeric fields must be in range; and duplicate scope+key
// pairs within a list are rejected because two entries on the same scope
// would race for the same counter.
func validateLimits(s *Stack, serverNames map[string]bool) ValidationErrors {
	var errs ValidationErrors
	if s.Limits == nil {
		return errs
	}

	// validateScope checks the shared one-of scope contract and returns the
	// dedupe key ("" when the scope itself is invalid). Client keys are
	// lower-slugged for deduping because the runtime normalizes them the
	// same way: "Claude Code" and "claude-code" would share one counter.
	validateScope := func(prefix, client, server, tool string) string {
		kind, key, ok := limitScopeKey(client, server, tool)
		if !ok {
			errs = append(errs, ValidationError{prefix, "must set exactly one of 'client', 'server', or 'tool'"})
			return ""
		}
		switch kind {
		case "client":
			key = slugifyLimitClientKey(key)
		case "server":
			if !serverNames[key] {
				errs = append(errs, ValidationError{prefix + ".server", fmt.Sprintf("references unknown MCP server '%s'", key)})
			}
		case "tool":
			srv, _, wellFormed := splitPrefixedToolName(key)
			if !wellFormed {
				errs = append(errs, ValidationError{prefix + ".tool", fmt.Sprintf("tool '%s' must be a prefixed name (server__tool)", key)})
			} else if !serverNames[srv] {
				errs = append(errs, ValidationError{prefix + ".tool", fmt.Sprintf("tool '%s' references unknown MCP server '%s'", key, srv)})
			}
		}
		return kind + ":" + key
	}

	seenBudgets := make(map[string]bool, len(s.Limits.Budgets))
	for i := range s.Limits.Budgets {
		b := &s.Limits.Budgets[i]
		prefix := fmt.Sprintf("limits.budgets[%d]", i)
		if scope := validateScope(prefix, b.Client, b.Server, b.Tool); scope != "" {
			if seenBudgets[scope] {
				errs = append(errs, ValidationError{prefix, fmt.Sprintf("duplicate budget for %s", scope)})
			}
			seenBudgets[scope] = true
		}
		if b.MaxUSD <= 0 {
			errs = append(errs, ValidationError{prefix + ".max_usd", "must be positive"})
		}
		switch b.Period {
		case "daily", "weekly", "monthly":
			// valid
		default:
			errs = append(errs, ValidationError{prefix + ".period", "must be 'daily', 'weekly', or 'monthly'"})
		}
		if b.WarnAtPercent < 0 || b.WarnAtPercent > 99 {
			errs = append(errs, ValidationError{prefix + ".warn_at_percent", "must be between 1 and 99 (omit to disable)"})
		}
	}

	seenRates := make(map[string]bool, len(s.Limits.RateLimits))
	for i := range s.Limits.RateLimits {
		r := &s.Limits.RateLimits[i]
		prefix := fmt.Sprintf("limits.rate_limits[%d]", i)
		if scope := validateScope(prefix, r.Client, r.Server, r.Tool); scope != "" {
			if seenRates[scope] {
				errs = append(errs, ValidationError{prefix, fmt.Sprintf("duplicate rate limit for %s", scope)})
			}
			seenRates[scope] = true
		}
		if r.CallsPerMinute <= 0 {
			errs = append(errs, ValidationError{prefix + ".calls_per_minute", "must be positive"})
		}
		if r.Burst < 0 {
			errs = append(errs, ValidationError{prefix + ".burst", "must not be negative"})
		}
	}
	return errs
}

// groupNameRe is the allowed shape for group names: URL-path-safe, short
// enough to leave room in the client-side mcp__<group>__<tool> budget.
var groupNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// groupOverrideNameRe is the allowed shape for a renamed tool: a flat name
// with no "__" (the prefix delimiter must stay unambiguous).
var groupOverrideNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// clientToolNameBudget is the hard tool-name limit clients hit after
// wrapping an entry's tools as mcp__<entry>__<tool> (the Claude API caps
// tool names at 64 characters). gridctl controls both halves of that
// string for groups, so it validates the whole budget at load time.
const clientToolNameBudget = 64

// validateGroups checks the optional `groups:` block: name shape,
// membership references, override integrity (keys must be members, renames
// flat and collision-free within the group), and the client-side tool-name
// budget. Tool existence stays a runtime property (mirroring
// validateClients); references are validated structurally against declared
// servers.
func validateGroups(s *Stack, serverNames map[string]bool) ValidationErrors {
	var errs ValidationErrors
	if len(s.Groups) == 0 {
		return errs
	}

	for name, group := range s.Groups {
		prefix := fmt.Sprintf("groups[%s]", name)
		if !groupNameRe.MatchString(name) {
			errs = append(errs, ValidationError{prefix, "group name must match ^[a-z0-9][a-z0-9_-]{0,31}$"})
		}

		if len(group.Servers) == 0 && len(group.Tools) == 0 {
			errs = append(errs, ValidationError{prefix, "must include at least one server or tool"})
		}

		memberServers := make(map[string]bool, len(group.Servers))
		for i, server := range group.Servers {
			if !serverNames[server] {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.servers[%d]", prefix, i),
					fmt.Sprintf("references unknown MCP server '%s'", server),
				})
			}
			memberServers[server] = true
		}

		memberTools := make(map[string]bool, len(group.Tools))
		checkPrefixed := func(field, tool string) (server string, ok bool) {
			server, _, wellFormed := splitPrefixedToolName(tool)
			if !wellFormed {
				errs = append(errs, ValidationError{field, fmt.Sprintf("tool '%s' must be a prefixed name (server__tool)", tool)})
				return "", false
			}
			if !serverNames[server] {
				errs = append(errs, ValidationError{field, fmt.Sprintf("tool '%s' references unknown MCP server '%s'", tool, server)})
				return "", false
			}
			return server, true
		}
		for i, tool := range group.Tools {
			if _, ok := checkPrefixed(fmt.Sprintf("%s.tools[%d]", prefix, i), tool); ok {
				memberTools[tool] = true
			}
		}
		excluded := make(map[string]bool, len(group.Exclude))
		for i, tool := range group.Exclude {
			if _, ok := checkPrefixed(fmt.Sprintf("%s.exclude[%d]", prefix, i), tool); ok {
				excluded[tool] = true
			}
		}

		// Structural emptiness: every explicit tool excluded and no server
		// inclusion left means the group can never expose anything.
		if len(memberServers) == 0 && len(memberTools) > 0 {
			remaining := 0
			for tool := range memberTools {
				if !excluded[tool] {
					remaining++
				}
			}
			if remaining == 0 {
				errs = append(errs, ValidationError{prefix, "exclude removes every included tool; the group would be empty"})
			}
		}

		// The client-side name budget is fully checkable for explicit tools:
		// entries (exposed as server__tool unless renamed). Server-wildcard
		// members stay a runtime property.
		for tool := range memberTools {
			if _, renamed := group.Overrides[tool]; renamed {
				continue // the rename's budget is checked below
			}
			if budget := len("mcp__") + len(name) + len("__") + len(tool); budget > clientToolNameBudget {
				errs = append(errs, ValidationError{
					fmt.Sprintf("%s.tools", prefix),
					fmt.Sprintf("exposed name 'mcp__%s__%s' is %d characters; clients cap tool names at %d (rename it via overrides)", name, tool, budget, clientToolNameBudget),
				})
			}
		}

		// Overrides: keys are members, renames are flat and reserved-word
		// free, exposed names are unique within the group and fit the
		// client-side budget.
		exposedNames := make(map[string]string, len(group.Overrides)) // exposed -> override key
		for key, ov := range group.Overrides {
			field := fmt.Sprintf("%s.overrides[%s]", prefix, key)
			server, _, wellFormed := splitPrefixedToolName(key)
			if !wellFormed {
				errs = append(errs, ValidationError{field, "override key must be a prefixed name (server__tool)"})
				continue
			}
			if !serverNames[server] {
				errs = append(errs, ValidationError{field, fmt.Sprintf("references unknown MCP server '%s'", server)})
				continue
			}
			isMember := (memberTools[key] || memberServers[server]) && !excluded[key]
			if !isMember {
				errs = append(errs, ValidationError{field, "override key is not a member of the group"})
			}
			if ov.Name != "" {
				if !groupOverrideNameRe.MatchString(ov.Name) || strings.Contains(ov.Name, "__") {
					errs = append(errs, ValidationError{field + ".name", "rename must match ^[a-zA-Z0-9_-]+$ and contain no '__'"})
				}
				if ov.Name == "search" || ov.Name == "execute" {
					errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("'%s' is reserved for the code-mode meta-tools", ov.Name)})
				}
				if prev, dup := exposedNames[ov.Name]; dup {
					errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("exposed name '%s' collides with override for '%s'", ov.Name, prev)})
				}
				exposedNames[ov.Name] = key
				if budget := len("mcp__") + len(name) + len("__") + len(ov.Name); budget > clientToolNameBudget {
					errs = append(errs, ValidationError{field + ".name", fmt.Sprintf("exposed name 'mcp__%s__%s' is %d characters; clients cap tool names at %d", name, ov.Name, budget, clientToolNameBudget)})
				}
			}
		}
		// A rename must not equal the unprefixed tail of another explicit
		// member tool on the same server: the sandbox builds server__<name>
		// forms, and a live sibling of that literal name would be shadowed.
		for exposed, key := range exposedNames {
			renameServer, _, _ := splitPrefixedToolName(key)
			for tool := range memberTools {
				if tool == key {
					continue
				}
				srv, tail, ok := splitPrefixedToolName(tool)
				if ok && srv == renameServer && tail == exposed {
					errs = append(errs, ValidationError{
						fmt.Sprintf("%s.overrides[%s].name", prefix, key),
						fmt.Sprintf("exposed name '%s' collides with member tool '%s' on the same server", exposed, tool),
					})
				}
			}
		}
	}
	return errs
}

// slugifyLimitClientKey lower-slugs a limits client key the way the runtime
// normalizes client identities (lowercase, separators collapsed to hyphens),
// without importing pkg/mcp — the same import-cycle rationale as
// splitPrefixedToolName. Used only for duplicate detection; alias-table
// variants the runtime also folds (e.g. "claude-ai" vs "claude-desktop")
// are caught at policy compile time with a WARN instead.
func slugifyLimitClientKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	prevSep := true
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.':
			b.WriteRune(r)
			prevSep = false
		default:
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// splitPrefixedToolName splits a "server__tool" name into its server and tool
// parts without importing pkg/mcp (which would create an import cycle). It is
// intentionally stricter than mcp.ParsePrefixedTool — it also rejects an empty
// server or tool half — so config validation rejects malformed names that the
// looser runtime parser would otherwise accept. Returns ok=false when the name
// is not a well-formed prefixed name.
func splitPrefixedToolName(prefixed string) (server, tool string, ok bool) {
	const delim = "__"
	idx := strings.Index(prefixed, delim)
	if idx <= 0 || idx+len(delim) >= len(prefixed) {
		return "", "", false
	}
	return prefixed[:idx], prefixed[idx+len(delim):], true
}

// validateAutoscale validates the autoscale block on one MCP server. prefix is
// the YAML path of the server entry (e.g. "mcp-servers[2]") so every error
// surfaces the full dotted path for CI / --format json consumers.
// validateServerAuth checks the downstream auth block of an external URL
// server. Fields that belong to a different auth type are rejected so a typo
// (e.g. a bearer token on type: oauth) fails loudly instead of being ignored.
func validateServerAuth(a *ServerAuth, prefix string) ValidationErrors {
	var errs ValidationErrors

	switch a.Type {
	case "bearer":
		if a.Token == "" {
			errs = append(errs, ValidationError{prefix + ".token", "required when type is 'bearer'"})
		}
	case "header":
		if a.Header == "" {
			errs = append(errs, ValidationError{prefix + ".header", "required when type is 'header'"})
		}
		if a.Value == "" {
			errs = append(errs, ValidationError{prefix + ".value", "required when type is 'header'"})
		}
	case "oauth":
		if a.ClientSecret != "" && a.ClientID == "" {
			errs = append(errs, ValidationError{prefix + ".client_id", "required when client_secret is set"})
		}
	case "":
		errs = append(errs, ValidationError{prefix + ".type", "is required"})
	default:
		errs = append(errs, ValidationError{prefix + ".type", "must be 'bearer', 'header', or 'oauth'"})
	}

	if a.Type != "bearer" && a.Token != "" {
		errs = append(errs, ValidationError{prefix + ".token", "only valid when type is 'bearer'"})
	}
	if a.Type != "header" && (a.Header != "" || a.Value != "") {
		errs = append(errs, ValidationError{prefix + ".header", "header/value only valid when type is 'header'"})
	}
	if a.Type != "oauth" && (len(a.Scopes) > 0 || a.ClientID != "" || a.ClientSecret != "") {
		errs = append(errs, ValidationError{prefix + ".scopes", "scopes/client_id/client_secret only valid when type is 'oauth'"})
	}

	return errs
}

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

// validateTelemetryRetention enforces hard bounds on the telemetry.retention
// block and emits a soft warning when the worst-case footprint per server
// exceeds telemetryWarnBytesPerServer. Hard bounds: every field must be a
// positive integer; max_size_mb must be >= 1.
func validateTelemetryRetention(r *RetentionConfig) ValidationErrors {
	var errs ValidationErrors
	const prefix = "telemetry.retention"

	if r.MaxSizeMB < 1 {
		errs = append(errs, ValidationError{prefix + ".max_size_mb", "must be >= 1"})
	} else if r.MaxSizeMB > telemetryMaxSizeMBHardCap {
		errs = append(errs, ValidationError{prefix + ".max_size_mb", fmt.Sprintf("must be <= %d", telemetryMaxSizeMBHardCap)})
	}
	if r.MaxBackups < 1 {
		errs = append(errs, ValidationError{prefix + ".max_backups", "must be >= 1"})
	} else if r.MaxBackups > telemetryMaxBackupsHardCap {
		errs = append(errs, ValidationError{prefix + ".max_backups", fmt.Sprintf("must be <= %d", telemetryMaxBackupsHardCap)})
	}
	if r.MaxAgeDays < 1 {
		errs = append(errs, ValidationError{prefix + ".max_age_days", "must be >= 1"})
	} else if r.MaxAgeDays > telemetryMaxAgeDaysHardCap {
		errs = append(errs, ValidationError{prefix + ".max_age_days", fmt.Sprintf("must be <= %d", telemetryMaxAgeDaysHardCap)})
	}

	// Soft warning. Only meaningful when the hard bounds are satisfied so the
	// product can't underflow into a misleading number.
	if len(errs) == 0 {
		bytesPerServer := int64(r.MaxSizeMB) * 1024 * 1024 * int64(r.MaxBackups)
		if bytesPerServer > telemetryWarnBytesPerServer {
			slog.Warn("telemetry retention exceeds soft cap; per-server footprint may grow large",
				"max_size_mb", r.MaxSizeMB,
				"max_backups", r.MaxBackups,
				"per_server_bytes", bytesPerServer,
				"soft_cap_bytes", telemetryWarnBytesPerServer,
			)
		}
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
