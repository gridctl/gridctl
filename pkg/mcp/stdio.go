package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/jsonrpc"

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
	closeMu  sync.Mutex
	writeMu  sync.Mutex
	readDone chan struct{}
	stdin    io.WriteCloser
	stdout   io.Reader
	attached bool
	retired  bool
	cancel   context.CancelFunc

	// Reconnection serialization
	reconnMu sync.Mutex

	// Response handling
	responses   map[int64]chan *jsonrpc.Response
	responsesMu sync.Mutex

	pingTimeout time.Duration // 0 = use DefaultPingTimeout
}

// SetPingTimeout overrides the per-ping deadline used by Ping. Zero restores
// the default (DefaultPingTimeout).
func (c *StdioClient) SetPingTimeout(d time.Duration) {
	c.pingTimeout = d
}

// ContainerID returns the docker container id this client was bound to.
func (c *StdioClient) ContainerID() string {
	return c.containerID
}

// NewStdioClient creates a new stdio-based MCP client.
func NewStdioClient(name, containerID string, cli dockerclient.DockerClient) *StdioClient {
	c := &StdioClient{
		containerID: containerID,
		cli:         cli,
		responses:   make(map[int64]chan *jsonrpc.Response),
	}
	initRPCClient(&c.RPCClient, name, c)
	return c
}

// Connect attaches to the container's stdin/stdout.
func (c *StdioClient) Connect(ctx context.Context) error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.retired {
		return fmt.Errorf("container client retired")
	}

	if c.attached {
		return nil
	}
	if c.readDone != nil {
		select {
		case <-c.readDone:
		default:
			return fmt.Errorf("previous container response cleanup pending")
		}
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
	readDone := make(chan struct{})
	c.readDone = readDone
	go func() { defer close(readDone); c.readResponses(readerCtx, stdoutReader) }()

	return nil
}

func (c *StdioClient) retire() error {
	c.connMu.Lock()
	c.retired = true
	c.connMu.Unlock()
	return c.Close()
}

// readResponses reads JSON-RPC responses from stdout.
// stdout is passed as a parameter to capture the value at goroutine launch
// time (under connMu), avoiding a data race with Reconnect.
func (c *StdioClient) readResponses(ctx context.Context, stdout io.Reader) {
	defer c.drainPendingRequests()

	scanner := bufio.NewScanner(stdout)
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

// drainPendingRequests sends error responses to all pending callers so they
// fail immediately instead of waiting for the 30s request timeout.
func (c *StdioClient) drainPendingRequests() {
	c.responsesMu.Lock()
	defer c.responsesMu.Unlock()

	for id, ch := range c.responses {
		select {
		case ch <- &jsonrpc.Response{
			JSONRPC: "2.0",
			Error:   &jsonrpc.Error{Code: jsonrpc.InternalError, Message: "connection lost"},
		}:
		default:
		}
		delete(c.responses, id)
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

	// Inject _meta.traceparent for downstream MCP servers that support it.
	paramsBytes = injectMetaTraceparent(ctx, paramsBytes)
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

	// Create response channel
	respCh := make(chan *jsonrpc.Response, 1)
	c.responsesMu.Lock()
	c.responses[id] = respCh
	c.responsesMu.Unlock()

	c.logger.Debug("sending request", "method", method, "id", id)

	// Send request
	if err := c.sendStdioContext(ctx, req); err != nil {
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
}

// send sends a JSON-RPC notification via stdin (no response expected).
func (c *StdioClient) send(ctx context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	return c.sendStdioContext(ctx, req)
}

// sendStdio writes a request to stdin.
func (c *StdioClient) sendStdio(req jsonrpc.Request) error {
	return c.sendStdioContext(context.Background(), req)
}

func (c *StdioClient) sendStdioContext(ctx context.Context, req jsonrpc.Request) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()
	c.connMu.Lock()

	if !c.attached || c.stdin == nil {
		c.connMu.Unlock()
		return fmt.Errorf("not connected")
	}
	stdin := c.stdin
	c.connMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	written := make(chan error, 1)
	go func() {
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if err := ctx.Err(); err != nil {
			written <- err
			return
		}
		_, err := stdin.Write(append(data, '\n'))
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			return fmt.Errorf("writing to stdin: %w", err)
		}
		return nil
	case <-ctx.Done():
		if err := stdin.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			c.logger.Debug("closing interrupted container input failed")
		}
		return ctx.Err()
	}
}

// Reconnect closes the existing connection and re-establishes it, including the
// MCP handshake and tool refresh. Thread-safe: concurrent callers will block until
// reconnection completes.
func (c *StdioClient) Reconnect(ctx context.Context) error {
	c.reconnMu.Lock()
	defer c.reconnMu.Unlock()

	c.logger.Info("reconnecting to container")

	// Close existing connection (cancels goroutines, closes pipes).
	// The deferred drainPendingRequests in readResponses will clear the
	// response map, so no explicit reset is needed here.
	if err := c.Close(); err != nil {
		return fmt.Errorf("closing previous container connection: %w", err)
	}

	// Re-establish connection
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("reconnect: %w", err)
	}

	// Re-do MCP handshake
	if err := c.Initialize(ctx); err != nil {
		return errors.Join(fmt.Errorf("reinitialize: %w", err), c.Close())
	}

	// Refresh tool list
	if err := c.RefreshTools(ctx); err != nil {
		return errors.Join(fmt.Errorf("refresh tools: %w", err), c.Close())
	}

	c.logger.Info("reconnected to container")
	return nil
}

// Ping checks if the container stdio connection is alive by sending a JSON-RPC ping.
func (c *StdioClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeoutOrDefault(c.pingTimeout))
	defer cancel()

	// Verify connection state first
	c.connMu.Lock()
	attached := c.attached
	c.connMu.Unlock()
	if !attached {
		return fmt.Errorf("not connected")
	}

	// Send a liveness request and wait for any response. The stateless
	// generation removed ping; server/discover is its spec-sanctioned
	// always-available method, so health checks use it there instead of
	// tripping -32601 into a permanent unhealthy loop.
	if c.Era() == EraStateless {
		var result DiscoverResult
		if err := c.call(ctx, "server/discover", map[string]any{"_meta": statelessMetaMap(c.ProtocolVersion())}, &result); err != nil {
			return err
		}
		return verifyDiscoverHealth(result)
	}
	return c.call(ctx, "ping", nil, nil)
}

// Close closes the connection.
func (c *StdioClient) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.connMu.Lock()

	if c.cancel != nil {
		c.cancel()
	}
	stdin, stdout, readDone := c.stdin, c.stdout, c.readDone
	c.attached = false
	c.connMu.Unlock()
	var cleanupErrors []error
	if stdin != nil {
		if err := stdin.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("container input cleanup failed"))
		}
	}
	if stdout != nil {
		if closer, ok := stdout.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("container output cleanup failed"))
			}
		}
	}
	if readDone != nil {
		select {
		case <-readDone:
		case <-time.After(processKillGracePeriod):
			cleanupErrors = append(cleanupErrors, fmt.Errorf("container response cleanup deadline exceeded"))
		}
	}
	return errors.Join(cleanupErrors...)
}
