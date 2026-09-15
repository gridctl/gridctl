package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
	"go.uber.org/mock/gomock"
)

func jsonRPCResponseLine(t *testing.T, id int64, result any) []byte {
	t.Helper()
	idBytes, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	rawID := json.RawMessage(idBytes)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resp := jsonrpc.Response{JSONRPC: "2.0", ID: &rawID, Result: payload}
	line, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

func paddedJSONRPC(t *testing.T, method string, id int, size int) []byte {
	t.Helper()
	idJSON, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	methodJSON, err := json.Marshal(method)
	if err != nil {
		t.Fatal(err)
	}
	prefix := `{"jsonrpc":"2.0","id":`
	mid := `,"method":`
	paramsHead := `,"params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"c","version":"1"},"pad":"`
	suffix := `"}}`
	fixed := len(prefix) + len(idJSON) + len(mid) + len(methodJSON) + len(paramsHead) + len(suffix)
	if size < fixed {
		t.Fatalf("requested size %d smaller than fixed envelope %d", size, fixed)
	}
	body := make([]byte, 0, size)
	body = append(body, prefix...)
	body = append(body, idJSON...)
	body = append(body, mid...)
	body = append(body, methodJSON...)
	body = append(body, paramsHead...)
	body = append(body, bytes.Repeat([]byte{'a'}, size-fixed)...)
	body = append(body, suffix...)
	if len(body) != size {
		t.Fatalf("padded body length %d, want %d", len(body), size)
	}
	return body
}

func paddedResultLine(t *testing.T, id int64, tokenLen int) []byte {
	t.Helper()
	idJSON, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	prefix := `{"jsonrpc":"2.0","id":`
	mid := `,"result":{"pad":"`
	suffix := `"}}`
	fixed := len(prefix) + len(idJSON) + len(mid) + len(suffix)
	if tokenLen < fixed {
		t.Fatalf("requested token %d smaller than envelope %d", tokenLen, fixed)
	}
	line := make([]byte, 0, tokenLen+1)
	line = append(line, prefix...)
	line = append(line, idJSON...)
	line = append(line, mid...)
	line = append(line, bytes.Repeat([]byte{'b'}, tokenLen-fixed)...)
	line = append(line, suffix...)
	if len(line) != tokenLen {
		t.Fatalf("padded result length %d, want %d", len(line), tokenLen)
	}
	return append(line, '\n')
}

func TestProcessClient_ReadResponses_Correlation(t *testing.T) {
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())
	respCh := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	client.stdout = pr

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		client.readResponses(ctx, client.stdout)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = pw.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("readResponses did not exit")
		}
	})

	malformed := []byte("{not-json\n")
	mismatch := jsonRPCResponseLine(t, 99, map[string]string{"token": "mismatch-sentinel"})
	matched := jsonRPCResponseLine(t, 1, map[string]string{"token": "intended-sentinel"})
	if _, err := pw.Write(malformed); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	if _, err := pw.Write(mismatch); err != nil {
		t.Fatalf("write mismatch: %v", err)
	}
	if _, err := pw.Write(matched); err != nil {
		t.Fatalf("write matched: %v", err)
	}

	watchdog, stopWatchdog := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopWatchdog()
	select {
	case got := <-respCh:
		if got.Error != nil {
			t.Fatalf("matched id completed with error: %v", got.Error)
		}
		var payload map[string]string
		if err := json.Unmarshal(got.Result, &payload); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if payload["token"] != "intended-sentinel" {
			t.Fatalf("pending request completed with %q, want intended-sentinel", payload["token"])
		}
	case <-watchdog.Done():
		t.Fatal("timed out waiting for matched response")
	}

	client.responsesMu.Lock()
	_, stillPending := client.responses[1]
	client.responsesMu.Unlock()
	if stillPending {
		t.Fatal("matched request remained registered after completion")
	}
}

func startProcessReader(t *testing.T) (*ProcessClient, chan *jsonrpc.Response, *io.PipeWriter) {
	t.Helper()
	client := newTestProcessClient("test-process", logging.NewDiscardLogger())
	respCh := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = respCh
	client.responsesMu.Unlock()
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.readResponses(ctx, pr)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = pw.Close()
		_ = pr.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("readResponses did not exit")
		}
	})
	return client, respCh, pw
}

func writePipeAsync(t *testing.T, pw *io.PipeWriter, payload []byte) {
	t.Helper()
	go func() {
		_, _ = pw.Write(payload)
		_ = pw.Close()
	}()
}

func TestProcessClient_ReadResponses_ScannerLimit(t *testing.T) {
	const maxToken = 1024 * 1024

	t.Run("below", func(t *testing.T) {
		_, respCh, pw := startProcessReader(t)
		writePipeAsync(t, pw, jsonRPCResponseLine(t, 1, map[string]string{"token": "small"}))
		select {
		case got := <-respCh:
			if got.Error != nil {
				t.Fatalf("below-limit line failed: %v", got.Error)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("below-limit line was not routed")
		}
	})

	t.Run("at", func(t *testing.T) {
		_, respCh, pw := startProcessReader(t)
		// Leave room for the newline inside the scanner max token.
		writePipeAsync(t, pw, paddedResultLine(t, 1, maxToken-2))
		select {
		case got := <-respCh:
			if got.Error != nil {
				t.Fatalf("at-limit line failed: %v", got.Error)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("at-limit line was not routed")
		}
	})

	t.Run("above", func(t *testing.T) {
		_, respCh, pw := startProcessReader(t)
		writePipeAsync(t, pw, append(bytes.Repeat([]byte{'x'}, maxToken+16), '\n'))
		select {
		case got := <-respCh:
			if got.Error == nil || got.Error.Message != "connection lost" {
				t.Fatalf("over-limit line completed pending request: %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("over-limit scanner failure did not drain pending request")
		}
	})
}

func TestProcessClient_CallCancel_SameClientRecovers(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})

	client := newTestProcessClient("test", logging.NewDiscardLogger())
	client.started = true
	client.stdin = stdinW
	client.stdout = stdoutR

	received := make(chan struct{}, 2)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		buf := make([]byte, 64*1024)
		for {
			n, err := stdinR.Read(buf)
			if n > 0 {
				select {
				case received <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()

	readerCtx, readerCancel := context.WithCancel(context.Background())
	client.cancel = readerCancel
	readerDone := make(chan struct{})
	go func() {
		client.readResponses(readerCtx, client.stdout)
		close(readerDone)
	}()
	t.Cleanup(func() {
		readerCancel()
		_ = stdoutW.Close()
		_ = stdinW.Close()
		_ = stdinR.Close()
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
			t.Error("readResponses did not exit")
		}
		select {
		case <-drainDone:
		case <-time.After(2 * time.Second):
			t.Error("stdin drain did not exit")
		}
	})

	callCtx, cancelCall := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.call(callCtx, "tools/list", nil, nil)
	}()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("call did not write a request before cancel")
	}
	cancelCall()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled call did not return")
	}

	client.responsesMu.Lock()
	_, pending := client.responses[1]
	client.responsesMu.Unlock()
	if pending {
		t.Fatal("cancelled request remained in the pending map")
	}

	recoverCtx, recoverCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer recoverCancel()
	recoverErr := make(chan error, 1)
	var recovered map[string]any
	go func() {
		recoverErr <- client.call(recoverCtx, "tools/list", nil, &recovered)
	}()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery call did not write a request")
	}
	if _, err := stdoutW.Write(jsonRPCResponseLine(t, 2, map[string]any{"ok": true})); err != nil {
		t.Fatalf("write recovery response: %v", err)
	}
	select {
	case err := <-recoverErr:
		if err != nil {
			t.Fatalf("same-client recovery call failed: %v", err)
		}
		if recovered["ok"] != true {
			t.Fatalf("recovery result = %#v", recovered)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("same-client recovery call did not complete")
	}
}

func TestStreamableHTTPServer_Post_RequestBodyLimit(t *testing.T) {
	below := paddedJSONRPC(t, "initialize", 1, 256)
	at := paddedJSONRPC(t, "initialize", 1, MaxRequestBodySize)
	above := paddedJSONRPC(t, "initialize", 1, MaxRequestBodySize+64)

	post := func(t *testing.T, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		srv := NewStreamableHTTPServer(NewGateway(), nil)
		req := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	belowW := post(t, below)
	if belowW.Code != http.StatusOK {
		t.Fatalf("below-limit initialize status %d, body %s", belowW.Code, belowW.Body.String())
	}
	if belowW.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("below-limit initialize did not create a session")
	}
	var belowResp jsonrpc.Response
	if err := json.NewDecoder(belowW.Body).Decode(&belowResp); err != nil {
		t.Fatal(err)
	}
	if belowResp.Error != nil {
		t.Fatalf("below-limit initialize error: %s", belowResp.Error.Message)
	}

	atW := post(t, at)
	if atW.Code != http.StatusOK {
		t.Fatalf("at-limit initialize status %d, body %s", atW.Code, atW.Body.String())
	}
	if atW.Header().Get("Mcp-Session-Id") == "" {
		t.Fatal("at-limit initialize did not create a session")
	}

	aboveW := post(t, above)
	if aboveW.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal("over-limit body created a session")
	}
	var aboveResp jsonrpc.Response
	if err := json.NewDecoder(aboveW.Body).Decode(&aboveResp); err != nil {
		t.Fatalf("over-limit response was not JSON-RPC: %v", err)
	}
	if aboveResp.Error == nil || aboveResp.Error.Code != jsonrpc.ParseError {
		t.Fatalf("over-limit body error = %+v, want parse error", aboveResp.Error)
	}
}

func TestGateway_HandleToolsCall_Truncation_AtLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(100)
	text := strings.Repeat("a", 100)
	client := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "fetch", Description: "Fetch data"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(text)}}, nil,
	).Times(1)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "server1__fetch"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != text {
		t.Fatalf("at-limit result changed: %q", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_DispatchCounts(t *testing.T) {
	newPair := func(t *testing.T) (*Gateway, *MockAgentClient, *MockAgentClient) {
		t.Helper()
		ctrl := gomock.NewController(t)
		g := NewGateway()
		alpha := setupMockAgentClient(ctrl, "alpha", []Tool{{Name: "echo", Description: "Echo"}})
		beta := setupMockAgentClient(ctrl, "beta", []Tool{{Name: "echo", Description: "Echo"}})
		g.Router().AddClient(alpha)
		g.Router().AddClient(beta)
		g.Router().RefreshTools()
		return g, alpha, beta
	}

	t.Run("named_receiver", func(t *testing.T) {
		g, alpha, beta := newPair(t)
		alpha.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(
			&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
		).Times(1)
		beta.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "hi"}})
		if err != nil || result.IsError {
			t.Fatalf("named dispatch failed: err=%v result=%+v", err, result)
		}
	})

	t.Run("unknown_tool", func(t *testing.T) {
		g, alpha, beta := newPair(t)
		alpha.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		beta.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "gamma__echo"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatal("unknown tool dispatched")
		}
	})

	t.Run("invalid_format", func(t *testing.T) {
		g, alpha, beta := newPair(t)
		alpha.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		beta.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "notprefixed"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatal("invalid format dispatched")
		}
	})

	t.Run("denied_gate", func(t *testing.T) {
		g, alpha, beta := newPair(t)
		alpha.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		beta.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		g.SetCallGates([]CallGate{&stubGate{name: "deny-gate", decision: GateDeny("Policy denied: do not retry.")}})
		result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__echo"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || result.Content[0].Text != "Policy denied: do not retry." {
			t.Fatalf("denied call result = %+v", result)
		}
	})
}
