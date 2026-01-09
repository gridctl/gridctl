package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client communicates with a downstream MCP server.
type Client struct {
	name       string
	endpoint   string
	httpClient *http.Client
	requestID  atomic.Int64

	mu          sync.RWMutex
	initialized bool
	tools       []Tool
	serverInfo  ServerInfo
	sessionID   string // MCP session ID for stateful servers
}

// NewClient creates a new MCP client for a downstream agent.
func NewClient(name, endpoint string) *Client {
	return &Client{
		name:     name,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
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
		ProtocolVersion: "2024-11-05",
		ClientInfo: ClientInfo{
			Name:    "agentlab-gateway",
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

	c.mu.Lock()
	c.initialized = true
	c.serverInfo = result.ServerInfo
	c.mu.Unlock()

	// Send initialized notification (non-fatal, some servers may not require this)
	_ = c.notify(ctx, "notifications/initialized", nil)

	return nil
}

// RefreshTools fetches the current tool list from the agent.
func (c *Client) RefreshTools(ctx context.Context) error {
	var result ToolsListResult
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	c.mu.Lock()
	c.tools = result.Tools
	c.mu.Unlock()

	return nil
}

// Tools returns the cached tools for this agent.
func (c *Client) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
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

// IsInitialized returns whether the client has been initialized.
func (c *Client) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// ServerInfo returns the server information.
func (c *Client) ServerInfo() ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
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

	resp, err := c.send(ctx, req)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

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
func (c *Client) parseSSEResponse(body io.Reader) (*Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading SSE response: %w", err)
	}

	// Parse SSE format: look for "data: " lines
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var resp Response
			if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
				return nil, fmt.Errorf("decoding SSE JSON: %w", err)
			}
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("no data field found in SSE response")
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
