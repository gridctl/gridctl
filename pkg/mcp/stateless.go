package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// Upstream stateless edge (2026-07-28): the sibling branch to the
// handshake/session paths in handlePost. Classification rule: modern
// per-request _meta selects this path; an initialize method selects the
// handshake path; anything else falls through to session lookup. The
// stateless path bypasses both session maps entirely and populates the
// same context keys (client ID, access ID, group) the session path
// populates, so everything inboard of the edge stays era-free.
//
// Group binding on this path is endpoint-derived by definition: with no
// session to freeze a group onto, the group is whatever the request
// context carries from the /groups/{name}/mcp route wrapper, and a
// request to the default /mcp endpoint is full-surface.

// mcpParamHeadersKey carries unrecognized Mcp-Param-* headers from the
// upstream request so the downstream HTTP leg can forward them
// untouched, per the transport spec's intermediary rules.
type mcpParamHeadersKey struct{}

func withMcpParamHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return context.WithValue(ctx, mcpParamHeadersKey{}, headers)
}

func mcpParamHeadersFromContext(ctx context.Context) map[string]string {
	v, _ := ctx.Value(mcpParamHeadersKey{}).(map[string]string)
	return v
}

// mrtrRelay carries MRTR retry fields from the stateless edge to the
// downstream chokepoint: the origin server's exact requestState bytes
// (already unwrapped from the gridctl envelope), the client's
// inputResponses, and the envelope's recorded server for the routing
// cross-check.
type mrtrRelay struct {
	ExpectedServer string
	RequestState   string
	InputResponses json.RawMessage
}

type mrtrRelayKey struct{}

func withMRTRRelay(ctx context.Context, relay *mrtrRelay) context.Context {
	if relay == nil {
		return ctx
	}
	return context.WithValue(ctx, mrtrRelayKey{}, relay)
}

func mrtrRelayFromContext(ctx context.Context) *mrtrRelay {
	v, _ := ctx.Value(mrtrRelayKey{}).(*mrtrRelay)
	return v
}

// stripMRTRRelay removes any relay from the context by shadowing it
// with a nil entry.
func stripMRTRRelay(ctx context.Context) context.Context {
	if mrtrRelayFromContext(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, mrtrRelayKey{}, (*mrtrRelay)(nil))
}

// writeStatelessResponse writes a JSON-RPC response with the given HTTP
// status. The stateless era, unlike the handshake era, uses HTTP status
// codes to distinguish rejection classes (400 validation, 404 unknown
// method), always with a JSON-RPC body.
func writeStatelessResponse(w http.ResponseWriter, status int, resp jsonrpc.Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleStateless serves one stateless-era request. hasMeta is false
// only for a server/discover carried without modern _meta: discover
// doubles as the backward-compatibility probe, so it is answered with
// maximum leniency rather than validated into a 400 a legacy-shaped
// prober cannot interpret.
func (s *StreamableHTTPServer) handleStateless(w http.ResponseWriter, r *http.Request, req *jsonrpc.Request, meta RequestMeta, hasMeta bool) {
	if hasMeta {
		// Version validation: _meta is authoritative on this path.
		if meta.ProtocolVersion == "" || EraOfVersion(meta.ProtocolVersion) != EraStateless || !IsSupportedProtocolVersion(meta.ProtocolVersion) {
			writeStatelessResponse(w, http.StatusBadRequest, jsonrpc.NewErrorResponseWithData(
				req.ID, ErrCodeUnsupportedProtocolVersion, "Unsupported protocol version",
				UnsupportedVersionErrorData{Supported: SupportedProtocolVersions, Requested: meta.ProtocolVersion}))
			return
		}
		if len(meta.ClientCapabilities) == 0 {
			writeStatelessResponse(w, http.StatusBadRequest, jsonrpc.NewErrorResponse(
				req.ID, jsonrpc.InvalidRequest, "missing required _meta field io.modelcontextprotocol/clientCapabilities"))
			return
		}
		if msg := validateStatelessHeaders(r, req, meta); msg != "" {
			writeStatelessResponse(w, http.StatusBadRequest, jsonrpc.NewErrorResponse(req.ID, ErrCodeHeaderMismatch, msg))
			return
		}
	}

	// Per-request identity, feeding the same context keys the session
	// path populates so access scoping, call gates, telemetry, cost
	// attribution, and tracing behave identically on both paths. The
	// explicit ?client= / X-Gridctl-Client-Id identifier wins, then the
	// normalized _meta clientInfo name.
	ctx := r.Context()
	clientID := NormalizeClientID(meta.ClientInfo.Name)
	accessID := NormalizeClientID(clientAccessIDFromRequest(r))
	if accessID == "" {
		accessID = clientID
	}
	ctx = WithClientID(ctx, clientID)
	ctx = WithClientAccessID(ctx, accessID)
	ctx = withMcpParamHeaders(ctx, collectMcpParamHeaders(r))
	// Relay the client's declared capabilities to the downstream leg so
	// modern servers know which input requests the far end can fulfill.
	ctx = withUpstreamClientCapabilities(ctx, meta.ClientCapabilities)

	// Notifications are acknowledged with 202 and no body. The core
	// protocol defines no client-to-server notifications over this
	// transport, so acceptance is the only defined behavior.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "server/discover":
		result := s.gateway.HandleServerDiscover(ctx)
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "tools/list":
		result, err := s.gateway.HandleToolsList(ctx)
		if err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
			return
		}
		s.gateway.attachListCacheMeta(&result.StatelessResultFields)
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "tools/call":
		s.handleStatelessToolsCall(ctx, w, req, meta)
	case "prompts/list":
		result, err := s.gateway.HandlePromptsList()
		if err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
			return
		}
		s.gateway.attachListCacheMeta(&result.StatelessResultFields)
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "prompts/get":
		if req.Params == nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "params required for prompts/get"))
			return
		}
		var params PromptsGetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid prompts/get params"))
			return
		}
		result, err := s.gateway.HandlePromptsGet(ctx, params)
		if err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
			return
		}
		result.ResultType = ResultTypeComplete
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "resources/list":
		result, err := s.gateway.HandleResourcesList()
		if err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
			return
		}
		s.gateway.attachListCacheMeta(&result.StatelessResultFields)
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "resources/read":
		if req.Params == nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "params required for resources/read"))
			return
		}
		var params ResourcesReadParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid resources/read params"))
			return
		}
		result, err := s.gateway.HandleResourcesRead(params)
		if err != nil {
			// 2026-07-28 renumbered resource-not-found from -32002 to
			// -32602; gridctl's read failures (unknown scheme, missing
			// skill) are not-found shaped. An unavailable registry is
			// infrastructure, not the caller's params.
			code := jsonrpc.InvalidParams
			if strings.Contains(err.Error(), "registry not available") {
				code = jsonrpc.InternalError
			}
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, code, err.Error()))
			return
		}
		s.gateway.attachListCacheMeta(&result.StatelessResultFields)
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
	case "tasks/get", "tasks/update", "tasks/cancel":
		s.handleStatelessTask(ctx, w, req)
	default:
		// Removed handshake-era methods (ping, logging/setLevel,
		// initialize, notifications/*) and anything else unknown get
		// the modern unknown-method shape: HTTP 404 with -32601, which
		// distinguishes a modern server from a legacy endpoint that
		// does not host /mcp at all.
		writeStatelessResponse(w, http.StatusNotFound, jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method)))
	}
}

// validateStatelessHeaders enforces the request-metadata header mirror:
// MCP-Protocol-Version must match _meta, Mcp-Method must match the
// body method, and Mcp-Name must match params.name/params.uri for the
// methods that require it. Returns a rejection message, or "" when the
// headers validate.
func validateStatelessHeaders(r *http.Request, req *jsonrpc.Request, meta RequestMeta) string {
	headerVersion := r.Header.Get("MCP-Protocol-Version")
	if headerVersion == "" {
		return "missing required MCP-Protocol-Version header"
	}
	if headerVersion != meta.ProtocolVersion {
		return fmt.Sprintf("Header mismatch: MCP-Protocol-Version header value %q does not match _meta value %q", headerVersion, meta.ProtocolVersion)
	}
	headerMethod := r.Header.Get(headerMcpMethod)
	if headerMethod == "" {
		return "missing required Mcp-Method header"
	}
	if headerMethod != req.Method {
		return fmt.Sprintf("Header mismatch: Mcp-Method header value %q does not match body value %q", headerMethod, req.Method)
	}
	bodyName := mcpNameForRequest(req.Method, req.Params)
	if bodyName == "" {
		return ""
	}
	rawName := r.Header.Get(headerMcpName)
	if rawName == "" {
		return "missing required Mcp-Name header"
	}
	headerName, ok := decodeHeaderValue(rawName)
	if !ok {
		return "malformed Mcp-Name header encoding"
	}
	if headerName != bodyName {
		return fmt.Sprintf("Header mismatch: Mcp-Name header value %q does not match body value %q", headerName, bodyName)
	}
	return ""
}

// collectMcpParamHeaders gathers Mcp-Param-* headers for opaque
// forwarding. gridctl recognizes none of them (recognition would
// require x-mcp-header schema interpretation), so per the transport
// spec they are forwarded untouched and otherwise ignored.
func collectMcpParamHeaders(r *http.Request) map[string]string {
	var params map[string]string
	for name, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "mcp-param-") && len(values) > 0 {
			if params == nil {
				params = make(map[string]string)
			}
			params[name] = values[0]
		}
	}
	return params
}

// handleStatelessToolsCall serves tools/call on the stateless path,
// including the MRTR relay legs.
func (s *StreamableHTTPServer) handleStatelessToolsCall(ctx context.Context, w http.ResponseWriter, req *jsonrpc.Request, meta RequestMeta) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid tools/call params"))
		return
	}

	// MRTR retry: recover the origin server's exact requestState bytes
	// from the gridctl envelope. A value gridctl did not mint cannot be
	// routed and is rejected rather than guessed at.
	if params.RequestState != "" {
		server, originState, ok := unwrapRequestState(params.RequestState)
		if !ok {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "requestState was not issued by this gateway"))
			return
		}
		ctx = withMRTRRelay(ctx, &mrtrRelay{
			ExpectedServer: server,
			RequestState:   originState,
			InputResponses: params.InputResponses,
		})
		params.RequestState = ""
		params.InputResponses = nil
	} else if len(params.InputResponses) > 0 {
		ctx = withMRTRRelay(ctx, &mrtrRelay{InputResponses: params.InputResponses})
	}

	result, err := s.gateway.HandleToolsCall(ctx, params)
	if err != nil {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
		return
	}

	switch result.ResultType {
	case ResultTypeInputRequired:
		// Servers MUST NOT send input requests the client has not
		// declared capabilities for; gridctl enforces that on behalf of
		// origin servers it relays for.
		if missing := missingInputCapability(result.InputRequests, meta.ClientCapabilities); missing != "" {
			writeStatelessResponse(w, http.StatusBadRequest, jsonrpc.NewErrorResponse(
				req.ID, ErrCodeMissingRequiredClientCapability,
				fmt.Sprintf("tool requires the %q client capability, which the request did not declare", missing)))
			return
		}
	case "":
		// A legacy downstream's result has no resultType; synthesize
		// the required field when bridging to a modern client.
		result.ResultType = ResultTypeComplete
	}
	writeStatelessResponse(w, http.StatusOK, jsonrpc.NewSuccessResponse(req.ID, result))
}

// missingInputCapability shallow-inspects the method of each relayed
// input request (the only field gridctl reads; requestState opacity is
// untouched) and returns the alphabetically first capability the
// client did not declare, or "". Sorted so the reported capability is
// deterministic across identical requests.
func missingInputCapability(inputRequests, clientCapabilities json.RawMessage) string {
	if len(inputRequests) == 0 {
		return ""
	}
	var requests map[string]struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(inputRequests, &requests); err != nil {
		return ""
	}
	var declared map[string]json.RawMessage
	_ = json.Unmarshal(clientCapabilities, &declared)
	missing := ""
	for _, r := range requests {
		var capability string
		switch r.Method {
		case "elicitation/create":
			capability = "elicitation"
		case "sampling/createMessage":
			capability = "sampling"
		case "roots/list":
			capability = "roots"
		default:
			continue
		}
		if _, ok := declared[capability]; !ok {
			if missing == "" || capability < missing {
				missing = capability
			}
		}
	}
	return missing
}

// handleStatelessTask proxies a tasks-extension method. gridctl does no
// task bookkeeping: the request is relayed verbatim to the one modern
// downstream server that declares the tasks extension. With several
// declaring servers a bare task handle is not routable without
// gateway-side state, which the extension proxy deliberately does not
// keep, so that configuration is rejected with a clear error.
func (s *StreamableHTTPServer) handleStatelessTask(ctx context.Context, w http.ResponseWriter, req *jsonrpc.Request) {
	client, err := s.gateway.taskCapableClient()
	if err != nil {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, err.Error()))
		return
	}
	result, err := client.RelayRaw(ctx, req.Method, req.Params)
	if err != nil {
		if rpcErr := rpcErrorFrom(err); rpcErr != nil {
			writeStatelessResponse(w, http.StatusOK, jsonrpc.Response{
				JSONRPC: "2.0", ID: req.ID,
				Error: &jsonrpc.Error{Code: rpcErr.Code, Message: rpcErr.Message, Data: rpcErr.Data},
			})
			return
		}
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
		return
	}
	writeStatelessResponse(w, http.StatusOK, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

// HandleServerDiscover builds the gateway's server/discover response.
// The identity and capability shape mirrors HandleInitialize (including
// the group-suffixed name), with the stateless-era additions: supported
// versions, extension declarations, and cache metadata aggregated from
// the downstream fleet.
func (g *Gateway) HandleServerDiscover(ctx context.Context) *DiscoverResult {
	caps := g.advertisedCapabilities()
	if name := g.taskCapableServerName(); name != "" {
		caps.Extensions = map[string]json.RawMessage{
			TasksExtensionID: json.RawMessage(`{}`),
		}
	}

	info := g.ServerInfo()
	if group := GroupFromContext(ctx); group != "" {
		info.Name = info.Name + "/" + group
	}
	info.Title = info.Name

	ttl, scope := g.aggregateListCacheMeta()
	return &DiscoverResult{
		ResultType:        ResultTypeComplete,
		SupportedVersions: SupportedProtocolVersions,
		Capabilities:      caps,
		Instructions:      g.buildInstructions(),
		TTLMs:             ttl,
		CacheScope:        scope,
		Meta: map[string]any{
			metaKeyServerInfo: info,
		},
	}
}

// listCacheMetaSource is the duck-typed slice of a downstream client
// the cache aggregation reads. RPCClient satisfies it; OpenAPIClient
// satisfies it too but always reports a nil TTL (unknowable), which is
// exactly right for an adapter with no MCP wire protocol.
type listCacheMetaSource interface {
	Era() ProtocolEra
	ListCacheMeta() (*int64, string)
}

// aggregateListCacheMeta folds the fleet's cache metadata into the
// gateway's own: ttlMs is the minimum across servers and 0 when any
// server is legacy or unknowable; cacheScope is private unless every
// server declares public. An empty fleet is 0/private: nothing may be
// cached on the say-so of nobody.
func (g *Gateway) aggregateListCacheMeta() (int64, string) {
	clients := g.router.Clients()
	ttl := int64(0)
	scope := CacheScopePrivate
	if len(clients) == 0 {
		return 0, CacheScopePrivate
	}
	allPublic := true
	first := true
	for _, c := range clients {
		src, ok := c.(listCacheMetaSource)
		if !ok || src.Era() != EraStateless {
			return 0, CacheScopePrivate
		}
		srvTTL, srvScope := src.ListCacheMeta()
		if srvTTL == nil {
			return 0, CacheScopePrivate
		}
		if first || *srvTTL < ttl {
			ttl = *srvTTL
		}
		first = false
		if srvScope != CacheScopePublic {
			allPublic = false
		}
	}
	if allPublic {
		scope = CacheScopePublic
	}
	return ttl, scope
}

// attachListCacheMeta stamps the required stateless result fields onto
// a list-shaped result: resultType complete plus the aggregated cache
// metadata.
func (g *Gateway) attachListCacheMeta(fields *StatelessResultFields) {
	ttl, scope := g.aggregateListCacheMeta()
	fields.ResultType = ResultTypeComplete
	fields.TTLMs = &ttl
	fields.CacheScope = scope
}

// advertisedCapabilities is the single source for what the gateway
// declares to upstream clients on both eras. listChanged is not
// advertised: gridctl has never emitted a list-changed notification,
// and advertising an undelivered capability is a spec violation the
// conformance suite surfaces.
func (g *Gateway) advertisedCapabilities() Capabilities {
	caps := Capabilities{
		Tools: &ToolsCapability{},
	}
	if g.promptProvider() != nil {
		caps.Prompts = &PromptsCapability{}
		caps.Resources = &ResourcesCapability{}
	}
	return caps
}

// rawRelayer is the optional client interface the tasks proxy needs: a
// verbatim JSON-RPC relay that preserves raw params and result bytes.
type rawRelayer interface {
	RelayRaw(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// resolveTaskCapableServer finds the stateless-era servers declaring
// the tasks extension. The proxy is only routable when exactly one
// exists: task handles are opaque, so with several declaring servers a
// bare handle cannot be routed without gateway-side task state, which
// gridctl deliberately does not keep.
func (g *Gateway) resolveTaskCapableServer() (name string, relay rawRelayer, count int) {
	for _, c := range g.router.Clients() {
		src, ok := c.(interface {
			Era() ProtocolEra
			DownstreamCapabilities() Capabilities
			Name() string
		})
		if !ok || src.Era() != EraStateless {
			continue
		}
		if _, declared := src.DownstreamCapabilities().Extensions[TasksExtensionID]; !declared {
			continue
		}
		count++
		if r, ok := c.(rawRelayer); ok {
			name, relay = src.Name(), r
		}
	}
	return name, relay, count
}

// taskCapableServerName returns the name of the single stateless-era
// server declaring the tasks extension, or "".
func (g *Gateway) taskCapableServerName() string {
	name, _, count := g.resolveTaskCapableServer()
	if count != 1 {
		return ""
	}
	return name
}

// taskCapableClient resolves the single downstream client the tasks
// proxy can relay to.
func (g *Gateway) taskCapableClient() (rawRelayer, error) {
	_, relay, count := g.resolveTaskCapableServer()
	switch {
	case count > 1:
		return nil, fmt.Errorf("multiple servers declare the %s extension; task handles cannot be routed without gateway-side task state, which gridctl does not keep", TasksExtensionID)
	case count == 0 || relay == nil:
		return nil, fmt.Errorf("no connected server declares the %s extension", TasksExtensionID)
	}
	return relay, nil
}
