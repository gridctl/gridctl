package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/dockerclient"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// StdioClient communicates with an MCP server via container stdin/stdout.
type StdioClient struct {
	RPCClient
	containerID string
	cli         dockerclient.DockerClient
	requestID   atomic.Int64

	// Connection state
	connMu   sync.Mutex
	stdin    io.WriteCloser
	stdout   io.Reader
	attached bool
	cancel   context.CancelFunc

	// Response handling
	responses   map[int64]chan *Response
	responsesMu sync.Mutex
}

// NewStdioClient creates a new stdio-based MCP client.
func NewStdioClient(name, containerID string, cli dockerclient.DockerClient) *StdioClient {
	c := &StdioClient{
		containerID: containerID,
		cli:         cli,
		responses:   make(map[int64]chan *Response),
	}
	initRPCClient(&c.RPCClient, name, c)
	return c
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

	// Start reading responses with cancellation
	readerCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.readResponses(readerCtx)

	return nil
}

// readResponses reads JSON-RPC responses from stdout.
func (c *StdioClient) readResponses(ctx context.Context) {
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
	if err := c.sendStdio(req); err != nil {
		c.responsesMu.Lock()
		delete(c.responses, id)
		c.responsesMu.Unlock()
		c.logger.Debug("request failed", "method", method, "id", id, "error", err)
		return err
	}

	// Wait for response with timeout to prevent hanging on dead containers
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

// send sends a JSON-RPC notification via stdin (no response expected).
func (c *StdioClient) send(_ context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	return c.sendStdio(req)
}

// sendStdio writes a request to stdin.
func (c *StdioClient) sendStdio(req Request) error {
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

	if c.cancel != nil {
		c.cancel()
	}
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		if closer, ok := c.stdout.(io.Closer); ok {
			closer.Close()
		}
	}
	c.attached = false
	return nil
}
