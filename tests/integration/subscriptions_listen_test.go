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
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
)

func TestSubscriptionsListen_UpstreamChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-protocol", "2026-07-28", "-list-changed")
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(40*time.Millisecond, time.Second)
	gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)

	bin := buildResourcePrompt(t)
	registerProcess(t, ctx, gw, "docs", bin, "handshake", "-prompts", "-resources")
	time.Sleep(200 * time.Millisecond)

	ts := httptest.NewServer(mcp.NewStreamableHTTPServer(gw, nil))
	t.Cleanup(ts.Close)
	all, allBuf := openIntegrationListen(t, ts.URL, 1, true, true, true, "alice")
	toolsOnly, toolsBuf := openIntegrationListen(t, ts.URL, "tools-only", true, false, false, "alice")
	defer all.Body.Close()
	defer toolsOnly.Body.Close()
	waitSSE(t, allBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
	waitSSE(t, toolsBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)

	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "echo",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ProtocolGeneration: mcp.GenerationStateless,
		ReadyTimeout:       10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	waitSSE(t, allBuf, "notifications/tools/list_changed", 1, 5*time.Second)

	if _, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      "echo__mutate_tools",
		Arguments: map[string]any{"action": "add"},
	}); err != nil {
		t.Fatal(err)
	}
	waitSSE(t, allBuf, "notifications/tools/list_changed", 2, 5*time.Second)

	registerProcess(t, ctx, gw, "extra", bin, "handshake", "-prompts", "-resources")
	waitSSE(t, allBuf, "notifications/prompts/list_changed", 1, 5*time.Second)
	waitSSE(t, allBuf, "notifications/resources/list_changed", 1, 5*time.Second)
	if strings.Count(toolsBuf.String(), "notifications/prompts/list_changed") != 0 || strings.Count(toolsBuf.String(), "notifications/resources/list_changed") != 0 {
		t.Fatalf("tools-only subscriber saw prompt or resource events: %s", toolsBuf.String())
	}
	if err := gw.UnregisterMCPServerContext(ctx, "extra"); err != nil {
		t.Fatal(err)
	}
	waitSSE(t, allBuf, "notifications/prompts/list_changed", 2, 5*time.Second)

	toolsBefore := strings.Count(allBuf.String(), "notifications/tools/list_changed")
	gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]mcp.ClientProfileSpec{"alice": {Servers: []string{"docs"}}},
	}))
	waitSSE(t, allBuf, "notifications/tools/list_changed", toolsBefore+1, 5*time.Second)

	regDir := t.TempDir()
	writeSkill(t, regDir, "notes", "active")
	store := registry.NewStore(regDir)
	reg := registry.New(store)
	if err := reg.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reg.RefreshTools(ctx); err != nil {
		t.Fatal(err)
	}
	gw.Router().AddClient(reg)
	gw.Router().RefreshTools()
	writeSkill(t, regDir, "notes", "inactive")
	if err := reg.RefreshTools(ctx); err != nil {
		t.Fatal(err)
	}
	beforePrompts := strings.Count(allBuf.String(), "notifications/prompts/list_changed")
	gw.NotifyRegistryChanged()
	waitSSE(t, allBuf, "notifications/prompts/list_changed", beforePrompts+1, 5*time.Second)
}

func TestSubscriptionsListen_NoReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(30*time.Millisecond, time.Second)
	ts := httptest.NewServer(mcp.NewStreamableHTTPServer(gw, nil))
	t.Cleanup(ts.Close)

	resp, buf, streamCancel := openIntegrationListenCancel(t, ts.URL, 4, true, false, false, "alice")
	waitSSE(t, buf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
	gw.Router().AddClient(&listenStub{name: "alpha", tools: []mcp.Tool{{Name: "echo", Description: "hi", InputSchema: []byte(`{"type":"object"}`)}}})
	gw.Router().RefreshTools()
	waitSSE(t, buf, "notifications/tools/list_changed", 1, 3*time.Second)
	streamCancel()
	resp.Body.Close()

	again, againBuf := openIntegrationListen(t, ts.URL, 5, true, false, false, "alice")
	defer again.Body.Close()
	first := waitSSE(t, againBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
	if strings.Count(first, "notifications/tools/list_changed") != 0 {
		t.Fatalf("replayed list_changed: %s", first)
	}
	listed, err := gw.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body := statelessIntegrationPost(t, ts.URL, "tools/list", nil)
	var decoded struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Result.Tools) != len(listed.Tools) {
		t.Fatalf("tools/list %d, gateway %d", len(decoded.Result.Tools), len(listed.Tools))
	}
	seen := map[string]int{}
	for _, tool := range decoded.Result.Tools {
		seen[tool.Name]++
	}
	for _, tool := range listed.Tools {
		if seen[tool.Name] != 1 {
			t.Fatalf("tool %s count %d; wire %#v gateway %#v", tool.Name, seen[tool.Name], decoded.Result.Tools, listed.Tools)
		}
	}
}

func TestSubscriptionsListen_DownstreamLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	port := freePort(t)
	cmd := startMockCmd(t, "-port", fmt.Sprintf("%d", port), "-protocol", "2026-07-28", "-list-changed")
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(40*time.Millisecond, time.Second)
	gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)
	apiSrv := httptest.NewServer(api.NewServer(gw, nil).Handler())
	t.Cleanup(apiSrv.Close)
	ts := httptest.NewServer(mcp.NewStreamableHTTPServer(gw, nil))
	t.Cleanup(ts.Close)
	_, session, handshake := startListChangedUpstream(t, gw)
	_ = session
	listen, listenBuf := openIntegrationListen(t, ts.URL, 1, true, false, false, "alice")
	defer listen.Body.Close()
	waitSSE(t, listenBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)

	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "echo",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ProtocolGeneration: mcp.GenerationStateless,
		ReadyTimeout:       10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	waitListeners(t, port, 1, 5*time.Second)
	if got := subscriptionStream(t, apiSrv.URL, "echo"); got != "open" {
		t.Fatalf("subscriptionStream = %q, want open", got)
	}
	if got := statusField(t, gw, "echo"); got != "open" {
		t.Fatalf("gateway status subscriptionStream = %q", got)
	}

	before, err := gw.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	handshakeBefore := strings.Count(handshake.body.String(), listChangedPayload)
	listenBefore := strings.Count(listenBuf.String(), "notifications/tools/list_changed")
	if _, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      "echo__mutate_tools",
		Arguments: map[string]any{"action": "add"},
	}); err != nil {
		t.Fatal(err)
	}
	waitListChanged(t, handshake.body, handshakeBefore+1, 5*time.Second)
	waitSSE(t, listenBuf, "notifications/tools/list_changed", listenBefore+1, 5*time.Second)
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

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waitSubscription(t, gw, "echo", "closed", 5*time.Second)
	cmd = startMockCmd(t, "-port", fmt.Sprintf("%d", port), "-protocol", "2026-07-28", "-list-changed")
	waitForPort(t, ctx, port)
	directMutate(t, port, "add")
	waitSubscription(t, gw, "echo", "open", 8*time.Second)
	deadline = time.Now().Add(5 * time.Second)
	for {
		after, err := gw.HandleToolsList(ctx)
		if err == nil && len(after.Tools) > len(before.Tools) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tool added while down did not appear after reconnect")
		}
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if got := strings.Count(listenBuf.String(), "notifications/tools/list_changed"); got != listenBefore+2 {
		t.Fatalf("reconnect notified %d tools events, want %d: %s", got, listenBefore+2, listenBuf.String())
	}

	if err := gw.UnregisterMCPServerContext(ctx, "echo"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	attempts := listenerStats(t, port)
	time.Sleep(1200 * time.Millisecond)
	if again := listenerStats(t, port); again.Attempts != attempts.Attempts {
		t.Fatalf("loop kept connecting after unregister: %d -> %d", attempts.Attempts, again.Attempts)
	}

	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "echo",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ProtocolGeneration: mcp.GenerationStateless,
		ReadyTimeout:       10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	waitListeners(t, port, 1, 5*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:               "echo",
			Transport:          mcp.TransportHTTP,
			Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
			External:           true,
			ProtocolGeneration: mcp.GenerationStateless,
			ReadyTimeout:       10 * time.Second,
		})
	}()
	sawZero := false
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if listenerStats(t, port).Parked == 0 {
			sawZero = true
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !sawZero {
		t.Fatal("in-place re-registration did not drop the old listen stream")
	}
	waitListeners(t, port, 1, 5*time.Second)
	_ = cmd
}

func TestSubscriptionsListen_UnsupportedAndPins(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	plainPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", plainPort), "-protocol", "2026-07-28")
	waitForPort(t, ctx, plainPort)
	handshakePort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", handshakePort))
	waitForPort(t, ctx, handshakePort)

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "plain",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", plainPort),
		External:           true,
		ProtocolGeneration: mcp.GenerationStateless,
		ReadyTimeout:       10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:         "legacy",
		Transport:    mcp.TransportHTTP,
		Endpoint:     fmt.Sprintf("http://127.0.0.1:%d/mcp", handshakePort),
		External:     true,
		ReadyTimeout: 10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if got := statusField(t, gw, "plain"); got != "unsupported" {
		t.Fatalf("plain subscriptionStream = %q, want unsupported", got)
	}
	if listenerStats(t, plainPort).Attempts != 0 {
		t.Fatalf("unsupported server opened a listen stream")
	}
	raw := statusJSON(t, gw, "legacy")
	if strings.Contains(raw, "subscriptionStream") {
		t.Fatalf("handshake status included subscriptionStream: %s", raw)
	}

	t.Run("pin failure opens no stream", func(t *testing.T) {
		basePort := freePort(t)
		startMockCmd(t, "-port", fmt.Sprintf("%d", basePort), "-protocol", "2026-07-28", "-list-changed")
		waitForPort(t, ctx, basePort)
		store := pins.NewWithPath(t.TempDir(), "listen-block")
		blocked := mcp.NewGateway()
		t.Cleanup(blocked.Close)
		blocked.SetSchemaVerifier(pins.NewGatewayAdapter(store), "block")
		cfg := mcp.MCPServerConfig{
			Name:               "echo",
			Transport:          mcp.TransportHTTP,
			Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", basePort),
			External:           true,
			ProtocolGeneration: mcp.GenerationStateless,
			ReadyTimeout:       10 * time.Second,
		}
		if err := blocked.RegisterMCPServer(ctx, cfg); err != nil {
			t.Fatal(err)
		}
		if err := blocked.UnregisterMCPServerContext(ctx, "echo"); err != nil {
			t.Fatal(err)
		}
		driftPort := freePort(t)
		cmd := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", driftPort), "-protocol", "2026-07-28", "-list-changed")
		cmd.Env = append(os.Environ(), "MOCK_ECHO_DESC=drifted description")
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		})
		waitForPort(t, ctx, driftPort)
		before := listenerStats(t, driftPort).Attempts
		cfg.Endpoint = fmt.Sprintf("http://127.0.0.1:%d/mcp", driftPort)
		// Block mode keeps the server registered and refuses calls. It must
		// not open a listen stream for the unapproved list.
		_ = blocked.RegisterMCPServer(ctx, cfg)
		time.Sleep(400 * time.Millisecond)
		if got := listenerStats(t, driftPort).Attempts; got != before {
			t.Fatalf("blocked registration opened a listen stream: attempts %d -> %d", before, got)
		}
	})

	t.Run("modify blocks before notify", func(t *testing.T) {
		port := freePort(t)
		startMockCmd(t, "-port", fmt.Sprintf("%d", port), "-protocol", "2026-07-28", "-list-changed")
		waitForPort(t, ctx, port)
		store := pins.NewWithPath(t.TempDir(), "listen-modify")
		blocked := mcp.NewGateway()
		t.Cleanup(blocked.Close)
		blocked.SetSchemaVerifier(pins.NewGatewayAdapter(store), "block")
		blocked.SetListChangeTiming(400*time.Millisecond, time.Second)
		blocked.SetDownstreamRefreshDebounce(20 * time.Millisecond)
		if err := blocked.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:               "echo",
			Transport:          mcp.TransportHTTP,
			Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
			External:           true,
			ProtocolGeneration: mcp.GenerationStateless,
			ReadyTimeout:       10 * time.Second,
		}); err != nil {
			t.Fatal(err)
		}
		ts := httptest.NewServer(mcp.NewStreamableHTTPServer(blocked, nil))
		t.Cleanup(ts.Close)
		listen, buf := openIntegrationListen(t, ts.URL, 1, true, false, false, "alice")
		defer listen.Body.Close()
		waitSSE(t, buf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
		before := strings.Count(buf.String(), "notifications/tools/list_changed")
		if _, err := blocked.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      "echo__mutate_tools",
			Arguments: map[string]any{"action": "modify"},
		}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		var blockedCall *mcp.ToolCallResult
		for time.Now().Before(deadline) {
			result, err := blocked.HandleToolsCall(ctx, mcp.ToolCallParams{
				Name:      "echo__echo",
				Arguments: map[string]any{"message": "hi"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result != nil && result.IsError && len(result.Content) > 0 && strings.Contains(result.Content[0].Text, "gridctl pins approve echo") {
				blockedCall = result
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if blockedCall == nil {
			t.Fatal("call was not blocked")
		}
		if strings.Count(buf.String(), "notifications/tools/list_changed") != before {
			t.Fatalf("client notified before the block was in place: %s", buf.String())
		}
	})
}

type listenStub struct {
	name  string
	tools []mcp.Tool
}

func (s *listenStub) Name() string                       { return s.name }
func (s *listenStub) Initialize(context.Context) error   { return nil }
func (s *listenStub) IsInitialized() bool                { return true }
func (s *listenStub) ServerInfo() mcp.ServerInfo         { return mcp.ServerInfo{Name: s.name, Version: "1"} }
func (s *listenStub) Tools() []mcp.Tool                  { return append([]mcp.Tool(nil), s.tools...) }
func (s *listenStub) RefreshTools(context.Context) error { return nil }
func (s *listenStub) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{}, nil
}

type listenerDebug struct {
	Parked   int `json:"parked"`
	Attempts int `json:"attempts"`
}

func listenerStats(t *testing.T, port int) listenerDebug {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/listeners", port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stats listenerDebug
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	return stats
}

func waitListeners(t *testing.T, port, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last listenerDebug
	for time.Now().Before(deadline) {
		last = listenerStats(t, port)
		if last.Parked == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("parked listeners = %d, want %d (attempts %d)", last.Parked, want, last.Attempts)
}

func subscriptionStream(t *testing.T, apiURL, name string) string {
	t.Helper()
	resp, err := http.Get(apiURL + "/api/mcp-servers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var statuses []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st["name"] == name {
			got, _ := st["subscriptionStream"].(string)
			return got
		}
	}
	t.Fatalf("server %s missing from API status", name)
	return ""
}

func statusField(t *testing.T, gw *mcp.Gateway, name string) string {
	t.Helper()
	for _, st := range gw.Status() {
		if st.Name == name {
			return st.SubscriptionStream
		}
	}
	t.Fatalf("server %s missing from gateway status", name)
	return ""
}

func statusJSON(t *testing.T, gw *mcp.Gateway, name string) string {
	t.Helper()
	for _, st := range gw.Status() {
		if st.Name == name {
			raw, err := json.Marshal(st)
			if err != nil {
				t.Fatal(err)
			}
			return string(raw)
		}
	}
	t.Fatalf("server %s missing", name)
	return ""
}

func waitSubscription(t *testing.T, gw *mcp.Gateway, name, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got string
	for time.Now().Before(deadline) {
		got = statusField(t, gw, name)
		if got == want {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("subscriptionStream = %q, want %q", got, want)
}

func openIntegrationListen(t *testing.T, url string, id any, tools, prompts, resources bool, clientID string) (*http.Response, *lockedBuffer) {
	t.Helper()
	resp, buf, _ := openIntegrationListenCancel(t, url, id, tools, prompts, resources, clientID)
	return resp, buf
}

func openIntegrationListenCancel(t *testing.T, url string, id any, tools, prompts, resources bool, clientID string) (*http.Response, *lockedBuffer, context.CancelFunc) {
	t.Helper()
	notes := map[string]any{}
	if tools {
		notes["toolsListChanged"] = true
	}
	if prompts {
		notes["promptsListChanged"] = true
	}
	if resources {
		notes["resourcesListChanged"] = true
	}
	params := map[string]any{
		"notifications": notes,
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "listen-client", "version": "1"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "subscriptions/listen", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	if clientID != "" {
		req.Header.Set("X-Gridctl-Client-Id", clientID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("listen status %d: %s", resp.StatusCode, raw)
	}
	buf := &lockedBuffer{}
	go func() {
		_, _ = io.Copy(buf, resp.Body)
	}()
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	return resp, buf, cancel
}

func waitSSE(t *testing.T, body *lockedBuffer, needle string, n int, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := body.String()
		if strings.Count(got, needle) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d %q, want %d: %s", strings.Count(got, needle), needle, n, got)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func statelessIntegrationPost(t *testing.T, url, method string, params map[string]any) []byte {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "listen-client", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status %d: %s", method, resp.StatusCode, raw)
	}
	return raw
}

func startMockCmd(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(mockHTTPServerBin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

func directMutate(t *testing.T, port int, action string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate_tools","arguments":{"action":%q}}}`, action))
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/mcp", port), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct mutate status %d: %s", resp.StatusCode, raw)
	}
}
