package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// Client communicates with a downstream MCP server.
type Client struct {
	RPCClient
	endpoint        string
	httpClient      *http.Client
	requestID       atomic.Int64
	sessionID       string        // MCP session ID for stateful servers
	protocolVersion string        // negotiated at initialize; stamped on subsequent requests
	pingTimeout     time.Duration // 0 = use DefaultPingTimeout
	headerSource    HeaderSource  // optional downstream auth header (nil = none)
	reconnMu        sync.Mutex    // serializes Reconnect (stdio/process precedent)
}

// SetHeaderSource installs the downstream auth header source. Must be called
// before Connect/Initialize; the client does not synchronize this field.
func (c *Client) SetHeaderSource(hs HeaderSource) {
	c.headerSource = hs
}

// applyAuthHeader attaches the downstream auth header when a source is set.
// Source errors abort the request wrapped as AuthSourceError (message
// unchanged, typed errors still reachable through Unwrap) so callers can
// tell "the credential machinery failed before any exchange" from "the
// server answered".
func (c *Client) applyAuthHeader(ctx context.Context, req *http.Request) error {
	if c.headerSource == nil {
		return nil
	}
	name, value, err := c.headerSource.AuthHeader(ctx)
	if err != nil {
		return &AuthSourceError{Err: err}
	}
	if name != "" {
		req.Header.Set(name, value)
	}
	return nil
}

// setProtocolVersion records the version negotiated at initialize so
// sendHTTP can stamp the MCP-Protocol-Version header on every
// post-initialize request, as the Streamable HTTP spec requires.
func (c *Client) setProtocolVersion(v string) {
	c.mu.Lock()
	c.protocolVersion = v
	c.mu.Unlock()
}

// SetPingTimeout overrides the per-ping deadline used by Ping. Zero restores
// the default (DefaultPingTimeout).
func (c *Client) SetPingTimeout(d time.Duration) {
	c.pingTimeout = d
}

// NewClient creates a new MCP client for a downstream agent.
func NewClient(name, endpoint string) *Client {
	c := &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: DefaultRequestTimeout,
		},
	}
	initRPCClient(&c.RPCClient, name, c)
	return c
}

// Endpoint returns the agent endpoint.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// call performs a JSON-RPC call and decodes the result.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	id := c.requestID.Add(1)
	idBytes, _ := json.Marshal(id)
	rawID := json.RawMessage(idBytes)

	var paramsBytes json.RawMessage
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshaling params: %w", err)
		}
	}

	// Stateless-era servers require version, capabilities, and identity
	// in _meta on every request.
	if c.Era() == EraStateless {
		paramsBytes = stampStatelessMeta(ctx, paramsBytes, c.ProtocolVersion())
	}

	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      &rawID,
		Method:  method,
		Params:  paramsBytes,
	}

	c.logger.Debug("sending request", "method", method, "id", id)

	resp, err := c.sendHTTP(ctx, req)
	if err != nil {
		c.logger.Debug("request failed", "method", method, "id", id, "error", err)
		return err
	}

	if resp.Error != nil {
		c.logger.Debug("received error response", "method", method, "id", id, "code", resp.Error.Code, "message", resp.Error.Message)
		return &RPCError{Code: resp.Error.Code, Message: resp.Error.Message, Data: marshalErrorData(resp.Error.Data)}
	}

	c.logger.Debug("received response", "method", method, "id", id)

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("unmarshaling result: %w", err)
		}
	}

	return nil
}

// send sends a JSON-RPC notification (no response expected).
func (c *Client) send(ctx context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	_, err = c.sendHTTP(ctx, req)
	return err
}

// sendHTTP sends a request to the downstream agent via HTTP. On an auth
// challenge it invalidates a cached credential and retries exactly once, so
// an access token that expired mid-session heals silently when a refresh
// path exists.
func (c *Client) sendHTTP(ctx context.Context, req jsonrpc.Request) (*jsonrpc.Response, error) {
	resp, err := c.sendHTTPOnce(ctx, req)
	if err == nil {
		return resp, nil
	}
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		if inv, ok := c.headerSource.(TokenInvalidator); ok && inv.InvalidateToken() {
			return c.sendHTTPOnce(ctx, req)
		}
	}
	return nil, err
}

// sendHTTPOnce performs a single HTTP round trip for a JSON-RPC request.
func (c *Client) sendHTTPOnce(ctx context.Context, req jsonrpc.Request) (*jsonrpc.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	if err := c.applyAuthHeader(ctx, httpReq); err != nil {
		return nil, err
	}

	// Inject W3C traceparent/tracestate into outgoing request headers.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))

	// Include session ID if we have one (for stateful MCP servers) and the
	// protocol version negotiated at initialize (required by the spec on all
	// post-initialize requests). The stateless era has no sessions:
	// Mcp-Session-Id is never sent, and the required Mcp-Method/Mcp-Name
	// request-metadata headers are stamped instead. The probe stamps
	// them too; a modern server validates them on every request it
	// accepts.
	c.mu.RLock()
	era, protocolVersion := c.era, c.protocolVersion
	if era != EraStateless && c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.RUnlock()
	if protocolVersion != "" {
		httpReq.Header.Set("MCP-Protocol-Version", protocolVersion)
	}
	if era == EraStateless || req.Method == "server/discover" {
		if req.Method == "server/discover" && protocolVersion == "" {
			httpReq.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
		}
		httpReq.Header.Set(headerMcpMethod, req.Method)
		if name := mcpNameForRequest(req.Method, req.Params); name != "" {
			httpReq.Header.Set(headerMcpName, encodeHeaderValue(name))
		}
		// Forward unrecognized Mcp-Param-* headers from the upstream
		// request untouched, per the intermediary rules.
		for name, value := range mcpParamHeadersFromContext(ctx) {
			httpReq.Header.Set(name, value)
		}
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer httpResp.Body.Close()

	// Stateless-era servers acknowledge notifications with 202 and no
	// body. Only notifications: a 202 to a request keeps its original
	// loud failure (a handshake-era server answering initialize with
	// 202 must not silently register empty).
	if httpResp.StatusCode == http.StatusAccepted && req.ID == nil {
		return &jsonrpc.Response{JSONRPC: "2.0"}, nil
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		if authErr := authRequiredFromResponse(httpResp, string(body)); authErr != nil {
			return nil, authErr
		}
		statusErr := &HTTPStatusError{Status: httpResp.StatusCode, Body: string(body)}
		// Modern servers put JSON-RPC errors inside 400/404 bodies;
		// surface them so the era probe can recognize a modern peer.
		var errResp jsonrpc.Response
		if json.Unmarshal([]byte(body), &errResp) == nil && errResp.Error != nil {
			statusErr.RPCErr = &RPCError{Code: errResp.Error.Code, Message: errResp.Error.Message, Data: marshalErrorData(errResp.Error.Data)}
		}
		return nil, statusErr
	}

	// Capture session ID if provided (for stateful MCP servers). A
	// stateless-era server never mints sessions; ignore any echo. The
	// era is re-checked at write time so a response that raced a
	// re-negotiation cannot write a stale session over the flipped
	// client's state.
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" && era != EraStateless {
		c.mu.Lock()
		if c.era != EraStateless {
			c.sessionID = sid
		}
		c.mu.Unlock()
	}

	// Check if response is SSE format (text/event-stream)
	contentType := httpResp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return c.parseSSEResponse(httpResp.Body)
	}

	var resp jsonrpc.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &resp, nil
}

// parseSSEResponse parses a Server-Sent Events formatted response.
// SSE streams may contain multiple events (notifications + result).
// We look for the response with an ID field (the actual result), skipping notifications.
func (c *Client) parseSSEResponse(body io.Reader) (*jsonrpc.Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading SSE response: %w", err)
	}

	// Parse SSE format: look for "data: " lines
	// Some MCP servers send multiple SSE events (notifications followed by result).
	// We need to find the response with an ID field (not a notification).
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var resp jsonrpc.Response
			if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
				// Skip malformed lines
				continue
			}
			// Return the response that has an ID (actual result), not notifications
			// Notifications have a "method" field but no "id" field
			if resp.ID != nil {
				return &resp, nil
			}
		}
	}

	return nil, fmt.Errorf("no response with ID found in SSE stream")
}

// Ping checks server liveness with an era-appropriate protocol request.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeoutOrDefault(c.pingTimeout))
	defer cancel()

	// On the stateless generation the health check exercises the
	// protocol, not bare reachability: server/discover is the era's
	// always-available method (the stdio/process precedent), so a
	// server that changed generation underneath a live connection
	// fails into the health channel instead of degrading as per-call
	// tool errors.
	if c.Era() == EraStateless {
		var result DiscoverResult
		if err := c.call(ctx, "server/discover", map[string]any{"_meta": statelessMetaMap(c.ProtocolVersion())}, &result); err != nil {
			return err
		}
		return verifyDiscoverHealth(result)
	}

	return c.pingHandshake(ctx)
}

// pingHandshake health-checks a handshake-era server with a protocol-level
// ping (stdio/process parity) instead of the old bare reachability GET,
// which a server redeployed as stateless-only still answers 200 while
// every tool call fails (#1088). Health fails only on transport-level
// unreachability, an auth signal, or positive evidence of a generation
// flip; every other answer preserves the long-standing tolerance for lax
// legacy servers that reject unknown methods and proxies that reject
// unauthenticated requests while serving real traffic fine.
func (c *Client) pingHandshake(ctx context.Context) error {
	// A client that once negotiated but now has no resolved era is a
	// failed re-negotiation (Reconnect resets the era before probing).
	// Era-less requests cannot serve tool calls, so health must keep
	// failing until Reconnect converges rather than reading the
	// reachable server as healthy (the stdio "not connected" latch
	// equivalent). Pre-Initialize readiness probes also run era-less but
	// are not initialized yet, and must stay tolerant: a modern answer
	// there means the server is up and Initialize will adopt it, and
	// failing health would deadlock registration.
	if c.Era() == "" && c.IsInitialized() {
		return errors.New("protocol generation unresolved after a failed re-negotiation; awaiting reconnect")
	}

	err := c.call(ctx, "ping", nil, nil)
	if err == nil {
		return nil
	}

	// Broker-managed auth state must keep surfacing so the health monitor
	// can distinguish needs-auth from unreachable.
	var needsAuth *NeedsAuthError
	if errors.As(err, &needsAuth) {
		return err
	}
	// An auth challenge is actionable with credentials configured; without
	// them any completed response counts as reachable (some proxies reject
	// unauthenticated requests while authenticated traffic works).
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		if c.headerSource != nil {
			return err
		}
		return nil
	}
	// A header-source failure happened before any exchange with the
	// server; tolerating it would read a token-endpoint outage as a
	// healthy server while every tool call fails on the same error.
	var srcErr *AuthSourceError
	if errors.As(err, &srcErr) {
		return err
	}

	// Generation-flip detection. Only meaningful once the era actually
	// resolved to handshake (see the latch above for the era-less
	// states). Skipped under a handshake pin: the pin makes Initialize
	// (and so Reconnect) unable to ever adopt the stateless era, so
	// failing health would strand the server with no recovery path.
	if c.Era() == EraHandshake && c.generationPinValue() != GenerationHandshake {
		if isRecognizedModernError(err) {
			// These codes exist only in the stateless era; the peer
			// validated modern request rules, so the flip is certain.
			return fmt.Errorf("handshake-era health check: server enforces stateless-era (%s) request rules; generation flip detected: %w", StatelessProtocolVersion, err)
		}
		if rpcErr := rpcErrorFrom(err); rpcErr != nil && rpcErr.Code == jsonrpc.MethodNotFound {
			// Ambiguous: a stateless-only server removed ping, but a lax
			// legacy server may reject it too. Confirm with a read-only
			// server/discover probe before failing health.
			if c.confirmGenerationFlip(ctx) {
				return fmt.Errorf("handshake-era health check: server answers as a stateless-era (%s) peer; generation flip detected: %w", StatelessProtocolVersion, err)
			}
			return nil
		}
	}

	// A deadline or cancellation that fired after response headers
	// arrived (a stalled body) surfaces as a bare context error, not a
	// url.Error; both are unreachability.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}

	// Transport-level failures (connection refused, DNS, dial timeout)
	// are unreachability regardless of era.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return err
	}

	// Everything else means the server completed an HTTP exchange:
	// odd statuses, JSON-RPC errors without flip evidence, undecodable
	// bodies. All stay healthy, matching the old reachability semantics
	// (the bare GET never read the body); tool calls surface real
	// failures.
	return nil
}

// confirmGenerationFlip reports whether the server positively answers as
// a stateless-era peer. Read-only: unlike the Initialize probe it never
// adopts the era or version (Ping detects, Reconnect converges). The
// stale handshake MCP-Protocol-Version header this request carries makes
// a strict modern server reject it with a modern-only error code, which
// confirms the flip just as well as a discover result does.
func (c *Client) confirmGenerationFlip(ctx context.Context) bool {
	var result DiscoverResult
	err := c.call(ctx, "server/discover", map[string]any{"_meta": statelessMetaMap(StatelessProtocolVersion)}, &result)
	if err != nil {
		return isRecognizedModernError(err)
	}
	return discoverIndicatesModern(result)
}

// Reconnect re-resolves the protocol generation and refreshes the tool
// list on the live client, so a health-detected generation flip converges
// instead of requiring a gateway restart. HTTP holds no persistent
// connection, so unlike stdio/process there is no transport teardown.
// Thread-safe: concurrent callers block until reconnection completes.
func (c *Client) Reconnect(ctx context.Context) error {
	c.reconnMu.Lock()
	defer c.reconnMu.Unlock()

	c.logger.Info("reconnecting to HTTP server")

	// Initialize resets the era and negotiated version, but the session is
	// transport state it knows nothing about: a stale handshake-era session
	// ID must not leak into the re-negotiated connection.
	c.mu.Lock()
	c.sessionID = ""
	c.mu.Unlock()

	if err := c.Initialize(ctx); err != nil {
		return fmt.Errorf("reinitialize: %w", err)
	}

	// The tool cache must be repopulated before returning: the gateway
	// verifies schema pins against Tools() right after a successful
	// reconnect, and a flipped server's post-flip schemas are the ones
	// that must be compared. On failure the era goes back to unresolved
	// so the Ping latch keeps health failing: a half-reconnected client
	// reading healthy would serve stale pre-flip tools and skip pin
	// verification forever.
	if err := c.RefreshTools(ctx); err != nil {
		c.SetEra("")
		return fmt.Errorf("refresh tools: %w", err)
	}

	c.logger.Info("reconnected to HTTP server")
	return nil
}

// authRequiredFromResponse returns an AuthRequiredError for a 401, or for a
// 403 that carries a WWW-Authenticate challenge (e.g. insufficient_scope).
// Returns nil for every other response.
func authRequiredFromResponse(resp *http.Response, body string) *AuthRequiredError {
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode == http.StatusUnauthorized ||
		(resp.StatusCode == http.StatusForbidden && challenge != "") {
		return &AuthRequiredError{Status: resp.StatusCode, Challenge: challenge, Body: body}
	}
	return nil
}
