package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
)

// TestReload_ToolDiscoveryNotifiesWithoutContainerWork is the focused
// case for a discovery-only hot reload: the handler does not register or
// restart servers, and the callback the builder installs notifies the
// stack-default handshake session and listen subscriber, not a session
// or subscriber pinned to discovery=full.
func TestReload_ToolDiscoveryNotifiesWithoutContainerWork(t *testing.T) {
	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(20*time.Millisecond, time.Second)

	ts := httptest.NewServer(mcp.NewStreamableHTTPServer(gw, nil))
	t.Cleanup(ts.Close)

	defStream := openReloadHandshake(t, ts.URL+"/mcp")
	fullStream := openReloadHandshake(t, ts.URL+"/mcp?discovery=full")
	defListen := openReloadListen(t, ts.URL+"/mcp", 21)
	fullListen := openReloadListen(t, ts.URL+"/mcp?discovery=full", 22)
	waitReloadText(t, defListen, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
	waitReloadText(t, fullListen, "notifications/subscriptions/acknowledged", 1, 3*time.Second)

	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	base := `name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
`
	if err := os.WriteFile(stackPath, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	initial, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	h := reload.NewHandler(stackPath, initial, gw, nil, 8180, 9000, nil, nil)
	registered := false
	h.SetRegisterServerFunc(func(context.Context, config.MCPServer, []reload.ReplicaRuntime, string) error {
		registered = true
		t.Error("registerServer called for a discovery-only reload")
		return nil
	})
	h.SetOnConfigApplied(func(cfg *config.Stack) {
		applyReloadedToolDiscovery(gw, cfg)
	})

	updated := base + `gateway:
  tool_discovery:
    mode: deferred
`
	if err := os.WriteFile(stackPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("reload: %s", result.Message)
	}
	if result.Message == "no changes detected" {
		t.Fatal("discovery-only edit was treated as empty")
	}
	if registered || len(result.Added) != 0 || len(result.Removed) != 0 || len(result.Modified) != 0 {
		t.Fatalf("container work: registered=%v added=%v removed=%v modified=%v", registered, result.Added, result.Removed, result.Modified)
	}
	if gw.ToolDiscoveryStatus() != mcp.DiscoveryDeferred {
		t.Fatalf("status = %q", gw.ToolDiscoveryStatus())
	}

	waitReloadText(t, defStream, "notifications/tools/list_changed", 1, 3*time.Second)
	waitReloadText(t, defListen, "notifications/tools/list_changed", 1, 3*time.Second)
	time.Sleep(150 * time.Millisecond)
	if got := strings.Count(fullStream.String(), "notifications/tools/list_changed"); got != 0 {
		t.Fatalf("full handshake notified: %s", fullStream.String())
	}
	if got := strings.Count(fullListen.String(), "notifications/tools/list_changed"); got != 0 {
		t.Fatalf("full subscriber notified: %s", fullListen.String())
	}
	if got := strings.Count(defStream.String(), "notifications/tools/list_changed"); got != 1 {
		t.Fatalf("deferred handshake events = %d: %s", got, defStream.String())
	}
	if got := strings.Count(defListen.String(), "notifications/tools/list_changed"); got != 1 {
		t.Fatalf("deferred subscriber events = %d: %s", got, defListen.String())
	}
	if !strings.Contains(defListen.String(), `"io.modelcontextprotocol/subscriptionId":21`) {
		t.Fatalf("deferred listen event missing id: %s", defListen.String())
	}
}

type reloadBuf struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *reloadBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *reloadBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func openReloadHandshake(t *testing.T, url string) *reloadBuf {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize %s: %d", url, resp.StatusCode)
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("missing session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.Split(url, "?")[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	get.Header.Set("Mcp-Session-Id", session)
	stream, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.Body.Close() })
	if stream.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(stream.Body)
		t.Fatalf("GET %d: %s", stream.StatusCode, raw)
	}
	buf := &reloadBuf{}
	go func() { _, _ = io.Copy(buf, stream.Body) }()
	time.Sleep(30 * time.Millisecond)
	return buf
}

func openReloadListen(t *testing.T, url string, id int) *reloadBuf {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"tester","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("listen %d: %s", resp.StatusCode, raw)
	}
	buf := &reloadBuf{}
	go func() { _, _ = io.Copy(buf, resp.Body) }()
	return buf
}

func waitReloadText(t *testing.T, buf *reloadBuf, needle string, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if strings.Count(buf.String(), needle) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d %q, want %d: %s", strings.Count(buf.String(), needle), needle, n, buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

