package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/vault"
)

const processKillGracePeriod = 5 * time.Second

// ProcessClient communicates with an MCP server via a local process stdin/stdout.
type ProcessClient struct {
	RPCClient
	command     []string
	workDir     string
	env         []string
	execution   *execution.ExecutionContract
	remote      bool
	envConflict bool
	requestID   atomic.Int64

	// Process state
	procMu      sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.Reader
	started     bool
	exited      bool
	cancel      context.CancelFunc
	ownerCancel context.CancelFunc
	retired     bool
	done        chan struct{}
	readDone    chan struct{}
	writeMu     sync.Mutex
	closeMu     sync.Mutex

	// Reconnection serialization
	reconnMu sync.Mutex

	// Response handling
	responses   map[int64]chan *jsonrpc.Response
	responsesMu sync.Mutex

	pingTimeout time.Duration // 0 = use DefaultPingTimeout
}

// SetPingTimeout overrides the per-ping deadline used by Ping. Zero restores
// the default (DefaultPingTimeout).
func (c *ProcessClient) SetPingTimeout(d time.Duration) {
	c.pingTimeout = d
}

// NewProcessClient creates a new process-based MCP client.
// The command is executed with the given working directory and environment.
// Environment variables are merged with the current process environment.
func NewProcessClient(name string, command []string, workDir string, env map[string]string) *ProcessClient {
	return newProcessClient(name, command, workDir, env, nil)
}

func newProcessClient(name string, command []string, workDir string, env map[string]string, execution *execution.ExecutionContract) *ProcessClient {
	// Normalize the inherited environment so configured values replace rather
	// than duplicate ambient entries. Internal credentials never cross this
	// downstream process boundary.
	merged := make(map[string]string)
	allowed := map[string]bool{}
	if execution != nil && execution.Inherit != nil {
		for _, name := range *execution.Inherit {
			allowed[processEnvKey(name, execution != nil, runtime.GOOS)] = true
		}
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		key = processEnvKey(key, execution != nil, runtime.GOOS)
		if !ok || vault.IsInternalCredential(key) {
			continue
		}
		if execution != nil && execution.Inherit != nil && !allowed[key] {
			continue
		}
		merged[key] = value
	}
	seenExplicit, envConflict := map[string]bool{}, false
	for key, v := range env {
		k := processEnvKey(key, execution != nil, runtime.GOOS)
		if seenExplicit[k] && execution != nil {
			envConflict = true
		}
		seenExplicit[k] = true
		if vault.IsInternalCredential(k) {
			continue
		}
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	envList := make([]string, 0, len(keys))
	for _, key := range keys {
		envList = append(envList, fmt.Sprintf("%s=%s", key, merged[key]))
	}

	c := &ProcessClient{
		envConflict: envConflict,
		execution:   execution,
		command:     command,
		workDir:     workDir,
		env:         envList,
		responses:   make(map[int64]chan *jsonrpc.Response),
	}
	initRPCClient(&c.RPCClient, name, c)
	return c
}

// Connect starts the process and attaches to its stdin/stdout.
func (c *ProcessClient) Connect(ctx context.Context) error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.procMu.Lock()
	defer c.procMu.Unlock()
	if c.retired {
		return fmt.Errorf("process client retired")
	}

	if c.started {
		return nil
	}
	if c.readDone != nil {
		select {
		case <-c.readDone:
		default:
			return fmt.Errorf("previous process output cleanup pending; close before reconnecting")
		}
	}

	if len(c.command) == 0 {
		return fmt.Errorf("no command specified")
	}
	if c.envConflict {
		return fmt.Errorf("execution.environment: duplicate platform-equivalent names; use one canonical name for explicit and scoped delivery")
	}

	// Create the command
	if c.execution != nil && c.execution.Lookup == "absolute" && !filepath.IsAbs(c.command[0]) {
		return fmt.Errorf("execution.lookup: absolute executable required")
	}
	c.cmd = exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	if c.execution != nil && c.cmd.Err == nil && !strings.ContainsAny(c.command[0], "/\\") && !filepath.IsAbs(c.cmd.Path) {
		c.cmd.Err = exec.ErrDot
	}
	configureProcessGroup(c.cmd, !c.remote && c.execution != nil)
	c.cmd.Dir = c.workDir
	c.cmd.Env = c.env

	// Get stdin pipe
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}
	c.stdin = stdin

	// Own the output pipes so Wait cannot close them ahead of the readers.
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	c.cmd.Stdout = stdoutWriter
	c.stdout = stdout

	// Capture stderr and log output at WARN level
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		stdoutWriter.Close()
		return fmt.Errorf("creating stderr pipe: %w", err)
	}
	c.cmd.Stderr = stderrWriter

	// Start the process
	if err := c.cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stdoutWriter.Close()
		stderr.Close()
		stderrWriter.Close()
		if c.execution != nil && !errors.Is(err, exec.ErrDot) {
			return fmt.Errorf("execution.start: local command failed; check executable access and required bootstrap caches or networking")
		}
		return fmt.Errorf("starting process: %w", err)
	}
	stdoutWriter.Close()
	stderrWriter.Close()

	c.started = true
	c.exited = false

	// Start reading responses and stderr with cancellation
	readerCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	readDone := make(chan struct{})
	c.readDone = readDone
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		defer stdout.Close()
		c.readResponses(readerCtx, stdout)
	}()
	go func() {
		defer readers.Done()
		defer stderr.Close()
		c.readStderr(readerCtx, stderr)
	}()
	go func() { readers.Wait(); close(readDone) }()
	done := make(chan struct{})
	c.done = done
	cmd := c.cmd
	go func() {
		err := cmd.Wait()
		c.procMu.Lock()
		if c.cmd == cmd {
			c.started = false
			c.exited = true
		}
		c.procMu.Unlock()
		if err != nil {
			c.logger.Debug("process exited", "success", false)
		}
		close(done)
		// Descendants may retain output descriptors after the child exits.
		timer := time.NewTimer(processKillGracePeriod)
		defer timer.Stop()
		select {
		case <-readDone:
		case <-timer.C:
			cancel()
			stdout.Close()
			stderr.Close()
		}
	}()

	return nil
}

func (c *ProcessClient) retire() error {
	c.procMu.Lock()
	c.retired = true
	c.procMu.Unlock()
	return c.Close()
}

func processEnvKey(key string, selected bool, platform string) string {
	if selected && platform == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func (c *ProcessClient) connectOwned(ctx context.Context) (func() bool, error) {
	childCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.procMu.Lock()
	c.ownerCancel = cancel
	c.procMu.Unlock()
	stopCancellation := context.AfterFunc(ctx, cancel)
	if err := ctx.Err(); err != nil {
		return stopCancellation, err
	}
	return stopCancellation, c.Connect(childCtx)
}

// readResponses reads JSON-RPC responses from stdout.
// stdout is passed as a parameter to capture the value at goroutine launch
// time (under procMu), avoiding a data race with Reconnect clearing c.stdout.
func (c *ProcessClient) readResponses(ctx context.Context, stdout io.Reader) {
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
func (c *ProcessClient) drainPendingRequests() {
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
func (c *ProcessClient) send(ctx context.Context, method string, params any) error {
	req, err := buildNotification(method, params)
	if err != nil {
		return err
	}

	return c.sendStdioContext(ctx, req)
}

// sendStdio writes a request to stdin.
func (c *ProcessClient) sendStdio(req jsonrpc.Request) error {
	return c.sendStdioContext(context.Background(), req)
}

func (c *ProcessClient) sendStdioContext(ctx context.Context, req jsonrpc.Request) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()
	c.procMu.Lock()
	if !c.started || c.stdin == nil {
		c.procMu.Unlock()
		return fmt.Errorf("not connected")
	}
	stdin := c.stdin
	c.procMu.Unlock()

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
		// A partial JSON frame cannot safely be retried on this stream.
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			c.logger.Debug("closing interrupted process input failed")
		}
		return ctx.Err()
	}
}

// Reconnect terminates the existing process and starts a new one, including the
// MCP handshake and tool refresh. Thread-safe: concurrent callers will block until
// reconnection completes.
func (c *ProcessClient) Reconnect(ctx context.Context) (reconnectErr error) {
	c.reconnMu.Lock()
	defer c.reconnMu.Unlock()
	c.procMu.Lock()
	owned := c.ownerCancel != nil
	c.procMu.Unlock()
	var stopCancellation func() bool
	defer func() {
		if stopCancellation != nil {
			if !stopCancellation() && reconnectErr == nil {
				reconnectErr = ctx.Err()
			}
			if reconnectErr != nil {
				reconnectErr = errors.Join(reconnectErr, c.Close())
			}
		}
	}()

	c.logger.Info("reconnecting process")

	// Close existing process (cancels goroutines, sends SIGTERM/SIGKILL).
	// The deferred drainPendingRequests in readResponses will clear the
	// response map, so no explicit reset is needed here.
	if err := c.Close(); err != nil {
		return fmt.Errorf("close previous process: %w", err)
	}

	// Reset process state for fresh connection
	c.procMu.Lock()
	c.cmd = nil
	c.stdin = nil
	c.stdout = nil
	c.procMu.Unlock()

	// Re-start the process
	var connectErr error
	if owned {
		stopCancellation, connectErr = c.connectOwned(ctx)
	} else {
		connectErr = c.Connect(ctx)
	}
	if err := connectErr; err != nil {
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

	c.logger.Info("reconnected process")
	return nil
}

// PID returns the operating-system process id of the running child. Returns 0
// when the process has not been started (or has exited and been cleared).
func (c *ProcessClient) PID() int {
	c.procMu.Lock()
	defer c.procMu.Unlock()
	if c.exited || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Ping checks if the process is alive by verifying it's still running
// and sending a JSON-RPC ping.
func (c *ProcessClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeoutOrDefault(c.pingTimeout))
	defer cancel()

	// Check process is still running
	c.procMu.Lock()
	started := c.started
	cmd := c.cmd
	c.procMu.Unlock()

	if !started || cmd == nil || cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	// Signal(0) checks if process exists without sending a signal
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process exited: %w", err)
	}

	// Send a liveness request and wait for a response. The stateless
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

// Close terminates the process gracefully.
// Sends SIGTERM, waits up to 5 seconds, then sends SIGKILL if still running.
func (c *ProcessClient) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.procMu.Lock()
	cmd, stdin, done, readDone := c.cmd, c.stdin, c.done, c.readDone
	if c.ownerCancel != nil {
		defer c.ownerCancel()
	}
	c.started = false
	if c.cancel != nil {
		c.cancel()
	}
	c.procMu.Unlock()
	if cmd == nil || cmd.Process == nil || done == nil {
		return nil
	}
	var cleanupErrors []error
	if stdin != nil {
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("closing process input: %w", err))
		}
	}
	select {
	case <-done:
	default:
		if err := signalProcess(cmd, !c.remote && c.execution != nil, false); err != nil && !errors.Is(err, os.ErrProcessDone) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("terminating process: %w", err))
		}
	}
	timer := time.NewTimer(processKillGracePeriod)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		if err := signalProcess(cmd, !c.remote && c.execution != nil, true); err != nil && !errors.Is(err, os.ErrProcessDone) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("killing process: %w", err))
		}
		timer.Reset(processKillGracePeriod)
		select {
		case <-done:
		case <-timer.C:
			return errors.Join(append(cleanupErrors, fmt.Errorf("process reap deadline exceeded"))...)
		}
	}
	// Old response readers must drain before Reconnect installs new requests.
	if readDone != nil {
		select {
		case <-readDone:
		case <-time.After(processKillGracePeriod + time.Second):
			return errors.Join(append(cleanupErrors, fmt.Errorf("process output cleanup deadline exceeded"))...)
		}
	}
	return errors.Join(cleanupErrors...)
}
