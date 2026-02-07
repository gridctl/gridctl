package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
)

// Client communicates with a downstream MCP server.
type Client struct {
	ClientBase
	name       string
	endpoint   string
	httpClient *http.Client
	requestID  atomic.Int64
	logger     *slog.Logger
	sessionID  string // MCP session ID for stateful servers
}

// NewClient creates a new MCP client for a downstream agent.
func NewClient(name, endpoint string) *Client {
	return &Client{
		name:     name,
		endpoint: endpoint,
		logger:   logging.NewDiscardLogger(),
		httpClient: &http.Client{
			Timeout: DefaultRequestTimeout,
		},
	}
}

// SetLogger sets the logger for this client.
func (c *Client) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// Name returns the agent name.
func (c *Client) Name() string {
	return c.name
}

// Endpoint returns the agent endpoint.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Initialize performs the MCP initialize handshake.
func (c *Client) Initialize(ctx context.Context) error {
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
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	c.SetInitialized(result.ServerInfo)

	// Send initialized notification (non-fatal, some servers may not require this)
	_ = c.notify(ctx, "notifications/initialized", nil)

	return nil
}

// RefreshTools fetches the current tool list from the agent.
// If a tool whitelist has been set, only tools matching the whitelist are stored.
func (c *Client) RefreshTools(ctx context.Context) error {
	var result ToolsListResult
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	c.SetTools(result.Tools)
	return nil
}

// CallTool invokes a tool on the downstream agent.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	params := ToolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	var result ToolCallResult
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return nil, fmt.Errorf("tools/call: %w", err)
	}

	return &result, nil
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

	req := Request{
		JSONRPC: "2.0",
		ID:      &rawID,
		Method:  method,
		Params:  paramsBytes,
	}

	c.logger.Debug("sending request", "method", method, "id", id)

	resp, err := c.send(ctx, req)
	if err != nil {
		c.logger.Debug("request failed", "method", method, "id", id, "error", err)
		return err
	}

	if resp.Error != nil {
		c.logger.Debug("received error response", "method", method, "id", id, "code", resp.Error.Code, "message", resp.Error.Message)
		return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	c.logger.Debug("received response", "method", method, "id", id)

	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("unmarshaling result: %w", err)
		}
	}

	return nil
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(ctx context.Context, method string, params any) error {
	var paramsBytes json.RawMessage
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshaling params: %w", err)
		}
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	}

	_, err := c.send(ctx, req)
	return err
}

// send sends a request to the downstream agent.
func (c *Client) send(ctx context.Context, req Request) (*Response, error) {
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

	// Include session ID if we have one (for stateful MCP servers)
	c.mu.RLock()
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.RUnlock()

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(body))
	}

	// Capture session ID if provided (for stateful MCP servers)
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	// Check if response is SSE format (text/event-stream)
	contentType := httpResp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return c.parseSSEResponse(httpResp.Body)
	}

	var resp Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &resp, nil
}

// parseSSEResponse parses a Server-Sent Events formatted response.
// SSE streams may contain multiple events (notifications + result).
// We look for the response with an ID field (the actual result), skipping notifications.
func (c *Client) parseSSEResponse(body io.Reader) (*Response, error) {
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
			var resp Response
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
	// Try to connect with a short timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
