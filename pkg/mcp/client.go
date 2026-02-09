package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// Client communicates with a downstream MCP server.
type Client struct {
	RPCClient
	endpoint   string
	httpClient *http.Client
	requestID  atomic.Int64
	sessionID  string // MCP session ID for stateful servers
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

// send sends a JSON-RPC notification (no response expected).
func (c *Client) send(ctx context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	_, err = c.sendHTTP(ctx, req)
	return err
}

// sendHTTP sends a request to the downstream agent via HTTP.
func (c *Client) sendHTTP(ctx context.Context, req jsonrpc.Request) (*jsonrpc.Response, error) {
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

