package mcp

import (
	"context"
	"encoding/json"
)

// Transport represents the type of transport for an MCP connection.
type Transport string

const (
	TransportHTTP  Transport = "http"
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
)

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

// JSON-RPC 2.0 types

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
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
func NewErrorResponse(id *json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
}

// NewSuccessResponse creates a JSON-RPC success response.
func NewSuccessResponse(id *json.RawMessage, result any) Response {
	var resultBytes json.RawMessage
	if result != nil {
		resultBytes, _ = json.Marshal(result)
	}
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultBytes,
	}
}
