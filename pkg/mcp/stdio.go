package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// StdioClient communicates with an MCP server via container stdin/stdout.
type StdioClient struct {
	name        string
	containerID string
	cli         dockerclient.DockerClient
	requestID   atomic.Int64
	logger      *slog.Logger

	mu            sync.RWMutex
	initialized   bool
	tools         []Tool
	serverInfo    ServerInfo
	toolWhitelist []string // Tool whitelist (empty = all tools)

	// Connection state
	connMu   sync.Mutex
	stdin    io.WriteCloser
	stdout   io.Reader
	attached bool

	// Response handling
	responses   map[int64]chan *Response
	responsesMu sync.Mutex
}

// NewStdioClient creates a new stdio-based MCP client.
func NewStdioClient(name, containerID string, cli dockerclient.DockerClient) *StdioClient {
	return &StdioClient{
		name:        name,
		containerID: containerID,
		cli:         cli,
		logger:      logging.NewDiscardLogger(),
		responses:   make(map[int64]chan *Response),
	}
}

// SetLogger sets the logger for this client.
func (c *StdioClient) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// Name returns the agent name.
func (c *StdioClient) Name() string {
	return c.name
}

// SetToolWhitelist sets the list of allowed tool names.
// Only tools in this list will be returned by Tools() and RefreshTools().
// An empty or nil list means all tools are allowed.
func (c *StdioClient) SetToolWhitelist(tools []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolWhitelist = tools
}

// Connect attaches to the container's stdin/stdout.
func (c *StdioClient) Connect(ctx context.Context) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.attached {
		return nil
	}

	// Attach to container
	resp, err := c.cli.ContainerAttach(ctx, c.containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("attaching to container: %w", err)
	}

	c.stdin = resp.Conn

	// Docker attach uses a multiplexed stream format with headers.
	// We need to demultiplex stdout from the stream using stdcopy.
	stdoutReader, stdoutWriter := io.Pipe()
	c.stdout = stdoutReader
	c.attached = true

	// Demultiplex the stream in the background
	go func() {
		defer stdoutWriter.Close()
		// StdCopy reads the multiplexed stream and writes stdout to the first writer
		_, _ = stdcopy.StdCopy(stdoutWriter, io.Discard, resp.Reader)
	}()

	// Start reading responses
	go c.readResponses()

	return nil
}

// readResponses reads JSON-RPC responses from stdout.
func (c *StdioClient) readResponses() {
	scanner := bufio.NewScanner(c.stdout)
	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			c.logger.Info("server output", "msg", string(line))
			continue
		}

		// Route response to waiting caller
		if resp.ID != nil {
			var id int64
			if err := json.Unmarshal(*resp.ID, &id); err == nil {
				c.responsesMu.Lock()
				if ch, ok := c.responses[id]; ok {
					ch <- &resp
					delete(c.responses, id)
				}
				c.responsesMu.Unlock()
			}
		}
	}
}

// Initialize performs the MCP initialize handshake.
func (c *StdioClient) Initialize(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
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

	c.mu.Lock()
	c.initialized = true
	c.serverInfo = result.ServerInfo
	c.mu.Unlock()

	// Send initialized notification
	_ = c.notify(ctx, "notifications/initialized", nil)

	return nil
}

// RefreshTools fetches the current tool list from the agent.
// If a tool whitelist has been set, only tools matching the whitelist are stored.
func (c *StdioClient) RefreshTools(ctx context.Context) error {
	var result ToolsListResult
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Filter tools if whitelist is set
	if len(c.toolWhitelist) > 0 {
		allowed := make(map[string]bool, len(c.toolWhitelist))
		for _, name := range c.toolWhitelist {
			allowed[name] = true
		}

		var filteredTools []Tool
		for _, tool := range result.Tools {
			if allowed[tool.Name] {
				filteredTools = append(filteredTools, tool)
			}
		}
		c.tools = filteredTools
	} else {
		c.tools = result.Tools
	}

	return nil
}

// Tools returns the cached tools for this agent.
func (c *StdioClient) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// CallTool invokes a tool on the agent.
func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
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
func (c *StdioClient) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// ServerInfo returns the server information.
func (c *StdioClient) ServerInfo() ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// call performs a JSON-RPC call via stdin/stdout.
func (c *StdioClient) call(ctx context.Context, method string, params any, result any) error {
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

	// Create response channel
	respCh := make(chan *Response, 1)
	c.responsesMu.Lock()
	c.responses[id] = respCh
	c.responsesMu.Unlock()

	c.logger.Debug("sending request", "method", method, "id", id)

	// Send request
	if err := c.send(req); err != nil {
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		c.logger.Debug("request failed", "method", method, "id", id, "error", err)
		return err
	}

	// Wait for response with timeout to prevent hanging on dead containers
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	select {
	case <-ctx.Done():
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		return ctx.Err()
	case <-timeout.C:
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		c.logger.Debug("request timed out", "method", method, "id", id)
		return fmt.Errorf("timeout waiting for response from container")
	case resp := <-respCh:
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
}

// notify sends a JSON-RPC notification.
func (c *StdioClient) notify(ctx context.Context, method string, params any) error {
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

	return c.send(req)
}

// send writes a request to stdin.
func (c *StdioClient) send(req Request) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if !c.attached || c.stdin == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	// Write JSON followed by newline
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing to stdin: %w", err)
	}

	return nil
}

// Close closes the connection.
func (c *StdioClient) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.stdin != nil {
		c.stdin.Close()
	}
	c.attached = false
	return nil
}
