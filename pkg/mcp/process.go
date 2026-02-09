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

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

const processKillGracePeriod = 5 * time.Second

// ProcessClient communicates with an MCP server via a local process stdin/stdout.
type ProcessClient struct {
	RPCClient
	command   []string
	workDir   string
	env       []string
	requestID atomic.Int64

	// Process state
	procMu  sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	started bool
	cancel  context.CancelFunc

	// Response handling
	responses   map[int64]chan *jsonrpc.Response
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

	c := &ProcessClient{
		command:   command,
		workDir:   workDir,
		env:       envList,
		responses: make(map[int64]chan *jsonrpc.Response),
	}
	initRPCClient(&c.RPCClient, name, c)
	return c
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

	// Capture stderr and log output at WARN level
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		c.cmd.Stderr = nil // fall back to discard on pipe error
	}

	// Start the process
	if err := c.cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("starting process: %w", err)
	}

	c.started = true

	// Start reading responses and stderr with cancellation
	readerCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.readResponses(readerCtx)
	if stderr != nil {
		go c.readStderr(readerCtx, stderr)
	}

	return nil
}

// readResponses reads JSON-RPC responses from stdout.
func (c *ProcessClient) readResponses(ctx context.Context) {
	scanner := bufio.NewScanner(c.stdout)
	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp jsonrpc.Response
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

// readStderr reads lines from the process stderr and logs them.
func (c *ProcessClient) readStderr(ctx context.Context, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.logger.Warn("server stderr", "output", scanner.Text())
	}
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

	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      &rawID,
		Method:  method,
		Params:  paramsBytes,
	}

	// Create response channel
	respCh := make(chan *jsonrpc.Response, 1)
	c.responsesMu.Lock()
	c.responses[id] = respCh
	c.responsesMu.Unlock()

	c.logger.Debug("sending request", "method", method, "id", id)

	// Send request
	if err := c.sendStdio(req); err != nil {
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		c.logger.Debug("request failed", "method", method, "id", id, "error", err)
		return err
	}

	// Wait for response with timeout to prevent hanging on dead processes
	timeout := time.NewTimer(DefaultRequestTimeout)
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
		return fmt.Errorf("timeout waiting for response from process")
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

// send sends a JSON-RPC notification via stdin (no response expected).
func (c *ProcessClient) send(_ context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	return c.sendStdio(req)
}

// sendStdio writes a request to stdin.
func (c *ProcessClient) sendStdio(req jsonrpc.Request) error {
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

	// Cancel reader goroutines
	if c.cancel != nil {
		c.cancel()
	}

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
	case <-time.After(processKillGracePeriod):
		// Force kill - ignore error since process may have already exited
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}
