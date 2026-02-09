package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
)

// newTestStdioClient creates a StdioClient for testing with the given name and logger.
func newTestStdioClient(name string, logger *slog.Logger) *StdioClient {
	c := &StdioClient{
		responses: make(map[int64]chan *jsonrpc.Response),
	}
	c.RPCClient.name = name
	c.RPCClient.logger = logger
	return c
}

func TestStdioClient_DrainPendingRequests(t *testing.T) {
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	// Register pending response channels
	ch1 := make(chan *jsonrpc.Response, 1)
	ch2 := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = ch1
	client.responses[2] = ch2
	client.responsesMu.Unlock()

	// Simulate readResponses exiting by piping then closing stdout
	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	// Close the pipe to simulate EOF (container crash)
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readResponses did not exit on EOF")
	}

	// Both channels should receive error responses
	for id, ch := range map[int64]chan *jsonrpc.Response{1: ch1, 2: ch2} {
		select {
		case resp := <-ch:
			if resp.Error == nil {
				t.Errorf("channel %d: expected error response", id)
			} else if resp.Error.Message != "connection lost" {
				t.Errorf("channel %d: expected 'connection lost', got '%s'", id, resp.Error.Message)
			}
		case <-time.After(time.Second):
			t.Errorf("channel %d: timed out waiting for drain", id)
		}
	}

	// Response map should be empty
	client.responsesMu.Lock()
	remaining := len(client.responses)
	client.responsesMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 remaining response channels after drain, got %d", remaining)
	}
}

func TestStdioClient_DrainPendingRequests_Empty(t *testing.T) {
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	// Drain with no pending requests should not panic
	client.drainPendingRequests()

	client.responsesMu.Lock()
	remaining := len(client.responses)
	client.responsesMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 remaining response channels, got %d", remaining)
	}
}

func TestStdioClient_ReadResponses(t *testing.T) {
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	// Create a response channel for request ID 1
	respCh := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	// Build a valid JSON-RPC response
	result, _ := json.Marshal(map[string]string{"status": "ok"})
	idBytes := json.RawMessage(`1`)
	resp := jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      &idBytes,
		Result:  result,
	}
	line, _ := json.Marshal(resp)

	// Set up a pipe for stdout
	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	// Write response line
	_, err := pw.Write(append(line, '\n'))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	pw.Close()

	// Wait for response to be routed
	select {
	case got := <-respCh:
		if got.Error != nil {
			t.Errorf("expected no error, got: %v", got.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	cancel()
	<-done
}

func TestStdioClient_SendStdio_NotConnected(t *testing.T) {
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	req := jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "ping",
	}

	err := client.sendStdio(req)
	if err == nil {
		t.Fatal("expected error when sending to unconnected client")
	}
	if err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestStdioClient_CallFailsFastOnConnectionDrop(t *testing.T) {
	// Verify that call() returns a "connection lost" error quickly when the
	// underlying reader closes (simulating a container crash), rather than
	// waiting for the full 30s DefaultRequestTimeout.
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())
	client.attached = true
	client.stdin = stdinW
	client.stdout = stdoutR

	// Drain stdin so sendStdio doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := stdinR.Read(buf); err != nil {
				return
			}
		}
	}()

	// Start reader goroutine
	readerCtx, readerCancel := context.WithCancel(context.Background())
	go client.readResponses(readerCtx)

	defer func() {
		readerCancel()
		stdinR.Close()
		stdinW.Close()
	}()

	// Launch call() in background — it will register a pending request and block
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.call(context.Background(), "tools/list", nil, nil)
	}()

	// Give call() a moment to register the pending request
	time.Sleep(20 * time.Millisecond)

	// Close stdout to simulate container crash (EOF to readResponses)
	stdoutW.Close()

	// call() should fail fast with "connection lost" — well under the 30s timeout
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error from call()")
		}
		if !strings.Contains(err.Error(), "connection lost") {
			t.Errorf("expected 'connection lost' error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call() did not fail fast on connection drop (waited >2s)")
	}
}

func TestStdioClient_DrainOnContextCancel(t *testing.T) {
	// Verify that drainPendingRequests runs even when readResponses exits
	// via context cancellation (not just scanner EOF).
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	ch := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = ch
	client.responsesMu.Unlock()

	// Use a pipe that won't EOF — only context cancel will cause exit
	pr, pw := io.Pipe()
	defer pw.Close()
	client.stdout = pr

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	// Cancel context and close the pipe to unblock Scan()
	cancel()
	pr.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readResponses did not exit on context cancel")
	}

	// Pending request should have been drained via defer
	select {
	case resp := <-ch:
		if resp.Error == nil {
			t.Error("expected error response from drain")
		} else if resp.Error.Message != "connection lost" {
			t.Errorf("expected 'connection lost', got '%s'", resp.Error.Message)
		}
	default:
		t.Error("expected channel to receive drain error after context cancel")
	}
}

func TestStdioClient_Close_NotAttached(t *testing.T) {
	client := newTestStdioClient("test-stdio", logging.NewDiscardLogger())

	// Close without attaching should not panic
	err := client.Close()
	if err != nil {
		t.Errorf("expected no error closing unattached client, got: %v", err)
	}
}

func TestStdioClient_Name(t *testing.T) {
	client := newTestStdioClient("my-container", logging.NewDiscardLogger())
	if client.Name() != "my-container" {
		t.Errorf("expected name 'my-container', got '%s'", client.Name())
	}
}
