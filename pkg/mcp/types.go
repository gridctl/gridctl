package mcp

import (
	"context"
	"encoding/json"
	"time"
)

// Transport represents the type of transport for an MCP connection.
type Transport string

const (
	TransportHTTP  Transport = "http"
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
)

//go:generate mockgen -destination=mock_agent_client_test.go -package=mcp . AgentClient

// AgentClient is the interface for communicating with MCP agents.
type AgentClient interface {
	Name() string
	Initialize(ctx context.Context) error
	RefreshTools(ctx context.Context) error
	Tools() []Tool
	CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error)
	IsInitialized() bool
	ServerInfo() ServerInfo
}

// Pingable is an optional interface for AgentClients that support health checks.
// The health monitor uses type assertion to check if a client implements this.
type Pingable interface {
	Ping(ctx context.Context) error
}

// Reconnectable is an optional interface for AgentClients that support reconnection
// after connection failures (e.g., container restart, process crash).
// The health monitor uses type assertion to trigger reconnection on unhealthy clients.
type Reconnectable interface {
	Reconnect(ctx context.Context) error
}

// ToolCaller allows calling tools across the gateway's aggregated servers.
// This interface decouples the registry from the gateway to avoid circular dependencies.
// The gateway implements this interface and passes it to components that need
// to call tools without holding a direct reference to the gateway or router.
type ToolCaller interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error)
}

// DefaultPingTimeout is the timeout for health check pings.
const DefaultPingTimeout = 5 * time.Second

// MCPProtocolVersion is the MCP protocol version supported by this implementation.
const MCPProtocolVersion = "2024-11-05"

// Default timeouts for MCP transport clients.
const (
	// DefaultRequestTimeout is the timeout for individual MCP JSON-RPC requests.
	DefaultRequestTimeout = 30 * time.Second

	// DefaultReadyPollInterval is the interval between readiness checks.
	DefaultReadyPollInterval = 500 * time.Millisecond

	// DefaultReadyTimeout is the overall timeout for server readiness.
	DefaultReadyTimeout = 30 * time.Second
)

// MaxRequestBodySize is the maximum allowed size for incoming JSON-RPC request bodies (1MB).
const MaxRequestBodySize = 1 * 1024 * 1024

// MCP Protocol types

// ServerInfo contains information about the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientInfo contains information about the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities describes what the server/client can do.
type Capabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability indicates tools support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates resources support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability indicates prompts support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeParams contains parameters for the initialize request.
type InitializeParams struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ClientInfo      ClientInfo   `json:"clientInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

// InitializeResult is the response to initialize.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// InputSchemaObject is a helper for building simple input schemas.
// Use this when creating tools programmatically (e.g., A2A skill adapters).
// For MCP tools received from servers, use json.RawMessage directly to
// preserve the full JSON Schema without loss.
type InputSchemaObject struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a single property in an input schema.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// ToolsListResult is the response to tools/list.
type ToolsListResult struct {
	Tools      []Tool  `json:"tools"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

// ToolCallParams contains parameters for tools/call.
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolCallResult is the response to tools/call.
type ToolCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents content in a tool response.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// NewTextContent creates a text content item.
func NewTextContent(text string) Content {
	return Content{Type: "text", Text: text}
}

// PromptProvider is an optional interface for AgentClients that manage prompts.
// The gateway uses type assertion to detect prompt-capable clients and serve
// the MCP prompts/* and resources/* protocol methods.
type PromptProvider interface {
	ListPromptData() []PromptData
	GetPromptData(name string) (*PromptData, error)
}

// PromptData contains prompt information used by the MCP protocol layer.
type PromptData struct {
	Name        string
	Description string
	Content     string
	Arguments   []PromptArgumentData
}

// PromptArgumentData describes a prompt argument with default value support.
type PromptArgumentData struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

// --- MCP Prompts Protocol Types ---

// MCPPrompt represents a prompt in the MCP prompts/list response.
type MCPPrompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a prompt parameter.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptsListResult is the response to prompts/list.
type PromptsListResult struct {
	Prompts []MCPPrompt `json:"prompts"`
}

// PromptsGetParams contains parameters for prompts/get.
type PromptsGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptMessage represents a message in a prompts/get response.
type PromptMessage struct {
	Role    string  `json:"role"` // "user" or "assistant"
	Content Content `json:"content"`
}

// PromptsGetResult is the response to prompts/get.
type PromptsGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// --- MCP Resources Protocol Types ---

// MCPResource represents a resource in the resources/list response.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourcesListResult is the response to resources/list.
type ResourcesListResult struct {
	Resources []MCPResource `json:"resources"`
}

// ResourcesReadParams contains parameters for resources/read.
type ResourcesReadParams struct {
	URI string `json:"uri"`
}

// ResourceContents represents the content of a resource.
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ResourcesReadResult is the response to resources/read.
type ResourcesReadResult struct {
	Contents []ResourceContents `json:"contents"`
}

