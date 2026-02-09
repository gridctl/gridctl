package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
)

// ClientBase provides shared state and accessor methods for all AgentClient implementations.
// Embed this struct to get Tools(), IsInitialized(), ServerInfo(), and SetToolWhitelist().
type ClientBase struct {
	mu            sync.RWMutex
	initialized   bool
	tools         []Tool
	serverInfo    ServerInfo
	toolWhitelist []string
}

// Tools returns the cached tool list.
func (b *ClientBase) Tools() []Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.tools
}

// IsInitialized returns whether the client has been initialized.
func (b *ClientBase) IsInitialized() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.initialized
}

// ServerInfo returns the server information.
func (b *ClientBase) ServerInfo() ServerInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.serverInfo
}

// SetToolWhitelist sets the list of allowed tool names.
// Only tools in this list will be returned by Tools() and RefreshTools().
// An empty or nil list means all tools are allowed.
func (b *ClientBase) SetToolWhitelist(tools []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolWhitelist = tools
}

// SetTools updates the cached tools, applying the whitelist filter.
func (b *ClientBase) SetTools(tools []Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.toolWhitelist) > 0 {
		b.tools = filterTools(tools, b.toolWhitelist)
	} else {
		b.tools = tools
	}
}

// SetInitialized marks the client as initialized with the given server info.
func (b *ClientBase) SetInitialized(info ServerInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.initialized = true
	b.serverInfo = info
}

// filterTools returns only tools whose names are in the whitelist.
func filterTools(tools []Tool, whitelist []string) []Tool {
	allowed := make(map[string]bool, len(whitelist))
	for _, name := range whitelist {
		allowed[name] = true
	}
	var filtered []Tool
	for _, tool := range tools {
		if allowed[tool.Name] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// transporter defines transport-specific JSON-RPC I/O that each client must implement.
// Unexported because it is a package-internal contract, not part of the public API.
// Implementations: Client (HTTP), StdioClient (Docker attach), ProcessClient (local process).
type transporter interface {
	call(ctx context.Context, method string, params any, result any) error
	send(ctx context.Context, method string, params any) error
}

// connector is an optional interface for transports that require connection setup
// before the MCP handshake (e.g., stdio, process). If a transport implements
// connector, RPCClient.Initialize() calls Connect() before the handshake.
type connector interface {
	Connect(ctx context.Context) error
}

// RPCClient provides shared JSON-RPC protocol methods for MCP transport clients.
// It embeds ClientBase for state management and delegates I/O to a transporter.
//
// Embedding hierarchy: ConcreteClient -> RPCClient -> ClientBase
//
// Each concrete client (Client, StdioClient, ProcessClient) embeds RPCClient and
// implements transporter, passing itself to initRPCClient. This allows the shared
// protocol methods to dispatch to transport-specific I/O.
//
// OpenAPIClient is separate — it embeds ClientBase directly since it does not
// use JSON-RPC at all.
type RPCClient struct {
	ClientBase
	name      string
	logger    *slog.Logger
	transport transporter
}

// initRPCClient initializes the RPCClient fields. Called by transport constructors.
func initRPCClient(r *RPCClient, name string, transport transporter) {
	r.name = name
	r.logger = logging.NewDiscardLogger()
	r.transport = transport
}

// Name returns the agent name.
func (r *RPCClient) Name() string {
	return r.name
}

// SetLogger sets the logger for this client.
func (r *RPCClient) SetLogger(logger *slog.Logger) {
	if logger != nil {
		r.logger = logger
	}
}

// Initialize performs the MCP initialize handshake.
// If the transport implements connector, Connect() is called first.
func (r *RPCClient) Initialize(ctx context.Context) error {
	if c, ok := r.transport.(connector); ok {
		if err := c.Connect(ctx); err != nil {
			return err
		}
	}

	params := InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo: ClientInfo{
			Name:    "gridctl-gateway",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
	}

	var result InitializeResult
	if err := r.transport.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	r.SetInitialized(result.ServerInfo)

	// Send initialized notification (non-fatal)
	_ = r.transport.send(ctx, "notifications/initialized", nil)

	return nil
}

// RefreshTools fetches the current tool list from the agent.
// If a tool whitelist has been set, only tools matching the whitelist are stored.
func (r *RPCClient) RefreshTools(ctx context.Context) error {
	var result ToolsListResult
	if err := r.transport.call(ctx, "tools/list", nil, &result); err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	r.SetTools(result.Tools)
	return nil
}

// CallTool invokes a tool on the downstream agent.
func (r *RPCClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	params := ToolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	var result ToolCallResult
	if err := r.transport.call(ctx, "tools/call", params, &result); err != nil {
		return nil, fmt.Errorf("tools/call: %w", err)
	}

	return &result, nil
}

// buildNotification constructs a JSON-RPC notification request.
func buildNotification(method string, params any) (jsonrpc.Request, error) {
	var paramsBytes json.RawMessage
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return jsonrpc.Request{}, fmt.Errorf("marshaling params: %w", err)
		}
	}

	return jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	}, nil
}
