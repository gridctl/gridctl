package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ProcessClient communicates with an MCP server via a local process stdin/stdout.
type ProcessClient struct {
	name      string
	command   []string
	workDir   string
	env       []string
	requestID atomic.Int64

	mu            sync.RWMutex
	initialized   bool
	tools         []Tool
	serverInfo    ServerInfo
	toolWhitelist []string // Tool whitelist (empty = all tools)

	// Process state
	procMu  sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	started bool

	// Response handling
	responses   map[int64]chan *Response
	responsesMu sync.Mutex
}

// NewProcessClient creates a new process-based MCP client.
// The command is executed with the given working directory and environment.
// Environment variables are merged with the current process environment.
func NewProcessClient(name string, command []string, workDir string, env map[string]string) *ProcessClient {
	// Build environment: inherit current env and merge specified vars
	envList := os.Environ()
	for k, v := range env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	return &ProcessClient{
		name:      name,
		command:   command,
		workDir:   workDir,
		env:       envList,
		responses: make(map[int64]chan *Response),
	}
}

// Name returns the agent name.
func (c *ProcessClient) Name() string {
	return c.name
}

// SetToolWhitelist sets the list of allowed tool names.
// Only tools in this list will be returned by Tools() and RefreshTools().
// An empty or nil list means all tools are allowed.
func (c *ProcessClient) SetToolWhitelist(tools []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolWhitelist = tools
}

// Connect starts the process and attaches to its stdin/stdout.
func (c *ProcessClient) Connect(ctx context.Context) error {
	c.procMu.Lock()
	defer c.procMu.Unlock()

	if c.started {
		return nil
	}

	if len(c.command) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Create the command
	c.cmd = exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	c.cmd.Dir = c.workDir
	c.cmd.Env = c.env

	// Get stdin pipe
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}
	c.stdin = stdin

	// Get stdout pipe
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	c.stdout = stdout

	// Discard stderr (or could log it)
	c.cmd.Stderr = nil

	// Start the process
	if err := c.cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("starting process: %w", err)
	}

	c.started = true

	// Start reading responses
	go c.readResponses()

	return nil
}

// readResponses reads JSON-RPC responses from stdout.
func (c *ProcessClient) readResponses() {
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
			// Not a valid JSON-RPC response, might be log output
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
func (c *ProcessClient) Initialize(ctx context.Context) error {
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
func (c *ProcessClient) RefreshTools(ctx context.Context) error {
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
func (c *ProcessClient) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// CallTool invokes a tool on the agent.
func (c *ProcessClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
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
func (c *ProcessClient) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// ServerInfo returns the server information.
func (c *ProcessClient) ServerInfo() ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// call performs a JSON-RPC call via stdin/stdout.
func (c *ProcessClient) call(ctx context.Context, method string, params any, result any) error {
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

	// Send request
	if err := c.send(req); err != nil {
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		return err
	}

	// Wait for response with timeout to prevent hanging on dead processes
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
		return fmt.Errorf("timeout waiting for response from process")
	case resp := <-respCh:
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
}

// notify sends a JSON-RPC notification.
func (c *ProcessClient) notify(ctx context.Context, method string, params any) error {
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
func (c *ProcessClient) send(req Request) error {
	c.procMu.Lock()
	defer c.procMu.Unlock()

	if !c.started || c.stdin == nil {
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

// Close terminates the process gracefully.
// Sends SIGTERM, waits up to 5 seconds, then sends SIGKILL if still running.
func (c *ProcessClient) Close() error {
	c.procMu.Lock()
	defer c.procMu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	// Close stdin first to signal EOF
	if c.stdin != nil {
		c.stdin.Close()
	}

	// Send SIGTERM for graceful shutdown
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process might have already exited
		return nil
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully
		return nil
	case <-time.After(5 * time.Second):
		// Force kill - ignore error since process may have already exited
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}
