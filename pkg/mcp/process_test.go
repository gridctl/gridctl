package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
)

// newTestProcessClient creates a ProcessClient for testing with the given name and logger.
// The transport field is not set since tests calling this construct the client
// to test low-level methods (readResponses, readStderr, call) directly.
func newTestProcessClient(name string, logger *slog.Logger) *ProcessClient {
	c := &ProcessClient{
		responses: make(map[int64]chan *Response),
	}
	c.RPCClient.name = name
	c.RPCClient.logger = logger
	return c
}

func TestProcessClient_ReadStderr(t *testing.T) {
	// Test that readStderr reads lines and logs them at WARN level
	buffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(buffer, nil)
	logger := slog.New(handler).With("server", "test-process")

	client := newTestProcessClient("test-process", logger)

	// Simulate stderr output
	stderrContent := "error: something failed\nwarning: disk space low\n"
	reader := strings.NewReader(stderrContent)

	// Run readStderr (it will read until EOF)
	done := make(chan struct{})
	go func() {
		client.readStderr(context.Background(), reader)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not complete in time")
	}

	entries := buffer.GetRecent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}

	// Verify first entry
	if entries[0].Level != "WARN" {
		t.Errorf("expected WARN level, got %s", entries[0].Level)
	}
	if entries[0].Message != "server stderr" {
		t.Errorf("expected message 'server stderr', got %s", entries[0].Message)
	}
	if entries[0].Attrs["output"] != "error: something failed" {
		t.Errorf("expected stderr output in attrs, got %v", entries[0].Attrs["output"])
	}

	// Verify second entry
	if entries[1].Attrs["output"] != "warning: disk space low" {
		t.Errorf("expected stderr output in attrs, got %v", entries[1].Attrs["output"])
	}
}

func TestProcessClient_ReadStderr_Empty(t *testing.T) {
	buffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(buffer, nil)
	logger := slog.New(handler)

	client := newTestProcessClient("test-process", logger)

	// Empty reader should produce no log entries
	reader := bytes.NewReader(nil)
	done := make(chan struct{})
	go func() {
		client.readStderr(context.Background(), reader)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not complete in time")
	}

	entries := buffer.GetRecent(10)
	if len(entries) != 0 {
		t.Errorf("expected 0 log entries for empty stderr, got %d", len(entries))
	}
}

func TestProcessClient_ReadResponses(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())

	// Create a response channel for request ID 1
	respCh := make(chan *Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	// Build a valid JSON-RPC response
	result, _ := json.Marshal(map[string]string{"status": "ok"})
	idBytes := json.RawMessage(`1`)
	resp := Response{
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

func TestProcessClient_ReadResponses_NonJSON(t *testing.T) {
	logBuffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(logBuffer, nil)
	logger := slog.New(handler)

	client := newTestProcessClient("test-process", logger)

	// Simulate non-JSON output (e.g., debug prints from the server)
	output := "DEBUG: starting up\nsome random text\n"
	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	_, _ = pw.Write([]byte(output))
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readResponses did not complete in time")
	}
	cancel()

	// Non-JSON lines should be logged at INFO level
	entries := logBuffer.GetRecent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries for non-JSON lines, got %d", len(entries))
	}
	if entries[0].Level != "INFO" {
		t.Errorf("expected INFO level for non-JSON output, got %s", entries[0].Level)
	}
	if entries[0].Message != "server output" {
		t.Errorf("expected message 'server output', got %s", entries[0].Message)
	}
}

func TestProcessClient_ReadResponses_EmptyLines(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())

	// Empty lines should be skipped
	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	// Write empty lines followed by close
	_, _ = pw.Write([]byte("\n\n\n"))
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readResponses did not complete in time")
	}
	cancel()
}

func TestProcessClient_ReadResponses_ErrorResponse(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())

	respCh := make(chan *Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	// Build an error JSON-RPC response
	idBytes := json.RawMessage(`1`)
	resp := Response{
		JSONRPC: "2.0",
		ID:      &idBytes,
		Error:   &Error{Code: -32600, Message: "invalid request"},
	}
	line, _ := json.Marshal(resp)

	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	_, _ = pw.Write(append(line, '\n'))
	pw.Close()

	select {
	case got := <-respCh:
		if got.Error == nil {
			t.Fatal("expected error response")
		}
		if got.Error.Code != -32600 {
			t.Errorf("expected error code -32600, got %d", got.Error.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error response")
	}

	cancel()
	<-done
}

func TestProcessClient_ReadResponses_UnmatchedID(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())

	// Register a channel for ID 1, but send response for ID 99
	respCh := make(chan *Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	idBytes := json.RawMessage(`99`)
	resp := Response{
		JSONRPC: "2.0",
		ID:      &idBytes,
		Result:  json.RawMessage(`{}`),
	}
	line, _ := json.Marshal(resp)

	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	_, _ = pw.Write(append(line, '\n'))
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readResponses did not complete in time")
	}
	cancel()

	// The channel for ID 1 should still be waiting (response was for ID 99)
	select {
	case <-respCh:
		t.Error("did not expect response on channel for ID 1")
	default:
		// Good - no response received
	}

	// Channel should still be registered
	client.responsesMu.Lock()
	_, exists := client.responses[1]
	client.responsesMu.Unlock()
	if !exists {
		t.Error("expected channel for ID 1 to still be registered")
	}
}

func TestProcessClient_Connect_EmptyCommand(t *testing.T) {
	client := NewProcessClient("test", nil, "", nil)

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "no command specified") {
		t.Errorf("expected 'no command specified' error, got: %v", err)
	}
}

func TestProcessClient_Connect_Idempotent(t *testing.T) {
	// Use "cat" as a simple command that reads stdin
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("first Connect failed: %v", err)
	}
	defer client.Close()

	// Second Connect should be a no-op
	err = client.Connect(context.Background())
	if err != nil {
		t.Fatalf("second Connect should succeed (idempotent), got: %v", err)
	}
}

func TestProcessClient_SendStdio_NotConnected(t *testing.T) {
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	req := Request{
		JSONRPC: "2.0",
		Method:  "ping",
	}

	err := client.sendStdio(req)
	if err == nil {
		t.Fatal("expected error when sending to unconnected client")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestProcessClient_Name(t *testing.T) {
	client := NewProcessClient("my-server", []string{"cat"}, "", nil)
	if client.Name() != "my-server" {
		t.Errorf("expected name 'my-server', got '%s'", client.Name())
	}
}

func TestProcessClient_SetLogger(t *testing.T) {
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	// Setting nil logger should not panic
	client.SetLogger(nil)

	// Setting non-nil logger should work
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client.SetLogger(logger)
}

func TestProcessClient_Connect_InvalidCommand(t *testing.T) {
	client := NewProcessClient("test", []string{"/nonexistent/binary"}, "", nil)

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
	if !strings.Contains(err.Error(), "starting process") {
		t.Errorf("expected 'starting process' error, got: %v", err)
	}
}

func TestProcessClient_Close_NotStarted(t *testing.T) {
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	// Close without starting should not panic
	err := client.Close()
	if err != nil {
		t.Errorf("expected no error closing unstarted client, got: %v", err)
	}
}

func TestProcessClient_StartAndClose(t *testing.T) {
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestProcessClient_CallTimeout(t *testing.T) {
	// Create pipes that simulate a process that accepts input but never responds
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	client := newTestProcessClient("test", logging.NewDiscardLogger())
	client.command = []string{"cat"}
	client.started = true
	client.stdin = stdinW
	client.stdout = stdoutR

	// Drain stdin so send() doesn't block
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := stdinR.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// Start reader goroutine
	readerCtx, readerCancel := context.WithCancel(context.Background())
	client.cancel = readerCancel
	go client.readResponses(readerCtx)

	defer func() {
		readerCancel()
		stdinR.Close()
		stdinW.Close()
		stdoutW.Close()
	}()

	// Use a short context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var result ToolCallResult
	err := client.call(ctx, "tools/list", nil, &result)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline error, got: %v", err)
	}
}

func TestProcessClient_NewProcessClient_EnvMerge(t *testing.T) {
	client := NewProcessClient("test", []string{"cat"}, "/tmp", map[string]string{
		"CUSTOM_VAR": "value1",
		"ANOTHER":    "value2",
	})

	// The env should contain the custom vars appended to os.Environ()
	foundCustom := false
	foundAnother := false
	for _, env := range client.env {
		if env == "CUSTOM_VAR=value1" {
			foundCustom = true
		}
		if env == "ANOTHER=value2" {
			foundAnother = true
		}
	}
	if !foundCustom {
		t.Error("expected CUSTOM_VAR=value1 in environment")
	}
	if !foundAnother {
		t.Error("expected ANOTHER=value2 in environment")
	}
}

func TestProcessClient_ReadStderr_ContextCancel(t *testing.T) {
	buffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(buffer, nil)
	logger := slog.New(handler)

	client := newTestProcessClient("test-process", logger)

	// Use a pipe that blocks on read until context cancellation
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.readStderr(ctx, pr)
		close(done)
	}()

	// Write one line then cancel
	_, _ = fmt.Fprintln(pw, "first line")
	time.Sleep(10 * time.Millisecond)
	cancel()
	pr.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not exit on context cancel")
	}
}

func TestProcessClient_FullLifecycle(t *testing.T) {
	// Use "cat" as a simple MCP server simulator
	// cat echoes stdin to stdout, so we can write a response and read it back
	client := NewProcessClient("test", []string{"cat"}, "", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Send a JSON-RPC request; cat will echo it back as if it were a response
	// This tests the full write -> read -> route path
	idBytes, _ := json.Marshal(int64(1))
	rawID := json.RawMessage(idBytes)
	resultBytes, _ := json.Marshal(map[string]string{"status": "ok"})

	// Manually construct what we want "cat" to echo back
	fakeResp := Response{
		JSONRPC: "2.0",
		ID:      &rawID,
		Result:  resultBytes,
	}
	respLine, _ := json.Marshal(fakeResp)

	// Create response channel
	respCh := make(chan *Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	// Write directly to stdin (bypassing send() which would add a request)
	client.procMu.Lock()
	_, err = client.stdin.Write(append(respLine, '\n'))
	client.procMu.Unlock()
	if err != nil {
		t.Fatalf("write to stdin failed: %v", err)
	}

	// Wait for response
	select {
	case got := <-respCh:
		if got.Error != nil {
			t.Errorf("unexpected error in response: %v", got.Error)
		}
		var result map[string]string
		if err := json.Unmarshal(got.Result, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("expected status 'ok', got '%s'", result["status"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	err = client.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestProcessClient_ReadResponses_MultipleResponses(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())

	// Create channels for IDs 1, 2, 3
	channels := make(map[int64]chan *Response)
	for i := int64(1); i <= 3; i++ {
		ch := make(chan *Response, 1)
		channels[i] = ch
		client.responsesMu.Lock()
		client.responses[i] = ch
		client.responsesMu.Unlock()
	}

	pr, pw := io.Pipe()
	client.stdout = pr

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		client.readResponses(ctx)
		close(done)
	}()

	// Write 3 responses in sequence
	var buf bytes.Buffer
	for i := int64(1); i <= 3; i++ {
		idBytes := json.RawMessage(fmt.Sprintf("%d", i))
		resp := Response{
			JSONRPC: "2.0",
			ID:      &idBytes,
			Result:  json.RawMessage(fmt.Sprintf(`{"id":%d}`, i)),
		}
		line, _ := json.Marshal(resp)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	_, _ = pw.Write(buf.Bytes())
	pw.Close()

	// All three responses should arrive
	for i := int64(1); i <= 3; i++ {
		select {
		case got := <-channels[i]:
			if got.Error != nil {
				t.Errorf("response %d: unexpected error: %v", i, got.Error)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for response %d", i)
		}
	}

	cancel()
	<-done

	// All channels should be cleaned up from the map
	client.responsesMu.Lock()
	remaining := len(client.responses)
	client.responsesMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 remaining response channels, got %d", remaining)
	}
}
