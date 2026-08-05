package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
}

// SetHeaderSource installs the downstream auth header source. Must be called
// before Connect/Initialize; the client does not synchronize this field.
func (c *Client) SetHeaderSource(hs HeaderSource) {
	c.headerSource = hs
}

// applyAuthHeader attaches the downstream auth header when a source is set.
// Source errors abort the request and pass through unchanged so typed errors
// (e.g. authorization-required) reach the caller.
func (c *Client) applyAuthHeader(ctx context.Context, req *http.Request) error {
	if c.headerSource == nil {
		return nil
	}
	name, value, err := c.headerSource.AuthHeader(ctx)
	if err != nil {
		return err
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
	// stateless-era server never mints sessions; ignore any echo.
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" && era != EraStateless {
		c.mu.Lock()
		c.sessionID = sid
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

// Ping checks if the agent is reachable.
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

	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint, nil)
	if err != nil {
		return err
	}

	if err := c.applyAuthHeader(ctx, req); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Reachability is all Ping asserts, with one exception: when this client
	// has credentials configured, an auth challenge means the server is up
	// but the credential is missing or rejected, and callers need to
	// distinguish that from both "ready" and "unreachable". Without
	// configured credentials the status code stays ignored, preserving the
	// long-standing behavior for servers that 401 bare GETs (e.g. proxies)
	// while serving authenticated POST traffic fine.
	if c.headerSource != nil {
		if authErr := authRequiredFromResponse(resp, ""); authErr != nil {
			return authErr
		}
	}

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
