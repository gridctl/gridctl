package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

type captureResponseWriter struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
	code   int
}

func (w *captureResponseWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *captureResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	w.code = code
	w.mu.Unlock()
}

func (w *captureResponseWriter) Flush() {}

func (w *captureResponseWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func listChangedCount(body string) int {
	return strings.Count(body, string(toolsListChangedPayload))
}

func TestStreamableHTTPServer_Initialize_AdvertisesToolsListChanged(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{"release": {Servers: []string{"alpha"}}}))
	srv := NewStreamableHTTPServer(g, nil)

	for _, group := range []string{"", "release"} {
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo":      map[string]any{"name": "c", "version": "1"},
			},
		})
		req := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		if group != "" {
			req = req.WithContext(WithGroup(req.Context(), group))
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("group %q: status %d: %s", group, w.Code, w.Body.String())
		}
		var resp jsonrpc.Response
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		var result InitializeResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatal(err)
		}
		if result.Capabilities.Tools == nil || !result.Capabilities.Tools.ListChanged {
			t.Fatalf("group %q: tools.listChanged not advertised: %#v", group, result.Capabilities.Tools)
		}
		if result.Capabilities.Prompts != nil && result.Capabilities.Prompts.ListChanged {
			t.Fatal("prompts listChanged advertised")
		}
		if result.Capabilities.Resources != nil && result.Capabilities.Resources.ListChanged {
			t.Fatal("resources listChanged advertised")
		}
	}
}

func TestStreamableHTTPServer_ListChangedDeliveryAndReplay(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetListChangeTiming(20*time.Millisecond, time.Second)
	srv := NewStreamableHTTPServer(g, nil)
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().RefreshTools()
	sessionID := initializeStreamable(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := loopbackRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := &captureResponseWriter{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()
	time.Sleep(15 * time.Millisecond)

	g.Router().AddClient(&listChangedStub{name: "beta", tools: []Tool{testTool("ping", "p")}})
	g.Router().RefreshTools()
	deadline := time.Now().Add(time.Second)
	for listChangedCount(w.String()) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	body := w.String()
	if listChangedCount(body) != 1 || !strings.Contains(body, "event: message") {
		t.Fatalf("live stream = %q", body)
	}
	g.Router().RefreshTools()
	time.Sleep(60 * time.Millisecond)
	if listChangedCount(w.String()) != 1 {
		t.Fatalf("no-op refresh emitted another event: %s", w.String())
	}
	g.Router().RemoveClient("beta")
	deadline = time.Now().Add(time.Second)
	for listChangedCount(w.String()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if listChangedCount(w.String()) != 2 {
		t.Fatalf("unregister stream = %q", w.String())
	}
	cancel()
	<-done

	g.Router().AddClient(&listChangedStub{name: "gamma", tools: []Tool{testTool("later", "x")}})
	g.Router().RefreshTools()
	time.Sleep(60 * time.Millisecond)
	replayCtx, replayCancel := context.WithCancel(context.Background())
	replayReq := loopbackRequest(http.MethodGet, "/mcp", nil).WithContext(replayCtx)
	replayReq.Header.Set("Mcp-Session-Id", sessionID)
	replayReq.Header.Set("Last-Event-ID", "0")
	replay := httptest.NewRecorder()
	replayDone := make(chan struct{})
	go func() {
		defer close(replayDone)
		srv.ServeHTTP(replay, replayReq)
	}()
	time.Sleep(30 * time.Millisecond)
	replayCancel()
	<-replayDone
	if listChangedCount(replay.Body.String()) == 0 {
		t.Fatalf("replay missed the event: %s", replay.Body.String())
	}
}

func TestStreamableHTTPServer_NotifyToolsListChanged_Replay(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	srv := NewStreamableHTTPServer(g, nil)
	sessionID := initializeStreamable(t, srv)
	srv.NotifyToolsListChanged(sessionID)
	srv.NotifyToolsListChanged("missing")

	ctx, cancel := context.WithCancel(context.Background())
	req := loopbackRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Last-Event-ID", "0")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	body := w.Body.String()
	if listChangedCount(body) != 1 || !strings.Contains(body, "event: message\ndata: "+string(toolsListChangedPayload)) {
		t.Fatalf("replay = %q", body)
	}
}
