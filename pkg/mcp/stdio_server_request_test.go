package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
)

type responseLoopFixture struct {
	read       func(context.Context, io.Reader)
	send       func(context.Context, jsonrpc.Request) error
	addPending func(int64) chan *jsonrpc.Response
	hasPending func(int64) bool
}

type responseLoopFactory func(*slog.Logger, io.WriteCloser) responseLoopFixture

func newContainerResponseLoopFixture(logger *slog.Logger, stdin io.WriteCloser) responseLoopFixture {
	client := newTestStdioClient("test-stdio", logger)
	client.attached = true
	client.stdin = stdin
	return responseLoopFixture{
		read: client.readResponses,
		send: client.sendStdioContext,
		addPending: func(id int64) chan *jsonrpc.Response {
			ch := make(chan *jsonrpc.Response, 1)
			client.responsesMu.Lock()
			client.responses[id] = ch
			client.responsesMu.Unlock()
			return ch
		},
		hasPending: func(id int64) bool {
			client.responsesMu.Lock()
			defer client.responsesMu.Unlock()
			_, ok := client.responses[id]
			return ok
		},
	}
}

func newProcessResponseLoopFixture(logger *slog.Logger, stdin io.WriteCloser) responseLoopFixture {
	client := newTestProcessClient("test-process", logger)
	client.started = true
	client.stdin = stdin
	return responseLoopFixture{
		read: client.readResponses,
		send: client.sendStdioContext,
		addPending: func(id int64) chan *jsonrpc.Response {
			ch := make(chan *jsonrpc.Response, 1)
			client.responsesMu.Lock()
			client.responses[id] = ch
			client.responsesMu.Unlock()
			return ch
		},
		hasPending: func(id int64) bool {
			client.responsesMu.Lock()
			defer client.responsesMu.Unlock()
			_, ok := client.responses[id]
			return ok
		},
	}
}

func TestStdioClient_ReadResponses_ServerMessages(t *testing.T) {
	testReadResponsesServerMessages(t, newContainerResponseLoopFixture)
}

func TestProcessClient_ReadResponses_ServerMessages(t *testing.T) {
	testReadResponsesServerMessages(t, newProcessResponseLoopFixture)
}

func testReadResponsesServerMessages(t *testing.T, factory responseLoopFactory) {
	t.Helper()
	t.Run("ping preserves correlation", func(t *testing.T) {
		clientInput, serverInput := net.Pipe()
		t.Cleanup(func() { _ = clientInput.Close(); _ = serverInput.Close() })
		fixture := factory(logging.NewDiscardLogger(), clientInput)
		pending := fixture.addPending(1)

		done := make(chan struct{})
		go func() {
			fixture.read(t.Context(), strings.NewReader(
				`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"+
					`{"jsonrpc":"2.0","id":1,"result":{"token":"expected"}}`+"\n",
			))
			close(done)
		}()

		if err := serverInput.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(serverInput).ReadBytes('\n')
		if err != nil {
			t.Fatalf("read ping reply: %v", err)
		}
		var reply jsonrpc.Response
		if err := json.Unmarshal(bytes.TrimSuffix(line, []byte{'\n'}), &reply); err != nil {
			t.Fatalf("decode ping reply: %v", err)
		}
		if reply.ID == nil || string(*reply.ID) != `1` || string(reply.Result) != `{}` || reply.Error != nil {
			t.Fatalf("ping reply = %#v", reply)
		}

		select {
		case response := <-pending:
			if string(response.Result) != `{"token":"expected"}` {
				t.Fatalf("correlated result = %s", response.Result)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("real response did not complete pending call")
		}
		if fixture.hasPending(1) {
			t.Fatal("completed call remained pending")
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("response reader did not exit")
		}
	})

	t.Run("unsupported string request", func(t *testing.T) {
		clientInput, serverInput := net.Pipe()
		t.Cleanup(func() { _ = clientInput.Close(); _ = serverInput.Close() })
		fixture := factory(logging.NewDiscardLogger(), clientInput)
		done := make(chan struct{})
		go func() {
			fixture.read(t.Context(), strings.NewReader(
				`{"jsonrpc":"2.0","id":"server-1","method":"sampling/createMessage"}`+"\n",
			))
			close(done)
		}()

		if err := serverInput.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(serverInput).ReadBytes('\n')
		if err != nil {
			t.Fatalf("read unsupported-method reply: %v", err)
		}
		var reply jsonrpc.Response
		if err := json.Unmarshal(line, &reply); err != nil {
			t.Fatalf("decode unsupported-method reply: %v", err)
		}
		if reply.ID == nil || string(*reply.ID) != `"server-1"` {
			t.Fatalf("reply ID = %v", reply.ID)
		}
		if reply.Error == nil || reply.Error.Code != jsonrpc.MethodNotFound || reply.Error.Message != "Method not found" {
			t.Fatalf("reply error = %#v", reply.Error)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("response reader did not exit")
		}
	})

	t.Run("notification is silent", func(t *testing.T) {
		clientInput, serverInput := net.Pipe()
		t.Cleanup(func() { _ = clientInput.Close(); _ = serverInput.Close() })
		fixture := factory(logging.NewDiscardLogger(), clientInput)
		done := make(chan struct{})
		go func() {
			fixture.read(t.Context(), strings.NewReader(
				`{"jsonrpc":"2.0","method":"notifications/cancelled"}`+"\n",
			))
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("response reader did not exit")
		}

		if err := serverInput.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		var output [1]byte
		if _, err := serverInput.Read(output[:]); err == nil {
			t.Fatal("notification produced output")
		} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			t.Fatalf("notification output check failed: %v", err)
		}
	})
}

type failingStdioWriter struct{}

func (failingStdioWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func (failingStdioWriter) Close() error              { return nil }

func TestStdioClient_ReadResponses_ReplyFailure(t *testing.T) {
	testReadResponsesReplyFailure(t, newContainerResponseLoopFixture)
}

func TestProcessClient_ReadResponses_ReplyFailure(t *testing.T) {
	testReadResponsesReplyFailure(t, newProcessResponseLoopFixture)
}

func testReadResponsesReplyFailure(t *testing.T, factory responseLoopFactory) {
	t.Helper()
	logBuffer := logging.NewLogBuffer(10)
	logger := slog.New(logging.NewBufferHandler(logBuffer, nil))
	fixture := factory(logger, failingStdioWriter{})
	pending := fixture.addPending(7)
	done := make(chan struct{})
	go func() {
		fixture.read(t.Context(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reply failure did not stop response reader")
	}
	select {
	case response := <-pending:
		if response.Error == nil || response.Error.Message != "connection lost" {
			t.Fatalf("drained response = %#v", response)
		}
	default:
		t.Fatal("reply failure did not drain pending calls")
	}
	entries := logBuffer.GetRecent(10)
	if len(entries) != 1 || entries[0].Level != "WARN" || entries[0].Message != "server request reply failed" {
		t.Fatalf("reply failure log = %#v", entries)
	}
	if _, ok := entries[0].Attrs["error"]; !ok {
		t.Fatal("reply failure log omitted error")
	}
}

type blockingFrameWriter struct {
	mu           sync.Mutex
	active       bool
	overlap      bool
	calls        int
	frames       [][]byte
	firstEntered chan struct{}
	releaseFirst chan struct{}
}

func newBlockingFrameWriter() *blockingFrameWriter {
	return &blockingFrameWriter{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
}

func (w *blockingFrameWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	w.calls++
	call := w.calls
	if w.active {
		w.overlap = true
	}
	w.active = true
	w.mu.Unlock()

	if call == 1 {
		close(w.firstEntered)
		<-w.releaseFirst
	}

	w.mu.Lock()
	w.frames = append(w.frames, append([]byte(nil), data...))
	w.active = false
	w.mu.Unlock()
	return len(data), nil
}

func (w *blockingFrameWriter) Close() error { return nil }

func TestStdioClient_ServerReplySerializesWithRequest(t *testing.T) {
	testServerReplySerialization(t, newContainerResponseLoopFixture)
}

func TestProcessClient_ServerReplySerializesWithRequest(t *testing.T) {
	testServerReplySerialization(t, newProcessResponseLoopFixture)
}

func testServerReplySerialization(t *testing.T, factory responseLoopFactory) {
	t.Helper()
	writer := newBlockingFrameWriter()
	fixture := factory(logging.NewDiscardLogger(), writer)
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- fixture.send(t.Context(), jsonrpc.Request{JSONRPC: "2.0", Method: "tools/list"})
	}()
	select {
	case <-writer.firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("outbound request did not enter writer")
	}

	readerDone := make(chan struct{})
	go func() {
		fixture.read(t.Context(), strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping"}`+"\n"))
		close(readerDone)
	}()
	time.Sleep(50 * time.Millisecond)
	close(writer.releaseFirst)

	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatalf("send request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("outbound request did not finish")
	}
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server reply did not finish")
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.overlap {
		t.Fatal("outbound request and server reply writes overlapped")
	}
	if len(writer.frames) != 2 {
		t.Fatalf("wrote %d frames, want 2", len(writer.frames))
	}
	for i, frame := range writer.frames {
		if len(frame) == 0 || frame[len(frame)-1] != '\n' {
			t.Errorf("frame %d lacks trailing newline", i)
		}
		if !json.Valid(bytes.TrimSuffix(frame, []byte{'\n'})) {
			t.Errorf("frame %d is not valid JSON: %q", i, frame)
		}
	}
}
