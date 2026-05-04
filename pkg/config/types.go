package config

import (
	"fmt"
	"time"
)

// Stack represents the complete gridctl configuration.
type Stack struct {
	Version    string           `yaml:"version"`
	Name       string           `yaml:"name"`
	Extends    string           `yaml:"extends,omitempty"` // Path to a parent stack file for composition
	Gateway    *GatewayConfig   `yaml:"gateway,omitempty"`
	Logging    *LoggingConfig   `yaml:"logging,omitempty"`
	Telemetry  *TelemetryConfig `yaml:"telemetry,omitempty"` // Opt-in disk persistence for logs/metrics/traces
	Secrets    *Secrets         `yaml:"secrets,omitempty"`   // Variable set references
	Network    Network          `yaml:"network"`             // Single network (simple mode)
	Networks   []Network        `yaml:"networks,omitempty"`  // Multiple networks (advanced mode)
	MCPServers []MCPServer      `yaml:"mcp-servers"`
	Resources  []Resource       `yaml:"resources,omitempty"`
}

// LoggingConfig configures log file output with automatic rotation.
type LoggingConfig struct {
	// File is the path to the log file. When set, logs are written to both the
	// in-memory ring buffer (web UI) and this file simultaneously.
	File string `yaml:"file,omitempty" json:"file,omitempty"`
	// MaxSizeMB is the maximum log file size in megabytes before rotation (default: 100).
	MaxSizeMB int `yaml:"maxSizeMB,omitempty" json:"maxSizeMB,omitempty"`
	// MaxAgeDays is the maximum number of days to retain old log files (default: 7).
	MaxAgeDays int `yaml:"maxAgeDays,omitempty" json:"maxAgeDays,omitempty"`
	// MaxBackups is the maximum number of compressed old log files to keep (default: 3).
	MaxBackups int `yaml:"maxBackups,omitempty" json:"maxBackups,omitempty"`
}

// TelemetryConfig configures opt-in disk persistence for the three signals
// gridctl already captures (logs, metrics, traces). All fields are optional;
// when the block is omitted entirely, every signal stays ephemeral (today's
// behavior). Per-server overrides on MCPServer.Telemetry can flip individual
// signals on or off relative to these defaults.
//
// Stack-global Persist fields are plain bool (binary on/off). Per-server
// MCPServerPersistence fields are *bool to express tri-state inheritance —
// see MCPServerTelemetry.
type TelemetryConfig struct {
	// Persist names which signals are written to disk by default. Per-server
	// blocks can override individual signals.
	Persist TelemetryPersistence `yaml:"persist,omitempty" json:"persist,omitempty"`
	// Retention controls lumberjack rotation for every persisted signal file.
	// SetDefaults fills sensible defaults when this block is omitted.
	Retention *RetentionConfig `yaml:"retention,omitempty" json:"retention,omitempty"`
}

// TelemetryPersistence is the stack-global signal toggle. Stack-global is
// binary (a bool) — the per-server override carries the tri-state.
type TelemetryPersistence struct {
	Logs    bool `yaml:"logs,omitempty" json:"logs,omitempty"`
	Metrics bool `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	Traces  bool `yaml:"traces,omitempty" json:"traces,omitempty"`
}

// RetentionConfig controls lumberjack rotation for persisted telemetry files.
// One block per stack — per-signal retention is intentionally out of scope at
// MVP. Defaults: 100MB / 5 backups / 7d. YAML tags use snake_case to match the
// AutoscaleConfig precedent for control-plane structs (LoggingConfig uses
// camelCase, but is closer to a runtime-rotation knob than a control-plane
// resource).
type RetentionConfig struct {
	MaxSizeMB  int `yaml:"max_size_mb,omitempty" json:"max_size_mb,omitempty"`
	MaxBackups int `yaml:"max_backups,omitempty" json:"max_backups,omitempty"`
	MaxAgeDays int `yaml:"max_age_days,omitempty" json:"max_age_days,omitempty"`
}

// MCPServerTelemetry holds per-server telemetry persistence overrides. Each
// *bool field uses tri-state semantics: nil = inherit stack-global, &true =
// explicitly persist, &false = explicitly do not persist (overrides stack
// global). Never default these to &false in SetDefaults — that would collapse
// inherit and explicit-off into the same value.
type MCPServerTelemetry struct {
	Persist MCPServerPersistence `yaml:"persist,omitempty" json:"persist,omitempty"`
}

// MCPServerPersistence is the *bool tri-state mirror of TelemetryPersistence.
type MCPServerPersistence struct {
	Logs    *bool `yaml:"logs,omitempty" json:"logs,omitempty"`
	Metrics *bool `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	Traces  *bool `yaml:"traces,omitempty" json:"traces,omitempty"`
}

// Secrets configures automatic secret injection from variable sets.
type Secrets struct {
	Sets []string `yaml:"sets,omitempty" json:"sets,omitempty"`
}

// TracingConfig configures distributed tracing for the gateway.
type TracingConfig struct {
	// Enabled controls whether tracing is active. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Sampling is the head-based sampling rate [0.0, 1.0]. Default: 1.0.
	Sampling float64 `yaml:"sampling,omitempty" json:"sampling,omitempty"`
	// Retention is how long completed traces are kept in memory (e.g. "24h"). Default: "24h".
	Retention string `yaml:"retention,omitempty" json:"retention,omitempty"`
	// Export selects an exporter: "otlp" or "" (none).
	Export string `yaml:"export,omitempty" json:"export,omitempty"`
	// Endpoint is the OTLP endpoint URL (e.g. "http://localhost:4318").
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
}

// GatewayConfig holds optional gateway-level configuration.
type GatewayConfig struct {
	// AllowedOrigins lists origins for CORS.
	// When not set, defaults to ["*"] (allow all) for backward compatibility.
	// Set explicit origins to restrict cross-origin access.
	AllowedOrigins []string    `yaml:"allowed_origins,omitempty"`
	Auth           *AuthConfig `yaml:"auth,omitempty"`

	// CodeMode controls whether the gateway replaces individual tool definitions
	// with two meta-tools (search + execute). Values: "off" (default), "on".
	// Experimental: may change without notice.
	CodeMode string `yaml:"code_mode,omitempty"`
	// CodeModeTimeout is the execution timeout in seconds (default: 30).
	// Experimental: may change without notice.
	CodeModeTimeout int `yaml:"code_mode_timeout,omitempty"`

	// OutputFormat sets the default output format for tool call results.
	// Values: "json" (default), "toon", "csv", "text".
	// Per-server output_format overrides this value.
	OutputFormat string `yaml:"output_format,omitempty"`

	// MaxToolResultBytes sets the maximum size of a tool result in bytes before truncation.
	// Results exceeding this limit are truncated with a suffix indicating the original size.
	// Default: 65536 (64KB). Set to 0 to use the default.
	MaxToolResultBytes int `yaml:"maxToolResultBytes,omitempty" json:"maxToolResultBytes,omitempty"`

	// Tracing configures distributed tracing. When nil, tracing is enabled with defaults.
	Tracing *TracingConfig `yaml:"tracing,omitempty" json:"tracing,omitempty"`

	// Security configures security features such as schema pinning. When nil, defaults apply.
	Security *GatewaySecurityConfig `yaml:"security,omitempty" json:"security,omitempty"`

	// Tokenizer selects the token counting strategy.
	// Values: "embedded" (default) uses the cl100k_base BPE vocabulary (pure Go, no network).
	// "api" uses Anthropic's count_tokens endpoint for exact counts — Anthropic-specific,
	// requires network access and an API key, wrong for non-Anthropic model routing.
	Tokenizer string `yaml:"tokenizer,omitempty"`
	// TokenizerAPIKey overrides ANTHROPIC_API_KEY for the api tokenizer mode.
	// When unset, the api tokenizer falls back to the ANTHROPIC_API_KEY environment variable.
	TokenizerAPIKey string `yaml:"tokenizer_api_key,omitempty"`
}

// GatewaySecurityConfig holds gateway-level security settings.
type GatewaySecurityConfig struct {
	// SchemaPinning configures TOFU schema pinning for MCP tool definitions.
	SchemaPinning *SchemaPinningConfig `yaml:"schema_pinning,omitempty" json:"schema_pinning,omitempty"`
}

// SchemaPinningConfig controls the schema pinning feature.
type SchemaPinningConfig struct {
	// Enabled controls whether schema pinning is active. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Action is the response when drift is detected: "warn" (default) or "block".
	// warn: log a structured diff and continue serving.
	// block: reject all tool calls from the drifted server until approved.
	Action string `yaml:"action,omitempty" json:"action,omitempty"`
}

// AuthConfig configures gateway authentication.
// When configured, all requests (except /health and /ready) must include a valid token.
type AuthConfig struct {
	// Type is the auth mechanism: "bearer" or "api_key".
	Type string `yaml:"type"`
	// Token is the expected token value (supports env var references via $VAR or ${VAR}).
	Token string `yaml:"token"`
	// Header is the header name for api_key auth (default: "Authorization").
	Header string `yaml:"header,omitempty"`
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
	Tools        []string          `yaml:"tools,omitempty"`          // Tool whitelist (empty = all tools exposed)
	OutputFormat string            `yaml:"output_format,omitempty"`  // Output format override: "json", "toon", "csv", "text"
	PinSchemas   *bool             `yaml:"pin_schemas,omitempty"`    // Override gateway schema pinning for this server (nil = inherit)
	// ReadyTimeout overrides the HTTP/SSE readiness wait for container-based servers.
	// Accepts any time.Duration string (e.g. "60s", "2m"). Empty/"0" inherits the gateway default (30s).
	// Ignored for stdio, local process, SSH, OpenAPI, and external transports.
	ReadyTimeout string `yaml:"ready_timeout,omitempty"`

	// PingTimeout overrides the per-ping deadline used by the gateway health monitor.
	// Accepts any time.Duration string (e.g. "10s"). Empty/"0" inherits DefaultPingTimeout (5s).
	// Tune this for slow upstreams (e.g. HTTP servers with many tools) where the
	// 5s default can flake under autoscale spawn load.
	PingTimeout string `yaml:"ping_timeout,omitempty"`

	// Replicas is the number of independent processes to spawn for this server.
	// Defaults to 1. Values >1 load-balance JSON-RPC tool calls across replicas
	// using ReplicaPolicy. Not supported for external URL or OpenAPI transports.
	Replicas int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// ReplicaPolicy selects the dispatch policy when Replicas > 1.
	// Valid values: "round-robin" (default), "least-connections".
	ReplicaPolicy string `yaml:"replica_policy,omitempty" json:"replica_policy,omitempty"`

	// Autoscale, when set, replaces the static Replicas count with reactive
	// autoscaling bounded by Min and Max. Mutually exclusive with Replicas.
	// Not supported on external URL or OpenAPI transports.
	Autoscale *AutoscaleConfig `yaml:"autoscale,omitempty" json:"autoscale,omitempty"`

	// Telemetry, when set, overrides stack-global telemetry persistence for
	// this server. nil fields inherit; *bool fields explicitly opt in or out.
	Telemetry *MCPServerTelemetry `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
}

// AutoscaleConfig controls reactive autoscaling of a ReplicaSet. All fields are
// optional at the YAML layer only in the sense that SetDefaults fills missing
// timings; Min, Max, and TargetInFlight are required for the block to validate.
type AutoscaleConfig struct {
	// Min is the minimum number of healthy replicas to maintain.
	// >= 0. Must be >= 1 when IdleToZero is false.
	Min int `yaml:"min" json:"min"`
	// Max is the upper bound on replica count. >= 1, >= Min, <= 32.
	Max int `yaml:"max" json:"max"`
	// TargetInFlight is the per-replica in-flight request count the scaler
	// tries to hold the median at or below. >= 1.
	TargetInFlight int `yaml:"target_in_flight" json:"target_in_flight"`
	// ScaleUpAfter is how long the window median must exceed the target
	// before spawning a replica. Default 30s. Minimum 10s.
	ScaleUpAfter string `yaml:"scale_up_after,omitempty" json:"scale_up_after,omitempty"`
	// ScaleDownAfter is how long the window median must be below the target
	// before reaping a replica. Default 5m. Minimum 1m.
	ScaleDownAfter string `yaml:"scale_down_after,omitempty" json:"scale_down_after,omitempty"`
	// WarmPool keeps this many extra idle-ready replicas above the load-derived
	// target at all times. Default 0. Must satisfy Min + WarmPool <= Max.
	WarmPool int `yaml:"warm_pool,omitempty" json:"warm_pool,omitempty"`
	// IdleToZero allows the scaler to reap every replica after a sustained
	// idle. Min may be 0 only when IdleToZero is true. Default false.
	IdleToZero bool `yaml:"idle_to_zero,omitempty" json:"idle_to_zero,omitempty"`
}

// ResolvedScaleUpAfter parses ScaleUpAfter; returns 30s when unset or invalid.
func (a *AutoscaleConfig) ResolvedScaleUpAfter() time.Duration {
	if a == nil || a.ScaleUpAfter == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(a.ScaleUpAfter)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}

// ResolvedScaleDownAfter parses ScaleDownAfter; returns 5m when unset or invalid.
func (a *AutoscaleConfig) ResolvedScaleDownAfter() time.Duration {
	if a == nil || a.ScaleDownAfter == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(a.ScaleDownAfter)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// ResolvedReadyTimeout parses ReadyTimeout; returns 0 when unset or invalid
// so the gateway falls back to its default.
func (s *MCPServer) ResolvedReadyTimeout() time.Duration {
	if s.ReadyTimeout == "" {
		return 0
	}
	d, err := time.ParseDuration(s.ReadyTimeout)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// ResolvedPingTimeout parses PingTimeout; returns 0 when unset or invalid so
// the gateway falls back to DefaultPingTimeout (5s).
func (s *MCPServer) ResolvedPingTimeout() time.Duration {
	if s.PingTimeout == "" {
		return 0
	}
	d, err := time.ParseDuration(s.PingTimeout)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// OpenAPIConfig defines an MCP server backed by an OpenAPI specification.
// The spec is parsed and each operation becomes an MCP tool.
type OpenAPIConfig struct {
	Spec       string            `yaml:"spec"`                 // URL or local file path to OpenAPI spec (JSON or YAML)
	BaseURL    string            `yaml:"baseUrl,omitempty"`    // Override the server URL from the spec
	Auth       *OpenAPIAuth      `yaml:"auth,omitempty"`       // Authentication configuration
	TLS        *OpenAPITLS       `yaml:"tls,omitempty"`        // TLS/mTLS configuration (transport-layer)
	Operations *OperationsFilter `yaml:"operations,omitempty"` // Filter which operations become tools
}

// OpenAPIAuth defines authentication for OpenAPI HTTP requests.
type OpenAPIAuth struct {
	Type     string `yaml:"type"`               // "bearer", "header", "query", "oauth2", or "basic"
	TokenEnv string `yaml:"tokenEnv,omitempty"` // Env var name containing bearer token (for type: bearer)
	Header   string `yaml:"header,omitempty"`   // Header name (for type: header, e.g., "X-API-Key")
	ValueEnv string `yaml:"valueEnv,omitempty"` // Env var name containing header value (for type: header or query)

	// Query param auth (type: query)
	ParamName string `yaml:"paramName,omitempty"` // Query parameter name (for type: query)

	// OAuth2 client credentials (type: oauth2)
	ClientIdEnv     string   `yaml:"clientIdEnv,omitempty"`     // Env var name containing OAuth2 client ID
	ClientSecretEnv string   `yaml:"clientSecretEnv,omitempty"` // Env var name containing OAuth2 client secret
	TokenUrl        string   `yaml:"tokenUrl,omitempty"`        // OAuth2 token endpoint URL
	Scopes          []string `yaml:"scopes,omitempty"`          // OAuth2 scopes to request

	// Basic auth (type: basic)
	UsernameEnv string `yaml:"usernameEnv,omitempty"` // Env var name containing username
	PasswordEnv string `yaml:"passwordEnv,omitempty"` // Env var name containing password
}

// OpenAPITLS defines TLS/mTLS configuration for OpenAPI HTTP connections.
// This is transport-layer config and can be combined with any auth type.
type OpenAPITLS struct {
	CertFile           string `yaml:"certFile,omitempty"`           // Client certificate file path (required for mTLS)
	KeyFile            string `yaml:"keyFile,omitempty"`            // Client private key file path (required for mTLS)
	CaFile             string `yaml:"caFile,omitempty"`             // Custom CA certificate file path
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify,omitempty"` // Skip server certificate verification (dangerous)
}

// OperationsFilter defines which OpenAPI operations to include or exclude.
// Only one of Include or Exclude should be specified.
type OperationsFilter struct {
	Include []string `yaml:"include,omitempty"` // Operation IDs to include (whitelist)
	Exclude []string `yaml:"exclude,omitempty"` // Operation IDs to exclude (blacklist)
}

// SSHConfig defines SSH connection parameters for remote MCP servers.
type SSHConfig struct {
	Host           string `yaml:"host"`                        // Required: hostname or IP address
	User           string `yaml:"user"`                        // Required: SSH username
	Port           int    `yaml:"port,omitempty"`              // Optional: SSH port (default 22)
	IdentityFile   string `yaml:"identityFile,omitempty"`      // Optional: path to SSH private key
	KnownHostsFile string `yaml:"knownHostsFile,omitempty"`    // Optional: path to known_hosts file; enables StrictHostKeyChecking=yes
	JumpHost       string `yaml:"jumpHost,omitempty"`          // Optional: bastion/jump host ([user@]host[:port])
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

// IsContainerBased returns true if this MCP server requires a container runtime.
func (s *MCPServer) IsContainerBased() bool {
	return !s.IsExternal() && !s.IsLocalProcess() && !s.IsSSH() && !s.IsOpenAPI()
}

// PersistLogs reports whether log persistence is effectively enabled for this
// server. An explicit per-server *bool override wins; otherwise the stack-
// global default is returned. Returns false when both stack and server are
// nil.
func (s *MCPServer) PersistLogs(stack *Stack) bool {
	if s != nil && s.Telemetry != nil && s.Telemetry.Persist.Logs != nil {
		return *s.Telemetry.Persist.Logs
	}
	return stack != nil && stack.Telemetry != nil && stack.Telemetry.Persist.Logs
}

// PersistMetrics — see PersistLogs for inheritance semantics.
func (s *MCPServer) PersistMetrics(stack *Stack) bool {
	if s != nil && s.Telemetry != nil && s.Telemetry.Persist.Metrics != nil {
		return *s.Telemetry.Persist.Metrics
	}
	return stack != nil && stack.Telemetry != nil && stack.Telemetry.Persist.Metrics
}

// PersistTraces — see PersistLogs for inheritance semantics.
func (s *MCPServer) PersistTraces(stack *Stack) bool {
	if s != nil && s.Telemetry != nil && s.Telemetry.Persist.Traces != nil {
		return *s.Telemetry.Persist.Traces
	}
	return stack != nil && stack.Telemetry != nil && stack.Telemetry.Persist.Traces
}

// Source defines how to build an MCP server from source code.
type Source struct {
	Type       string      `yaml:"type"` // "git" or "local"
	URL        string      `yaml:"url,omitempty"`
	Ref        string      `yaml:"ref,omitempty"`
	Path       string      `yaml:"path,omitempty"`
	Dockerfile string      `yaml:"dockerfile,omitempty"`
	Auth       *SourceAuth `yaml:"auth,omitempty"`
}

// SourceAuth is the declarative auth block on an MCP server git source. Raw
// tokens must NOT appear here — use CredentialRef (e.g. "${vault:GIT_TOKEN}")
// which is resolved against the live vault at clone time. Never add a Token
// field to this struct: anything with a yaml tag here gets persisted to disk.
type SourceAuth struct {
	Method        string `yaml:"method,omitempty"`         // "", "none", "token", "ssh-agent", "ssh-key"
	CredentialRef string `yaml:"credential_ref,omitempty"` // e.g. "${vault:GIT_TOKEN}" — resolved on every clone/fetch
	SSHUser       string `yaml:"ssh_user,omitempty"`       // defaults to "git" when empty
	SSHKeyPath    string `yaml:"ssh_key_path,omitempty"`   // required for method "ssh-key"
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

// NeedsContainerRuntime returns true if the stack has workloads requiring a container runtime.
func (s *Stack) NeedsContainerRuntime() bool {
	if len(s.Resources) > 0 {
		return true
	}
	for _, srv := range s.MCPServers {
		if srv.IsContainerBased() {
			return true
		}
	}
	return false
}

// ContainerWorkloads returns human-readable descriptions of workloads that require a container runtime.
func (s *Stack) ContainerWorkloads() []string {
	var workloads []string
	for _, srv := range s.MCPServers {
		if srv.IsContainerBased() {
			detail := "container"
			if srv.Image != "" {
				detail = "image: " + srv.Image
			} else if srv.Source != nil {
				detail = "source: " + srv.Source.Type
			}
			workloads = append(workloads, fmt.Sprintf("  - %-20s (%s)", srv.Name, detail))
		}
	}
	for _, res := range s.Resources {
		workloads = append(workloads, fmt.Sprintf("  - %-20s (resource)", res.Name))
	}
	return workloads
}

// NonContainerWorkloads returns human-readable descriptions of workloads that work without a container runtime.
func (s *Stack) NonContainerWorkloads() []string {
	var workloads []string
	for _, srv := range s.MCPServers {
		var kind string
		switch {
		case srv.IsExternal():
			kind = "external"
		case srv.IsLocalProcess():
			kind = "local process"
		case srv.IsSSH():
			kind = "ssh"
		case srv.IsOpenAPI():
			kind = "openapi"
		default:
			continue
		}
		workloads = append(workloads, fmt.Sprintf("  - %-20s (%s)", srv.Name, kind))
	}
	return workloads
}

// SetDefaults applies default values to the stack.
func (s *Stack) SetDefaults() {
	if s.Version == "" {
		s.Version = "1"
	}

	if s.Gateway != nil && s.Gateway.Tokenizer == "" {
		s.Gateway.Tokenizer = "embedded"
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
		// When autoscale is configured the scaler owns replica count — leave
		// Replicas at 0 so downstream code can distinguish static from elastic.
		if s.MCPServers[i].Autoscale == nil && s.MCPServers[i].Replicas <= 0 {
			s.MCPServers[i].Replicas = 1
		}
		if s.MCPServers[i].ReplicaPolicy == "" {
			s.MCPServers[i].ReplicaPolicy = "round-robin"
		}
	}

	// Telemetry retention defaults. Only fill when the stack opts in to
	// telemetry; never synthesize a Telemetry block on stacks that omit one,
	// since that would change parsed-config equality vs today's behavior.
	if s.Telemetry != nil {
		if s.Telemetry.Retention == nil {
			s.Telemetry.Retention = &RetentionConfig{}
		}
		if s.Telemetry.Retention.MaxSizeMB == 0 {
			s.Telemetry.Retention.MaxSizeMB = 100
		}
		if s.Telemetry.Retention.MaxBackups == 0 {
			s.Telemetry.Retention.MaxBackups = 5
		}
		if s.Telemetry.Retention.MaxAgeDays == 0 {
			s.Telemetry.Retention.MaxAgeDays = 7
		}
	}
}

