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
//
// The embedded tool list stores every tool the downstream server advertises
// (post include/exclude for OpenAPI, raw otherwise). The whitelist filter is
// applied at read time by Tools() so operators can widen the whitelist beyond
// the currently-exposed subset without losing the superset.
type ClientBase struct {
	mu            sync.RWMutex
	initialized   bool
	allTools      []Tool
	serverInfo    ServerInfo
	toolWhitelist []string
	// protocolVersion is the MCP protocol version the downstream server
	// reported at initialize (handshake era) or the mutually selected
	// stateless version (stateless era); empty for lax servers that
	// omit it.
	protocolVersion string

	// era is the resolved protocol generation of the downstream server.
	// Empty means unresolved (pre-Initialize) or not applicable
	// (OpenAPIClient, which speaks no MCP wire protocol at all).
	era ProtocolEra

	// generationPin is the operator's protocol_generation override from
	// stack.yaml: "" or "auto" probes, "handshake" and "stateless" skip
	// the probe and force one era.
	generationPin string

	// capabilities is what the downstream server declared (initialize
	// result or discover result). Read by the tasks-extension proxy.
	capabilities Capabilities

	// listTTLMs/listCacheScope capture the CacheableResult fields a
	// stateless-era server reported on its last tools/list; nil TTL
	// means unknowable (legacy server, or no list yet). Feeds the
	// gateway's min/intersect cache-metadata aggregation.
	listTTLMs      *int64
	listCacheScope string
}

// Tools returns the cached tool list filtered by the whitelist, if any.
// This is what the router exposes to LLM clients.
func (b *ClientBase) Tools() []Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.toolWhitelist) == 0 {
		return b.allTools
	}
	return filterTools(b.allTools, b.toolWhitelist)
}

// AllTools returns every tool the downstream server advertises, ignoring
// any configured whitelist. Used by the management UI so operators can see
// the full selectable set regardless of the currently-applied curation.
func (b *ClientBase) AllTools() []Tool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.allTools
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

// SetTools updates the cached tools. The full set is retained; the whitelist
// filter is applied on read by Tools().
func (b *ClientBase) SetTools(tools []Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.allTools = tools
}

// SetInitialized marks the client as initialized with the given server info.
func (b *ClientBase) SetInitialized(info ServerInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.initialized = true
	b.serverInfo = info
}

// SetProtocolVersion records the protocol version the downstream server
// reported at initialize.
func (b *ClientBase) SetProtocolVersion(v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.protocolVersion = v
}

// ProtocolVersion returns the protocol version the downstream server reported
// at initialize; empty when the server omitted it or the handshake has not
// completed.
func (b *ClientBase) ProtocolVersion() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.protocolVersion
}

// Era returns the resolved protocol generation of the downstream
// server; empty when unresolved or not applicable (OpenAPI adapters).
func (b *ClientBase) Era() ProtocolEra {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.era
}

// SetEra records the resolved protocol generation.
func (b *ClientBase) SetEra(e ProtocolEra) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.era = e
}

// SetGenerationPin installs the operator's protocol_generation override
// ("", "auto", "handshake", or "stateless"). Must be set before
// Initialize.
func (b *ClientBase) SetGenerationPin(pin string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.generationPin = pin
}

func (b *ClientBase) generationPinValue() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.generationPin
}

// SetDownstreamCapabilities records what the server declared at
// initialize or discover.
func (b *ClientBase) SetDownstreamCapabilities(c Capabilities) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.capabilities = c
}

// DownstreamCapabilities returns the server's declared capabilities.
func (b *ClientBase) DownstreamCapabilities() Capabilities {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.capabilities
}

// SetListCacheMeta records the CacheableResult fields from the server's
// latest tools/list response.
func (b *ClientBase) SetListCacheMeta(ttlMs int64, scope string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listTTLMs = &ttlMs
	b.listCacheScope = scope
}

// ListCacheMeta returns the last-seen tools/list cache metadata; a nil
// TTL means unknowable (legacy server or no modern list observed).
func (b *ClientBase) ListCacheMeta() (ttlMs *int64, scope string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.listTTLMs, b.listCacheScope
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

// protocolVersionSetter is an optional interface for transports that carry the
// negotiated protocol version on subsequent requests (HTTP stamps it as the
// MCP-Protocol-Version header). RPCClient.Initialize() calls it with the
// version the downstream server negotiated.
type protocolVersionSetter interface {
	setProtocolVersion(v string)
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

// Initialize resolves the downstream server's protocol generation and
// completes era-appropriate setup. If the transport implements
// connector, Connect() is called first. Under the default "auto" pin it
// probes server/discover and falls back to the legacy initialize
// handshake on any response not positively recognized as modern; the
// operator's protocol_generation pin can skip the probe in either
// direction. Restart paths construct fresh clients, and stdio/process
// Reconnect re-enters here, so the era verdict is re-derived whenever a
// server could have changed underneath us.
func (r *RPCClient) Initialize(ctx context.Context) error {
	if c, ok := r.transport.(connector); ok {
		if err := c.Connect(ctx); err != nil {
			return err
		}
	}
	r.SetEra("") // re-resolve on every Initialize (Reconnect reuses the client)

	switch r.generationPinValue() {
	case GenerationHandshake:
		return r.initializeHandshake(ctx)
	case GenerationStateless:
		modern, err := r.probeDiscover(ctx)
		if err != nil {
			return err
		}
		if !modern {
			return fmt.Errorf("protocol_generation is pinned to stateless but the server did not answer server/discover as a stateless-era (%s) peer; remove the pin to auto-negotiate or run 'gridctl doctor' for per-server generation details", StatelessProtocolVersion)
		}
		return nil
	default: // "" or "auto": probe, then conservative fallback
		modern, err := r.probeDiscover(ctx)
		if err != nil {
			return err
		}
		if modern {
			return nil
		}
		return r.initializeHandshake(ctx)
	}
}

// initializeHandshake performs the legacy (2025-11-25 and earlier)
// initialize handshake.
func (r *RPCClient) initializeHandshake(ctx context.Context) error {
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

	// Reject servers negotiating a version we do not support; proceeding
	// silently risks tool calls failing in undebuggable ways later. An empty
	// version is tolerated for back-compat with lax servers that omit it.
	// A stateless-era version is equally rejected here: initialize does
	// not exist in that era, so a server negotiating one through a
	// handshake is confused, and adopting it would stamp stateless
	// version headers on session-bearing legacy requests.
	if result.ProtocolVersion != "" &&
		(!IsSupportedProtocolVersion(result.ProtocolVersion) || EraOfVersion(result.ProtocolVersion) != EraHandshake) {
		return fmt.Errorf("server negotiated protocol version %q, which this gridctl does not support for the initialize handshake (supported: %s); run 'gridctl doctor' for per-server generation details",
			result.ProtocolVersion, supportedProtocolVersionList())
	}

	// Hand the negotiated version to transports that stamp it on subsequent
	// requests (the Streamable HTTP spec requires the MCP-Protocol-Version
	// header on every post-initialize request).
	if result.ProtocolVersion != "" {
		if setter, ok := r.transport.(protocolVersionSetter); ok {
			setter.setProtocolVersion(result.ProtocolVersion)
		}
	}

	r.SetProtocolVersion(result.ProtocolVersion)
	r.SetEra(EraHandshake)
	r.SetDownstreamCapabilities(result.Capabilities)
	r.SetInitialized(result.ServerInfo)
	r.logger.Info("protocol generation negotiated", "server", r.name, "generation", EraHandshake)

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
	// Stateless-era servers report cache metadata on list results; feed
	// it into the gateway's aggregation. Legacy servers leave it nil
	// (unknowable), which pins the aggregate TTL to zero.
	if result.TTLMs != nil {
		r.SetListCacheMeta(*result.TTLMs, result.CacheScope)
	}
	return nil
}

// CallTool invokes a tool on the downstream agent.
func (r *RPCClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	// Normalize a nil map to an empty one so the request always carries an
	// arguments object ({}) rather than null. Strict downstream servers reject
	// both a missing field and null for no-argument tools. A caller-provided
	// map is left untouched.
	if arguments == nil {
		arguments = map[string]any{}
	}

	params := ToolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	// An MRTR retry carries the origin server's exact requestState and
	// the client's inputResponses through to the downstream leg. Only
	// meaningful on the stateless era; the fields marshal away
	// otherwise.
	if relay := mrtrRelayFromContext(ctx); relay != nil && r.Era() == EraStateless {
		params.RequestState = relay.RequestState
		params.InputResponses = relay.InputResponses
	}

	var result ToolCallResult
	if err := r.transport.call(ctx, "tools/call", params, &result); err != nil {
		return nil, fmt.Errorf("tools/call: %w", err)
	}

	return &result, nil
}

// RelayRaw sends a JSON-RPC request with verbatim params and returns
// the raw result bytes. The tasks-extension proxy uses it so extension
// payloads cross the gateway without gridctl committing to their shape.
// Transport-level _meta stamping still applies on the stateless era.
func (r *RPCClient) RelayRaw(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var result json.RawMessage
	if err := r.transport.call(ctx, method, params, &result); err != nil {
		return nil, err
	}
	return result, nil
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
