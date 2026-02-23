// Package provisioner detects installed LLM clients and manages their MCP
// gateway configuration, enabling zero-friction connection between gridctl
// and tools like Claude Desktop, Cursor, VS Code, and others.
package provisioner

import (
	"errors"
	"fmt"
)

// Sentinel errors for link operations.
var (
	ErrAlreadyLinked = errors.New("already linked with identical config")
	ErrConflict      = errors.New("existing entry has unexpected config")
	ErrNotLinked     = errors.New("no gridctl entry found")
	ErrClientNotFound = errors.New("client not detected on this system")
	ErrNpxNotFound   = errors.New("npx not found in PATH")
)

// ClientProvisioner handles config for a single LLM client.
type ClientProvisioner interface {
	// Name returns a human-readable client name (e.g., "Claude Desktop").
	Name() string

	// Slug returns the CLI identifier (e.g., "claude", "cursor").
	Slug() string

	// Detect checks if this client is installed on the current system.
	// Returns the config file path if found, empty string if not installed.
	Detect() (configPath string, found bool)

	// IsLinked checks if a gridctl entry already exists in the config.
	IsLinked(configPath string, serverName string) (bool, error)

	// Link injects or updates the gridctl entry in the client config.
	Link(configPath string, opts LinkOptions) error

	// Unlink removes the gridctl entry from the client config.
	Unlink(configPath string, serverName string) error

	// NeedsBridge returns true if this client requires mcp-remote for SSE.
	NeedsBridge() bool
}

// LinkOptions configures how a link is created.
type LinkOptions struct {
	GatewayURL string // e.g., "http://localhost:8180/sse"
	Port       int    // Gateway port for HTTP URL construction
	ServerName string // Key name in config (default: "gridctl")
	Force      bool   // Overwrite existing entry
	DryRun     bool   // Show what would change without modifying files
}

// DetectedClient pairs a provisioner with its found config path.
type DetectedClient struct {
	Provisioner ClientProvisioner
	ConfigPath  string
}

// LinkResult describes what happened during a link operation.
type LinkResult struct {
	Client     string // Human-readable client name
	ConfigPath string
	BackupPath string
	Action     string // "linked", "updated", "skipped", "already-linked"
	Transport  string // "native SSE" or "mcp-remote bridge"
	Error      error
}

// Registry manages all known client provisioners.
type Registry struct {
	clients []ClientProvisioner
}

// NewRegistry creates a Registry with all known client provisioners.
func NewRegistry() *Registry {
	return &Registry{
		clients: []ClientProvisioner{
			// Tier 1
			newClaudeDesktop(),
			newClaudeCode(),
			newCursor(),
			newWindsurf(),
			newVSCode(),
			newGeminiCLI(),
			newOpenCode(),
			// Tier 2
			newContinueDev(),
			newCline(),
			newAnythingLLM(),
			newRooCode(),
			newZed(),
			newGoose(),
		},
	}
}

// DetectAll returns all clients found on this system.
func (r *Registry) DetectAll() []DetectedClient {
	var detected []DetectedClient
	for _, c := range r.clients {
		if path, found := c.Detect(); found {
			detected = append(detected, DetectedClient{
				Provisioner: c,
				ConfigPath:  path,
			})
		}
	}
	return detected
}

// FindBySlug returns the provisioner matching the given slug.
func (r *Registry) FindBySlug(slug string) (ClientProvisioner, bool) {
	for _, c := range r.clients {
		if c.Slug() == slug {
			return c, true
		}
	}
	return nil, false
}

// AllSlugs returns the slugs of all registered clients.
func (r *Registry) AllSlugs() []string {
	slugs := make([]string, len(r.clients))
	for i, c := range r.clients {
		slugs[i] = c.Slug()
	}
	return slugs
}

// IsAnyLinked checks if any known client has a gridctl entry.
func (r *Registry) IsAnyLinked(serverName string) bool {
	for _, c := range r.clients {
		path, found := c.Detect()
		if !found {
			continue
		}
		linked, err := c.IsLinked(path, serverName)
		if err == nil && linked {
			return true
		}
	}
	return false
}

// TransportDescription returns a human-readable transport description.
func TransportDescription(needsBridge bool) string {
	if needsBridge {
		return "mcp-remote bridge"
	}
	return "native SSE"
}

// TransportDescriptionFor returns a transport description for a specific provisioner,
// distinguishing HTTP-native clients from SSE-native clients.
func TransportDescriptionFor(prov ClientProvisioner) string {
	if prov.NeedsBridge() {
		return "mcp-remote bridge"
	}
	switch prov.(type) {
	case *ClaudeCode, *GeminiCLI, *OpenCode:
		return "native HTTP"
	default:
		return "native SSE"
	}
}

// ClientInfo holds detection and link status for one client provisioner.
type ClientInfo struct {
	Name       string
	Slug       string
	Detected   bool
	Linked     bool
	Transport  string
	ConfigPath string
}

// AllClientInfo returns detection and link status for every registered client.
func (r *Registry) AllClientInfo(serverName string) []ClientInfo {
	infos := make([]ClientInfo, 0, len(r.clients))
	for _, c := range r.clients {
		info := ClientInfo{
			Name:      c.Name(),
			Slug:      c.Slug(),
			Transport: TransportDescriptionFor(c),
		}
		if path, found := c.Detect(); found {
			info.Detected = true
			info.ConfigPath = path
			linked, err := c.IsLinked(path, serverName)
			if err == nil {
				info.Linked = linked
			}
		}
		infos = append(infos, info)
	}
	return infos
}

// GatewayURL constructs the SSE gateway URL from a port.
func GatewayURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/sse", port)
}

// GatewayHTTPURL constructs the streamable HTTP gateway URL from a port.
func GatewayHTTPURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/mcp", port)
}
