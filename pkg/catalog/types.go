// Package catalog provides the MCP server catalog behind `gridctl search`
// and `gridctl add`: a small embedded set of curated entries plus an
// on-demand, disk-cached consumer of the official MCP Registry API
// (registry.modelcontextprotocol.io, API v0.1). Entries map onto
// config.MCPServer blocks through Entry.Server; nothing in this package
// touches stack.yaml itself.
package catalog

// Source tiers. Curated entries ship embedded in the binary and are vetted
// by hand; registry entries come from the official MCP Registry, which does
// no code scanning, and must not be presented as vetted.
const (
	TierCurated  = "curated"
	TierRegistry = "registry"
)

// Entry statuses mirror the MCP Registry lifecycle. Deleted entries are
// filtered out before they reach callers, so only these two appear.
const (
	StatusActive     = "active"
	StatusDeprecated = "deprecated"
)

// Install spec types.
const (
	InstallImage   = "image"   // container image (OCI)
	InstallCommand = "command" // local process (npx, uvx, ...)
	InstallURL     = "url"     // external remote server
)

// Entry is one installable catalog server. Curated entries use a short
// install name ("github"); registry entries use the full reverse-DNS
// registry name ("io.github.user/weather").
type Entry struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	// Tier is TierCurated or TierRegistry, assigned at load time.
	Tier string `json:"tier,omitempty"`
	// Namespace links a curated entry to its MCP Registry name so merged
	// search results dedupe (the curated entry wins). Empty for registry
	// entries, whose Name is already the registry name.
	Namespace  string `json:"namespace,omitempty"`
	Homepage   string `json:"homepage,omitempty"`
	Repository string `json:"repository,omitempty"`
	// Status is StatusActive or StatusDeprecated. Empty means active.
	Status  string  `json:"status,omitempty"`
	Install Install `json:"install"`
	Inputs  []Input `json:"inputs,omitempty"`

	// Unsupported records the registry package type when no supported
	// install shape could be derived (mcpb, nuget, cargo, templated URLs).
	// Such entries still appear in search; Entry.Server rejects them with
	// UnsupportedInstallError.
	Unsupported string `json:"unsupported,omitempty"`

	// Reserved metadata: parsed and preserved for later enforcement
	// features (approval gates, trifecta policy, per-call costs), never
	// acted on today.
	Permissions      *Permissions `json:"permissions,omitempty"`
	RequiresApproval *bool        `json:"requires_approval,omitempty"`
	CostPerCall      *float64     `json:"cost_per_call,omitempty"`
}

// Install describes how the server runs. Type selects the shape; the other
// fields belong to exactly one type.
type Install struct {
	Type string `json:"type"`
	// Transport is "stdio", "http", or "sse" (config.MCPServer vocabulary;
	// the registry's "streamable-http" is normalized to "http" at load).
	Transport string `json:"transport"`

	// Container image (type: image).
	Image string `json:"image,omitempty"`
	Port  int    `json:"port,omitempty"`

	// Local process (type: command). Positional inputs (Input.Arg) are
	// appended to this base command in input order.
	Command []string `json:"command,omitempty"`

	// External remote (type: url).
	URL string `json:"url,omitempty"`
	// AuthType is "", "bearer", or "header". The value comes from the
	// entry's Auth input at install time.
	AuthType   string `json:"auth_type,omitempty"`
	AuthHeader string `json:"auth_header,omitempty"`
}

// Input is one value the user supplies at install time. By default the
// resolved value lands in the server's env under Name; Arg and Auth inputs
// land in the command line and the auth block instead.
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Secret values are masked when prompted and routed into the variable
	// store as ${var:KEY} references rather than written literally.
	Secret bool `json:"secret,omitempty"`
	// Arg inputs are appended to the install command as positional
	// arguments instead of set in env.
	Arg bool `json:"arg,omitempty"`
	// Auth inputs feed the install's auth block (bearer token or header
	// value) instead of env. Only meaningful on url installs.
	Auth        bool     `json:"auth,omitempty"`
	Default     string   `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	// Format hints the value shape: "string", "number", "boolean", or
	// "filepath" (the MCP Registry input vocabulary).
	Format string `json:"format,omitempty"`
}

// Permissions is the reserved trifecta classification (roadmap item 17).
// Parsed and preserved; no enforcement reads it yet.
type Permissions struct {
	ReadPrivateData         bool `json:"read_private_data,omitempty"`
	ReadUntrustedPublicData bool `json:"read_untrusted_public_data,omitempty"`
	WriteOperation          bool `json:"write_operation,omitempty"`
}

// UnsupportedInstallError is returned when an entry cannot be installed
// because its package type or shape has no stack.yaml mapping.
type UnsupportedInstallError struct {
	// Kind names what was unsupported, e.g. "mcpb", "nuget", "cargo", or
	// "templated URL".
	Kind string
}

func (e *UnsupportedInstallError) Error() string {
	return "unsupported package type " + e.Kind +
		" (supported: oci container images, npm and pypi packages, and remote URLs)"
}
