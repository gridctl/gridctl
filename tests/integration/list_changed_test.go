//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

const listChangedPayload = `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`

type upstreamStream struct {
	cancel context.CancelFunc
	body   *lockedBuffer
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startListChangedUpstream(t *testing.T, gw *mcp.Gateway) (*httptest.Server, string, *upstreamStream) {
	t.Helper()
	ts := httptest.NewServer(mcp.NewStreamableHTTPServer(gw, nil))
	t.Cleanup(ts.Close)
	client := &http.Client{Transport: &http.Transport{}}
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	initReq, err := http.NewRequestWithContext(initCtx, http.MethodPost, ts.URL+"/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	initReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(initReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status %d", resp.StatusCode)
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("missing session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Session-Id", session)
	streamResp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	buf := &lockedBuffer{}
	go func() {
		_, _ = io.Copy(buf, streamResp.Body)
		streamResp.Body.Close()
	}()
	t.Cleanup(func() {
		cancel()
		streamResp.Body.Close()
	})
	time.Sleep(30 * time.Millisecond)
	return ts, session, &upstreamStream{cancel: cancel, body: buf}
}

func waitListChanged(t *testing.T, body *lockedBuffer, n int, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := body.String()
		if strings.Count(got, listChangedPayload) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d list_changed events, want %d: %s", strings.Count(got, listChangedPayload), n, got)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func assertListChangedCount(t *testing.T, body *lockedBuffer, want int, quiet time.Duration) {
	t.Helper()
	time.Sleep(quiet)
	got := body.String()
	if strings.Count(got, listChangedPayload) != want {
		t.Fatalf("got %d list_changed events, want %d: %s", strings.Count(got, listChangedPayload), want, got)
	}
}

func TestListChanged_ProcessRegisterQuietAndUnregister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(3*time.Second, 30*time.Second)
	_, _, stream := startListChangedUpstream(t, gw)

	cfg := mcp.MCPServerConfig{
		Name:         "echo",
		LocalProcess: true,
		Command:      []string{mockStdioBin, "-list-changed"},
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	waitListChanged(t, stream.body, 1, 6*time.Second)
	var declared bool
	for _, st := range gw.Status() {
		if st.Name == "echo" && st.Capabilities.ToolsListChanged {
			declared = true
		}
	}
	if !declared {
		t.Fatal("status missing toolsListChanged")
	}

	if err := gw.UnregisterMCPServerContext(ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	assertListChangedCount(t, stream.body, 1, 3500*time.Millisecond)

	if err := gw.UnregisterMCPServerContext(ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	body := waitListChanged(t, stream.body, 2, 6*time.Second)
	if strings.Count(body, listChangedPayload) != 2 || !strings.Contains(body, "event: message") {
		t.Fatalf("stream = %s", body)
	}
}

func TestListChanged_HTTPRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)
	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(40*time.Millisecond, time.Second)
	_, _, stream := startListChangedUpstream(t, gw)
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:         "echo",
		Transport:    mcp.TransportHTTP,
		Endpoint:     fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		ReadyTimeout: 5 * time.Second,
		External:     true,
	}); err != nil {
		t.Fatal(err)
	}
	waitListChanged(t, stream.body, 1, 3*time.Second)
	gw.Router().RefreshTools()
	assertListChangedCount(t, stream.body, 1, 120*time.Millisecond)
}

func TestListChanged_MutateProcessAndHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("process add", func(t *testing.T) {
		gw := mcp.NewGateway()
		t.Cleanup(gw.Close)
		gw.SetListChangeTiming(40*time.Millisecond, time.Second)
		gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:         "echo",
			LocalProcess: true,
			Command:      []string{mockStdioBin, "-list-changed"},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, stream := startListChangedUpstream(t, gw)
		before, err := gw.HandleToolsList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      "echo__mutate_tools",
			Arguments: map[string]any{"action": "add"},
		}); err != nil {
			t.Fatal(err)
		}
		waitListChanged(t, stream.body, 1, 5*time.Second)
		deadline := time.Now().Add(3 * time.Second)
		for {
			after, err := gw.HandleToolsList(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Tools) > len(before.Tools) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("tool count stayed %d", len(before.Tools))
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("process block", func(t *testing.T) {
		gw := mcp.NewGateway()
		t.Cleanup(gw.Close)
		store := pins.NewWithPath(t.TempDir(), "list-changed-block")
		gw.SetSchemaVerifier(pins.NewGatewayAdapter(store), "block")
		gw.SetListChangeTiming(40*time.Millisecond, time.Second)
		gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:         "echo",
			LocalProcess: true,
			Command:      []string{mockStdioBin, "-list-changed"},
		}); err != nil {
			t.Fatal(err)
		}
		_, _, stream := startListChangedUpstream(t, gw)
		listed, err := gw.HandleToolsList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range listed.Tools {
			names = append(names, tool.Name)
		}
		result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      "echo__mutate_tools",
			Arguments: map[string]any{"action": "modify"},
		})
		if err != nil {
			t.Fatalf("call: %v tools=%v", err, names)
		}
		if result == nil || result.IsError {
			t.Fatalf("mutate result = %#v tools=%v", result, names)
		}
		waitListChanged(t, stream.body, 1, 5*time.Second)
		result, err = gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      "echo__echo",
			Arguments: map[string]any{"message": "hi"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || !result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "gridctl pins approve echo") {
			t.Fatalf("blocked call = %#v", result)
		}
	})

	t.Run("http sse", func(t *testing.T) {
		port := freePort(t)
		startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-sse", "-list-changed")
		waitForPort(t, ctx, port)
		gw := mcp.NewGateway()
		t.Cleanup(gw.Close)
		gw.SetListChangeTiming(40*time.Millisecond, time.Second)
		gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:      "echo",
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		}); err != nil {
			t.Fatal(err)
		}
		apiSrv := httptest.NewServer(api.NewServer(gw, nil).Handler())
		t.Cleanup(apiSrv.Close)
		statusResp, err := http.Get(apiSrv.URL + "/api/mcp-servers")
		if err != nil {
			t.Fatal(err)
		}
		var statuses []mcp.MCPServerStatus
		if err := json.NewDecoder(statusResp.Body).Decode(&statuses); err != nil {
			statusResp.Body.Close()
			t.Fatal(err)
		}
		statusResp.Body.Close()
		var declared bool
		for _, st := range statuses {
			if st.Name == "echo" && st.Capabilities.ToolsListChanged {
				declared = true
			}
		}
		if !declared {
			t.Fatalf("api status = %#v", statuses)
		}
		_, _, stream := startListChangedUpstream(t, gw)
		before, err := gw.HandleToolsList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      "echo__mutate_tools",
			Arguments: map[string]any{"action": "add"},
		}); err != nil {
			t.Fatal(err)
		}
		waitListChanged(t, stream.body, 1, 5*time.Second)
		deadline := time.Now().Add(3 * time.Second)
		for {
			after, err := gw.HandleToolsList(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Tools) > len(before.Tools) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("http tool count stayed %d", len(before.Tools))
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}
