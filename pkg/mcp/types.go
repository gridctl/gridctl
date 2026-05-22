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

// ToolCallObserver receives notifications after tool calls complete.
// Used by the metrics system to count tokens without coupling the gateway
// to the metrics package directly.
type ToolCallObserver interface {
	// ObserveToolCall is called after a tool call completes.
	// serverName is the MCP server that handled the call.
	// replicaID is the zero-indexed replica within that server's set, or -1
	// when the caller did not dispatch through a ReplicaSet.
	// arguments are the tool call arguments (input).
	// result is the tool call response (output). May be nil on error.
	ObserveToolCall(serverName string, replicaID int, arguments map[string]any, result *ToolCallResult)
}

// ToolCallObservation is the additive payload passed to ClientObserver. New
// optional fields land here over time so the ToolCallObserver interface
// stays stable.
type ToolCallObservation struct {
	// ServerName is the MCP server that handled the call.
	ServerName string
	// ReplicaID is the zero-indexed replica within ServerName's replica set,
	// or -1 when the caller did not dispatch through a ReplicaSet.
	ReplicaID int
	// ClientID is the normalized identifier of the originating MCP client
	// (e.g. "claude-code", "cursor"). Empty when no session attribution
	// was available — observers should treat that as anonymous.
	ClientID string
	// ToolName is the unprefixed tool name (no "<server>__" prefix).
	ToolName string
	// Arguments are the tool call arguments (input).
	Arguments map[string]any
	// Result is the tool call response (output). May be nil on error.
	Result *ToolCallResult
}

// ToolCallSummary is the synchronous return value of ObserveToolCallWithClient.
// The gateway uses these values to populate OTel GenAI semantic-convention
// span attributes without re-counting tokens. Cache-token fields are zero
// when the underlying tool result did not report cache usage.
type ToolCallSummary struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	// Model is the canonical model ID used to price the call, or "" when
	// no model could be resolved.
	Model string
	// CostUSD is the total USD cost recorded for the call. Zero when
	// HasCost is false.
	CostUSD float64
	// HasCost reports whether the active pricing source recognised Model
	// and produced a non-zero breakdown for the call.
	HasCost bool
}

// ClientObserver is the optional richer interface that observers implement to
// receive context-aware, client-attributed observations. Implementations are
// invoked synchronously by the gateway so they can read the active span
// from ctx and return precomputed values for span attribution. Observers
// that only implement the legacy ToolCallObserver continue to work via
// the asynchronous fan-out path.
type ClientObserver interface {
	// ObserveToolCallWithClient records a tool-call observation. The returned
	// summary carries the values the gateway needs to set OTel GenAI span
	// attributes on the call's child span.
	ObserveToolCallWithClient(ctx context.Context, obs ToolCallObservation) ToolCallSummary
}

// FormatSavingsRecorder receives format savings observations.
// Used by the gateway to report token savings from format conversion
// without coupling to the metrics package directly.
// Normal token usage tracking is handled separately by the ToolCallObserver.
type FormatSavingsRecorder interface {
	// RecordFormatSavings records token counts before and after format conversion.
	// originalTokens is the token count of the original JSON content.
	// formattedTokens is the token count of the converted content (TOON/CSV).
	RecordFormatSavings(serverName string, originalTokens, formattedTokens int)
}

// SchemaDrift describes a single tool whose definition changed since it was pinned.
type SchemaDrift struct {
	Name           string
	OldHash        string
	NewHash        string
	OldDescription string
	NewDescription string
}

// SchemaVerifier performs TOFU schema pinning for MCP tool definitions.
// It is defined in the mcp package (rather than importing pkg/pins) to avoid
// an import cycle: pkg/pins already imports pkg/mcp for the Tool type.
// pkg/pins.GatewayAdapter implements this interface.
type SchemaVerifier interface {
	// VerifyOrPin pins tools on first use and verifies them on subsequent calls.
	// Returns the list of modified tools (empty = no drift) and any error.
	VerifyOrPin(serverName string, tools []Tool) ([]SchemaDrift, error)
}

// PinResetter is an optional extension of SchemaVerifier for clearing stored pin
// records. Implementations that support this (e.g. pins.GatewayAdapter) satisfy
// this interface. It is checked via type assertion, not embedded in SchemaVerifier,
// so existing implementations remain compatible.
type PinResetter interface {
	// ResetServerPins deletes the pin record for serverName so the next
	// VerifyOrPin call treats it as a first-use pin.
	ResetServerPins(serverName string) error
}

// DefaultPingTimeout is the timeout for health check pings.
const DefaultPingTimeout = 5 * time.Second

// pingTimeoutOrDefault returns d when it is positive, otherwise DefaultPingTimeout.
// Clients store a configured PingTimeout that may be zero (unset) or negative
// (treated as unset); this helper keeps the fallback rule in one place.
func pingTimeoutOrDefault(d time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return DefaultPingTimeout
}

// MCPProtocolVersion is the MCP protocol version supported by this implementation.
const MCPProtocolVersion = "2025-11-25"

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
	Instructions    string       `json:"instructions,omitempty"`
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

	// Usage carries optional usage metadata reported alongside a tool
	// result — model ID, cache-read/cache-write tokens. The field is
	// nil for tool calls that do not report usage. Skipped during
	// JSON marshalling: the MCP wire shape for usage metadata is
	// finalized in a follow-up PR (the spec's `_meta` field is used
	// by tracing and pipelock for keyed extensions, so we cannot
	// claim the entire object here). Internal observers populate
	// Usage in memory only.
	Usage *CallUsage `json:"-"`

	// Meta is the MCP-spec extension point on result objects. The
	// gateway populates Meta["run_id"] when a typed-skill call is
	// persisted to the on-disk ledger so external MCP clients have a
	// handle for resume / approval / trace inspection. Other keyed
	// extensions (usage, drift signals) can land here as the wire
	// shape stabilises.
	Meta map[string]any `json:"_meta,omitempty"`
}

// CallUsage is the optional per-call usage metadata that an MCP server may
// report alongside a tool result. All fields are optional: zero values
// indicate "not reported" rather than "zero usage." The metrics observer
// reads these to price cache traffic separately from input traffic.
type CallUsage struct {
	// Model is the canonical model ID used to service the call (e.g.
	// "claude-opus-4-7"). When empty, the observer falls back to the
	// server's configured default model.
	Model string `json:"model,omitempty"`

	// CacheReadTokens is the count of input tokens served from a prompt
	// cache. Priced via the provider's cache_read_input_token_cost rate.
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`

	// CacheCreationTokens is the count of input tokens written to a
	// prompt cache. Priced via the provider's
	// cache_creation_input_token_cost rate.
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
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

