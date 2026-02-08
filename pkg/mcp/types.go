package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
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

// JSON-RPC 2.0 types — re-exported from pkg/jsonrpc for backward compatibility.
type Request = jsonrpc.Request
type Response = jsonrpc.Response
type Error = jsonrpc.Error

const (
	ParseError     = jsonrpc.ParseError
	InvalidRequest = jsonrpc.InvalidRequest
	MethodNotFound = jsonrpc.MethodNotFound
	InvalidParams  = jsonrpc.InvalidParams
	InternalError  = jsonrpc.InternalError
)

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

// NewErrorResponse creates a JSON-RPC error response.
var NewErrorResponse = jsonrpc.NewErrorResponse

// NewSuccessResponse creates a JSON-RPC success response.
var NewSuccessResponse = jsonrpc.NewSuccessResponse
