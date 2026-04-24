package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/token"
	"go.uber.org/mock/gomock"
)

func TestNewGateway(t *testing.T) {
	g := NewGateway()
	if g == nil {
		t.Fatal("NewGateway returned nil")
	}
	if g.Router() == nil {
		t.Error("Router should not be nil")
	}
	if g.Sessions() == nil {
		t.Error("Sessions should not be nil")
	}

	info := g.ServerInfo()
	if info.Name != "gridctl-gateway" {
		t.Errorf("expected server name 'gridctl-gateway', got '%s'", info.Name)
	}
	if info.Version != "dev" {
		t.Errorf("expected version 'dev', got '%s'", info.Version)
	}
}

func TestGateway_SetVersion(t *testing.T) {
	g := NewGateway()
	g.SetVersion("v0.1.0-alpha.2")

	info := g.ServerInfo()
	if info.Version != "v0.1.0-alpha.2" {
		t.Errorf("expected version 'v0.1.0-alpha.2', got '%s'", info.Version)
	}
}

func TestGateway_HandleInitialize(t *testing.T) {
	g := NewGateway()
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
		Capabilities:    Capabilities{},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProtocolVersion != MCPProtocolVersion {
		t.Errorf("expected protocol version '%s', got '%s'", MCPProtocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "gridctl-gateway" {
		t.Errorf("expected server name 'gridctl-gateway', got '%s'", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if !result.Capabilities.Tools.ListChanged {
		t.Error("expected Tools.ListChanged to be true")
	}
}

func TestGateway_HandleToolsList(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a mock client with tools
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestGateway_HandleToolsCall(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	// Override default CallTool with custom echo behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			msg := args["message"].(string)
			return &ToolCallResult{
				Content: []Content{NewTextContent("Echo: " + msg)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	params := ToolCallParams{
		Name:      "agent1__echo",
		Arguments: map[string]any{"message": "hello"},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Error("expected successful result, got error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "Echo: hello" {
		t.Errorf("expected 'Echo: hello', got '%s'", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_UnknownTool(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()

	params := ToolCallParams{
		Name:      "unknown__tool",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for unknown tool")
	}
}

func TestGateway_HandleToolsCall_InvalidFormat(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()

	params := ToolCallParams{
		Name:      "invalidformat",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid format")
	}
}

func TestGateway_HandleToolsCall_AgentError(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	// Override default CallTool to return error
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("agent error")).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	params := ToolCallParams{
		Name:      "agent1__fail",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when agent fails")
	}
	if len(result.Content) == 0 {
		t.Error("expected error content")
	}
}

func TestGateway_Status(t *testing.T) {
	g := NewGateway()

	// Initially no servers
	statuses := g.Status()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}

	// Add a mock client
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)

	// Store metadata manually (normally done by RegisterMCPServer)
	g.mu.Lock()
	g.serverMeta["agent1"] = MCPServerConfig{
		Name:      "agent1",
		Transport: TransportHTTP,
		Endpoint:  "http://localhost:9000/mcp",
	}
	g.mu.Unlock()

	statuses = g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	status := statuses[0]
	if status.Name != "agent1" {
		t.Errorf("expected name 'agent1', got '%s'", status.Name)
	}
	if status.Transport != TransportHTTP {
		t.Errorf("expected transport 'http', got '%s'", status.Transport)
	}
	if status.ToolCount != 1 {
		t.Errorf("expected 1 tool, got %d", status.ToolCount)
	}
	if !status.Initialized {
		t.Error("expected initialized to be true")
	}
}

func TestGateway_Status_IncludesOutputFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Server with explicit output format
	client1 := setupMockAgentClient(ctrl, "toon-server", []Tool{{Name: "tool1"}})
	g.Router().AddClient(client1)
	g.SetServerMeta(MCPServerConfig{
		Name:         "toon-server",
		Transport:    TransportHTTP,
		OutputFormat: "toon",
	})

	// Server without output format (should inherit gateway default)
	client2 := setupMockAgentClient(ctrl, "default-server", []Tool{{Name: "tool2"}})
	g.Router().AddClient(client2)
	g.SetServerMeta(MCPServerConfig{
		Name:      "default-server",
		Transport: TransportStdio,
	})

	// Set gateway default
	g.SetDefaultOutputFormat("csv")

	statuses := g.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Statuses are sorted by name
	defaultStatus := statuses[0] // "default-server"
	toonStatus := statuses[1]    // "toon-server"

	if toonStatus.OutputFormat != "toon" {
		t.Errorf("toon-server output format = %q, want %q", toonStatus.OutputFormat, "toon")
	}
	if defaultStatus.OutputFormat != "csv" {
		t.Errorf("default-server output format = %q, want %q (gateway default)", defaultStatus.OutputFormat, "csv")
	}
}

func TestGateway_Status_OutputFormat_NoDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "plain-server", []Tool{{Name: "tool1"}})
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{
		Name:      "plain-server",
		Transport: TransportHTTP,
	})

	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].OutputFormat != "" {
		t.Errorf("output format = %q, want empty (no override, no default)", statuses[0].OutputFormat)
	}
}

func TestGateway_UnregisterMCPServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Verify exists
	if len(g.Router().AggregatedTools()) != 1 {
		t.Fatal("expected 1 tool before unregister")
	}

	g.UnregisterMCPServer("agent1")

	if len(g.Router().AggregatedTools()) != 0 {
		t.Error("expected 0 tools after unregister")
	}
	if g.Router().GetClient("agent1") != nil {
		t.Error("expected client to be removed")
	}
}

// closableClient wraps a MockAgentClient to implement io.Closer.
type closableClient struct {
	AgentClient
	closeFn func() error
}

func (c *closableClient) Close() error {
	return c.closeFn()
}

func TestGateway_RestartMCPServer_NotFound(t *testing.T) {
	g := NewGateway()
	err := g.RestartMCPServer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
	if !strings.Contains(err.Error(), "unknown MCP server") {
		t.Errorf("expected 'unknown MCP server' in error, got: %s", err)
	}
}

func TestGateway_RestartMCPServer_ClosesExistingClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var closed atomic.Bool
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &closableClient{
		AgentClient: mock,
		closeFn:     func() error { closed.Store(true); return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP, Endpoint: "http://localhost:9999", External: true})

	// Restart will fail at re-registration (no real server), but close should be called
	_ = g.RestartMCPServer(context.Background(), "server1")

	if !closed.Load() {
		t.Error("expected existing client to be closed")
	}
}

func TestGateway_RestartMCPServer_UnregistersBeforeReregister(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP, Endpoint: "http://localhost:9999", External: true})

	// Verify tools exist before restart
	if len(g.Router().AggregatedTools()) != 1 {
		t.Fatal("expected 1 tool before restart")
	}

	// Restart will fail at re-registration, but unregister should have cleared the router
	_ = g.RestartMCPServer(context.Background(), "server1")

	if g.Router().GetClient("server1") != nil {
		t.Error("expected client to be removed after failed restart")
	}
	if len(g.Router().AggregatedTools()) != 0 {
		t.Error("expected 0 tools after failed restart")
	}
}

func TestGateway_SessionCount(t *testing.T) {
	g := NewGateway()

	if g.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", g.SessionCount())
	}

	_, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "client1", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if g.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", g.SessionCount())
	}
}

func TestGateway_Close(t *testing.T) {
	g := NewGateway()

	// Close without StartCleanup should not panic
	g.Close()

	// Start and close
	ctx := context.Background()
	g.StartCleanup(ctx)
	g.Close()
}

// pingableClient wraps a MockAgentClient to also implement Pingable.
type pingableClient struct {
	AgentClient
	pingFn func(ctx context.Context) error
}

func (p *pingableClient) Ping(ctx context.Context) error {
	return p.pingFn(ctx)
}

func TestGateway_HealthMonitor_DetectsUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Run a single health check
	ctx := context.Background()
	g.checkHealth(ctx)

	// Verify health status
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to be unhealthy")
	}
	if hs.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got '%s'", hs.Error)
	}
	if hs.LastCheck.IsZero() {
		t.Error("expected LastCheck to be set")
	}

	// Verify WARN log
	entries := logBuffer.GetRecent(20)
	found := false
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "MCP server unhealthy" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WARN log for unhealthy server")
	}
}

func TestGateway_HealthMonitor_DetectsHealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if !hs.Healthy {
		t.Error("expected server to be healthy")
	}
	if hs.Error != "" {
		t.Errorf("expected empty error, got '%s'", hs.Error)
	}
	if hs.LastHealthy.IsZero() {
		t.Error("expected LastHealthy to be set")
	}
}

func TestGateway_HealthMonitor_Recovery(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	pingErr := fmt.Errorf("connection refused")
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return pingErr },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()

	// First check: unhealthy
	g.checkHealth(ctx)
	hs := g.GetHealthStatus("server1")
	if hs == nil || hs.Healthy {
		t.Fatal("expected unhealthy after first check")
	}

	// Server recovers
	client.pingFn = func(ctx context.Context) error { return nil }
	g.checkHealth(ctx)

	hs = g.GetHealthStatus("server1")
	if hs == nil || !hs.Healthy {
		t.Fatal("expected healthy after recovery")
	}

	// Verify recovery log
	entries := logBuffer.GetRecent(20)
	found := false
	for _, entry := range entries {
		if entry.Level == "INFO" && entry.Message == "MCP server recovered" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected INFO log for server recovery")
	}
}

func TestGateway_HealthMonitor_SkipsNonPingable(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Use a regular mock (not pingable)
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Should have no health status since client is not Pingable
	hs := g.GetHealthStatus("server1")
	if hs != nil {
		t.Error("expected no health status for non-pingable client")
	}
}

func TestGateway_HealthMonitor_SkipsNonMCPServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a client without server metadata (e.g., A2A adapter)
	mock := setupMockAgentClient(ctrl, "a2a-adapter", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	// Deliberately not calling SetServerMeta

	ctx := context.Background()
	g.checkHealth(ctx)

	hs := g.GetHealthStatus("a2a-adapter")
	if hs != nil {
		t.Error("expected no health status for client without server meta")
	}
}

func TestGateway_HealthMonitor_MultipleServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Server 1: healthy
	mock1 := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client1 := &pingableClient{
		AgentClient: mock1,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client1)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Server 2: unhealthy
	mock2 := setupMockAgentClient(ctrl, "server2", []Tool{{Name: "tool2"}})
	client2 := &pingableClient{
		AgentClient: mock2,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("timeout") },
	}
	g.Router().AddClient(client2)
	g.SetServerMeta(MCPServerConfig{Name: "server2", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	hs1 := g.GetHealthStatus("server1")
	if hs1 == nil || !hs1.Healthy {
		t.Error("expected server1 to be healthy")
	}

	hs2 := g.GetHealthStatus("server2")
	if hs2 == nil || hs2.Healthy {
		t.Error("expected server2 to be unhealthy")
	}
	if hs2.Error != "timeout" {
		t.Errorf("expected error 'timeout', got '%s'", hs2.Error)
	}
}

func TestGateway_Status_IncludesHealth(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Before health check, status should have no health data
	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Healthy != nil {
		t.Error("expected Healthy to be nil before health check")
	}

	// After health check
	g.checkHealth(context.Background())

	statuses = g.Status()
	if statuses[0].Healthy == nil {
		t.Fatal("expected Healthy to be set after health check")
	}
	if !*statuses[0].Healthy {
		t.Error("expected Healthy to be true")
	}
	if statuses[0].LastCheck == nil {
		t.Error("expected LastCheck to be set")
	}
}

func TestGateway_StartHealthMonitor_Lifecycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var pingCount atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn: func(ctx context.Context) error {
			pingCount.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx, cancel := context.WithCancel(context.Background())

	// Start with a very short interval for testing
	g.StartHealthMonitor(ctx, 50*time.Millisecond)

	// Wait for at least 2 checks
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Wait for goroutine to clean up
	time.Sleep(20 * time.Millisecond)

	if pingCount.Load() < 2 {
		t.Errorf("expected at least 2 health checks, got %d", pingCount.Load())
	}

	hs := g.GetHealthStatus("server1")
	if hs == nil || !hs.Healthy {
		t.Error("expected server1 to be healthy")
	}
}

func TestGateway_HealthMonitor_NoRepeatWarnings(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("down") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()

	// Run multiple health checks while server stays unhealthy
	g.checkHealth(ctx)
	g.checkHealth(ctx)
	g.checkHealth(ctx)

	// Should only log WARN once (on first detection)
	entries := logBuffer.GetRecent(20)
	warnCount := 0
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "MCP server unhealthy" {
			warnCount++
		}
	}
	if warnCount != 1 {
		t.Errorf("expected exactly 1 unhealthy WARN log, got %d", warnCount)
	}
}

func TestGateway_GetHealthStatus_NotFound(t *testing.T) {
	g := NewGateway()

	hs := g.GetHealthStatus("nonexistent")
	if hs != nil {
		t.Error("expected nil health status for unknown server")
	}
}

func TestGateway_recomputeRollup_ClearsOnEmptySet(t *testing.T) {
	g := NewGateway()
	name := "server1"

	// Seed prior state as if a replica had been unhealthy and then reaped.
	g.healthMu.Lock()
	g.health[name] = &HealthStatus{Healthy: false, Error: "context deadline exceeded"}
	g.replicaHealth[name] = map[int]*HealthStatus{
		0: {Healthy: false, Error: "context deadline exceeded"},
	}
	g.healthMu.Unlock()

	// Empty set simulates scale-to-zero after the autoscaler reaps the last replica.
	set := NewReplicaSet(name, ReplicaPolicyRoundRobin, nil)
	g.recomputeRollup(name, set)

	g.healthMu.RLock()
	_, hasHealth := g.health[name]
	_, hasReplicaHealth := g.replicaHealth[name]
	g.healthMu.RUnlock()

	if hasHealth {
		t.Error("expected g.health[name] cleared after scale-to-zero; still present")
	}
	if hasReplicaHealth {
		t.Error("expected g.replicaHealth[name] cleared after scale-to-zero; still present")
	}
}

// reconnectableClient wraps a MockAgentClient to implement both Pingable and Reconnectable.
type reconnectableClient struct {
	AgentClient
	pingFn      func(ctx context.Context) error
	reconnectFn func(ctx context.Context) error
}

func (r *reconnectableClient) Ping(ctx context.Context) error {
	return r.pingFn(ctx)
}

func (r *reconnectableClient) Reconnect(ctx context.Context) error {
	return r.reconnectFn(ctx)
}

func TestGateway_HealthMonitor_ReconnectsUnhealthyClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	var reconnected atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(ctx context.Context) error {
			reconnected.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Verify reconnection was attempted
	if reconnected.Load() != 1 {
		t.Errorf("expected 1 reconnection attempt, got %d", reconnected.Load())
	}

	// After successful reconnection, health should be updated to healthy
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if !hs.Healthy {
		t.Error("expected server to be healthy after successful reconnection")
	}

	// Verify reconnection log
	entries := logBuffer.GetRecent(20)
	foundAttempt := false
	foundReconnected := false
	for _, entry := range entries {
		if entry.Level == "INFO" && entry.Message == "attempting reconnection" {
			foundAttempt = true
		}
		if entry.Level == "INFO" && entry.Message == "MCP server reconnected" {
			foundReconnected = true
		}
	}
	if !foundAttempt {
		t.Error("expected 'attempting reconnection' log entry")
	}
	if !foundReconnected {
		t.Error("expected 'MCP server reconnected' log entry")
	}
}

func TestGateway_HealthMonitor_ReconnectionFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(ctx context.Context) error { return fmt.Errorf("container not found") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Health should remain unhealthy after failed reconnection
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to remain unhealthy after failed reconnection")
	}

	// Verify failure log
	entries := logBuffer.GetRecent(20)
	foundFailed := false
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "reconnection failed" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Error("expected 'reconnection failed' WARN log entry")
	}
}

func TestGateway_HealthMonitor_SkipsReconnectForNonReconnectable(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Use pingableClient (not reconnectable)
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Server should be unhealthy but no reconnection attempted (no panic, no error)
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to be unhealthy")
	}
}

func TestGateway_HealthMonitor_SkipsReconnectForHealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var reconnectCount atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil }, // healthy
		reconnectFn: func(ctx context.Context) error {
			reconnectCount.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// No reconnection should be attempted for healthy server
	if reconnectCount.Load() != 0 {
		t.Errorf("expected 0 reconnection attempts for healthy server, got %d", reconnectCount.Load())
	}
}

func TestGateway_RegisterMCPServer_LogsTiming(t *testing.T) {
	g := NewGateway()

	// Set up log buffer to capture logs
	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	// Add a mock client directly to test that RegisterMCPServer logs
	// We can't fully test RegisterMCPServer without real transport,
	// but we can verify the gateway logger is wired up by checking
	// other logged operations. Instead, verify the log methods work
	// by checking tool call logging (which uses the same logger).
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "test-server", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	// Override default CallTool with custom behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("ok")},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx := context.Background()
	_, _ = g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "test-server__echo",
		Arguments: map[string]any{},
	})

	// Verify tool call logging includes timing info
	entries := logBuffer.GetRecent(20)
	foundStarted := false
	foundFinished := false
	for _, entry := range entries {
		if entry.Message == "tool call started" {
			foundStarted = true
		}
		if entry.Message == "tool call finished" {
			foundFinished = true
			if entry.Attrs["duration"] == nil {
				t.Error("expected duration attribute on tool call finished log")
			}
		}
	}
	if !foundStarted {
		t.Error("expected 'tool call started' log entry")
	}
	if !foundFinished {
		t.Error("expected 'tool call finished' log entry")
	}
}

func TestGateway_ImplementsToolCaller(t *testing.T) {
	var _ ToolCaller = (*Gateway)(nil) // compile-time check
}

func TestGateway_CallTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			msg := args["message"].(string)
			return &ToolCallResult{
				Content: []Content{NewTextContent("Echo: " + msg)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.CallTool(ctx, "agent1__echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected successful result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Echo: hello" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// promptProviderClient wraps a MockAgentClient to also implement PromptProvider.
type promptProviderClient struct {
	AgentClient
	prompts []PromptData
}

func (p *promptProviderClient) ListPromptData() []PromptData {
	return p.prompts
}

func (p *promptProviderClient) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

func TestGateway_HandleInitialize_WithRegistry(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Register a registry client that implements PromptProvider
	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{{Name: "test-prompt"}},
	}
	g.Router().AddClient(client)

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if result.Capabilities.Prompts == nil {
		t.Error("expected Prompts capability to be set")
	}
	if result.Capabilities.Prompts != nil && !result.Capabilities.Prompts.ListChanged {
		t.Error("expected Prompts.ListChanged to be true")
	}
	if result.Capabilities.Resources == nil {
		t.Error("expected Resources capability to be set")
	}
	if result.Capabilities.Resources != nil && !result.Capabilities.Resources.ListChanged {
		t.Error("expected Resources.ListChanged to be true")
	}
}

func TestGateway_HandleInitialize_WithoutRegistry(t *testing.T) {
	g := NewGateway()

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if result.Capabilities.Prompts != nil {
		t.Error("expected Prompts capability to be nil without registry")
	}
	if result.Capabilities.Resources != nil {
		t.Error("expected Resources capability to be nil without registry")
	}
}

func TestGateway_HandlePromptsList_Empty(t *testing.T) {
	g := NewGateway()

	result, err := g.HandlePromptsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Prompts == nil {
		t.Fatal("expected non-nil prompts slice")
	}
	if len(result.Prompts) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(result.Prompts))
	}
}

func TestGateway_HandlePromptsList_WithPrompts(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "code-review",
				Description: "Review code for issues",
				Arguments: []PromptArgumentData{
					{Name: "language", Description: "Programming language", Required: true},
					{Name: "style", Description: "Review style", Required: false},
				},
			},
			{
				Name:        "summarize",
				Description: "Summarize content",
			},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandlePromptsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(result.Prompts))
	}

	// Find the code-review prompt
	var found bool
	for _, p := range result.Prompts {
		if p.Name == "code-review" {
			found = true
			if p.Description != "Review code for issues" {
				t.Errorf("expected description 'Review code for issues', got %q", p.Description)
			}
			if len(p.Arguments) != 2 {
				t.Errorf("expected 2 arguments, got %d", len(p.Arguments))
			}
			if p.Arguments[0].Name != "language" || !p.Arguments[0].Required {
				t.Errorf("unexpected first argument: %+v", p.Arguments[0])
			}
			break
		}
	}
	if !found {
		t.Error("expected 'code-review' prompt to be present")
	}
}

func TestGateway_HandlePromptsGet_ArgumentSubstitution(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "greet",
				Description: "Greeting prompt",
				Content:     "Hello {{name}}, welcome to {{place}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
					{Name: "place", Description: "Location", Required: false, Default: "the world"},
				},
			},
		},
	}
	g.Router().AddClient(client)

	// Test with all arguments provided
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{"name": "Alice", "place": "Wonderland"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Description != "Greeting prompt" {
		t.Errorf("expected description 'Greeting prompt', got %q", result.Description)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content.Text != "Hello Alice, welcome to Wonderland!" {
		t.Errorf("expected substituted content, got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_DefaultArguments(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "greet",
				Content: "Hello {{name}}, welcome to {{place}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
					{Name: "place", Description: "Location", Default: "the world"},
				},
			},
		},
	}
	g.Router().AddClient(client)

	// Test with missing "place" argument — should use default
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{"name": "Bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Messages[0].Content.Text != "Hello Bob, welcome to the world!" {
		t.Errorf("expected default substitution, got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandlePromptsGet(PromptsGetParams{Name: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent prompt")
	}
}

func TestGateway_HandlePromptsGet_NoRegistry(t *testing.T) {
	g := NewGateway()

	_, err := g.HandlePromptsGet(PromptsGetParams{Name: "anything"})
	if err == nil {
		t.Fatal("expected error when no registry")
	}
	if err.Error() != "registry not available" {
		t.Errorf("expected 'registry not available' error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesList(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{Name: "code-review", Description: "Review code"},
			{Name: "summarize", Description: "Summarize content"},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandleResourcesList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result.Resources))
	}

	for _, r := range result.Resources {
		if r.MimeType != "text/markdown" {
			t.Errorf("expected mimeType 'text/markdown', got %q", r.MimeType)
		}
		if r.URI != "skills://registry/"+r.Name {
			t.Errorf("expected URI 'skills://registry/%s', got %q", r.Name, r.URI)
		}
	}
}

func TestGateway_HandleResourcesList_Empty(t *testing.T) {
	g := NewGateway()

	result, err := g.HandleResourcesList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resources == nil {
		t.Fatal("expected non-nil resources slice")
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result.Resources))
	}
}

func TestGateway_HandleResourcesRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "code-review",
				Content: "Please review the following code.",
			},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/code-review"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Contents))
	}
	if result.Contents[0].URI != "skills://registry/code-review" {
		t.Errorf("expected URI 'skills://registry/code-review', got %q", result.Contents[0].URI)
	}
	if result.Contents[0].MimeType != "text/markdown" {
		t.Errorf("expected mimeType 'text/markdown', got %q", result.Contents[0].MimeType)
	}
	if result.Contents[0].Text != "Please review the following code." {
		t.Errorf("expected prompt content, got %q", result.Contents[0].Text)
	}
}

func TestGateway_HandleResourcesRead_InvalidURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "https://example.com/foo"})
	if err == nil {
		t.Fatal("expected error for non-prompt:// URI")
	}
	if !strings.Contains(err.Error(), "unsupported URI scheme") {
		t.Errorf("expected 'unsupported URI scheme' error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesRead_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent prompt")
	}
}

func TestGateway_HandleResourcesRead_NoRegistry(t *testing.T) {
	g := NewGateway()

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://anything"})
	if err == nil {
		t.Fatal("expected error when no registry")
	}
}

func TestGateway_HandlePromptsGet_NilArguments(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "simple",
				Content: "Hello world",
			},
		},
	}
	g.Router().AddClient(client)

	// nil arguments map should work for prompts without required args
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "simple",
		Arguments: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Messages[0].Content.Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_RequiredArgumentMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "greet",
				Content: "Hello {{name}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
				},
			},
		},
	}
	g.Router().AddClient(client)

	_, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{},
	})
	if err == nil {
		t.Fatal("expected error for missing required argument")
	}
	if !strings.Contains(err.Error(), "required argument") {
		t.Errorf("expected 'required argument' in error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesRead_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/"})
	if err == nil {
		t.Fatal("expected error for empty resource name")
	}
	if !strings.Contains(err.Error(), "empty resource name") {
		t.Errorf("expected 'empty resource name' in error, got %q", err.Error())
	}
}

func TestGateway_SetCodeMode(t *testing.T) {
	g := NewGateway()

	// Default is off
	if g.CodeModeStatus() != "off" {
		t.Errorf("expected initial code mode 'off', got %q", g.CodeModeStatus())
	}

	// Enable code mode
	g.SetCodeMode(30 * time.Second)
	if g.CodeModeStatus() != "on" {
		t.Errorf("expected code mode 'on', got %q", g.CodeModeStatus())
	}
}

func TestGateway_CodeModeStatus_Default(t *testing.T) {
	g := NewGateway()
	if g.CodeModeStatus() != "off" {
		t.Errorf("expected 'off', got %q", g.CodeModeStatus())
	}
}

func TestGateway_HandleToolsList_CodeMode(t *testing.T) {
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Code mode should return meta-tools instead of real tools
	if len(result.Tools) == 0 {
		t.Error("expected code mode meta-tools")
	}

	// Meta-tools should include "search" and "execute"
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["search"] && !toolNames["execute"] {
		t.Errorf("expected meta-tools 'search' and 'execute', got %v", toolNames)
	}
}

func TestGateway_RefreshAllTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)

	ctx := context.Background()
	err := g.RefreshAllTools(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_logToolCountHint(t *testing.T) {
	g := NewGateway()
	logBuf := logging.NewLogBuffer(100)
	g.SetLogger(slog.New(logging.NewBufferHandler(logBuf, nil)))

	// Should not warn for <= 50 tools
	g.logToolCountHint(50)
	if g.toolCountWarned {
		t.Error("should not warn for exactly 50 tools")
	}

	// Should warn for > 50 tools
	g.logToolCountHint(51)
	if !g.toolCountWarned {
		t.Error("should warn for 51 tools")
	}

	// Should not warn again (already warned)
	g.logToolCountHint(100)
	// No panic or double warning
}

func TestGateway_HandleToolsList_LogsHint(t *testing.T) {
	g := NewGateway()

	// Add >50 mock tools by creating a mock with many tools
	ctrl := gomock.NewController(t)
	tools := make([]Tool, 55)
	for i := range tools {
		tools[i] = Tool{Name: fmt.Sprintf("tool%d", i), Description: "desc"}
	}
	client := setupMockAgentClient(ctrl, "server1", tools)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 55 {
		t.Errorf("expected 55 tools, got %d", len(result.Tools))
	}
	if !g.toolCountWarned {
		t.Error("expected toolCountWarned to be true after listing >50 tools")
	}
}

func TestGateway_HandleToolsCall_CodeMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "A test tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx := context.Background()

	// Meta-tool search should be handled by code mode
	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected search to succeed")
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
}

func TestGateway_HandleResourcesRead_LegacyPromptURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Set up registry provider
	registryMock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &gatewayTestPromptProvider{
		AgentClient: registryMock,
		prompts: []PromptData{
			{
				Name:        "test-prompt",
				Description: "A prompt",
				Content:     "Hello world",
			},
		},
	}
	g.Router().AddClient(pp)

	// Legacy prompt:// URI should work
	result, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://test-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	if result.Contents[0].Text != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", result.Contents[0].Text)
	}
}

func TestGateway_HandleResourcesRead_EmptyNameInURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	registryMock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &gatewayTestPromptProvider{
		AgentClient: registryMock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(pp)

	// Empty name after prefix strip
	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/"})
	if err == nil {
		t.Fatal("expected error for empty resource name")
	}
	if !strings.Contains(err.Error(), "empty resource name") {
		t.Errorf("expected 'empty resource name' error, got: %v", err)
	}
}

// gatewayTestPromptProvider wraps a MockAgentClient for gateway-level prompt tests.
type gatewayTestPromptProvider struct {
	AgentClient
	prompts []PromptData
}

func (p *gatewayTestPromptProvider) ListPromptData() []PromptData {
	return p.prompts
}

func (p *gatewayTestPromptProvider) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

func TestGateway_SetLogger_Nil(t *testing.T) {
	g := NewGateway()
	// Should not panic when passing nil
	g.SetLogger(nil)
	// Logger should remain the default discard logger
	if g.logger == nil {
		t.Error("logger should not be nil after SetLogger(nil)")
	}
}

func TestGateway_buildSSHCommand(t *testing.T) {
	tests := []struct {
		name     string
		cfg      MCPServerConfig
		wantLen  int
		contains []string
	}{
		{
			name: "basic SSH",
			cfg: MCPServerConfig{
				SSHUser: "user",
				SSHHost: "host.example.com",
				Command: []string{"/opt/server"},
			},
			wantLen:  7,
			contains: []string{"ssh", "user@host.example.com", "/opt/server"},
		},
		{
			name: "SSH with identity file",
			cfg: MCPServerConfig{
				SSHUser:         "admin",
				SSHHost:         "10.0.0.1",
				SSHIdentityFile: "~/.ssh/id_ed25519",
				Command:         []string{"/opt/server"},
			},
			contains: []string{"-i", "~/.ssh/id_ed25519"},
		},
		{
			name: "SSH with custom port",
			cfg: MCPServerConfig{
				SSHUser: "admin",
				SSHHost: "10.0.0.1",
				SSHPort: 2222,
				Command: []string{"/opt/server"},
			},
			contains: []string{"-p", "2222"},
		},
		{
			name: "SSH with default port (22) should not add -p",
			cfg: MCPServerConfig{
				SSHUser: "admin",
				SSHHost: "10.0.0.1",
				SSHPort: 22,
				Command: []string{"/opt/server"},
			},
		},
		{
			name: "SSH with known_hosts file enables strict checking",
			cfg: MCPServerConfig{
				SSHUser:           "admin",
				SSHHost:           "10.0.0.1",
				SSHKnownHostsFile: "/etc/ssh/known_hosts",
				Command:           []string{"/opt/server"},
			},
			contains: []string{"StrictHostKeyChecking=yes", "UserKnownHostsFile=/etc/ssh/known_hosts"},
		},
		{
			name: "SSH with jump host",
			cfg: MCPServerConfig{
				SSHUser:     "admin",
				SSHHost:     "10.0.0.1",
				SSHJumpHost: "bastion.example.com",
				Command:     []string{"/opt/server"},
			},
			contains: []string{"-J", "bastion.example.com"},
		},
		{
			name: "SSH with jump host and known_hosts file",
			cfg: MCPServerConfig{
				SSHUser:           "admin",
				SSHHost:           "10.0.0.1",
				SSHJumpHost:       "user@bastion.example.com",
				SSHKnownHostsFile: "~/.ssh/gridctl_known_hosts",
				Command:           []string{"/opt/server"},
			},
			contains: []string{"-J", "user@bastion.example.com", "StrictHostKeyChecking=yes", "UserKnownHostsFile=~/.ssh/gridctl_known_hosts"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := buildSSHCommand(tc.cfg)
			if tc.wantLen > 0 && len(result) != tc.wantLen {
				t.Errorf("expected %d args, got %d: %v", tc.wantLen, len(result), result)
			}
			for _, want := range tc.contains {
				found := false
				for _, arg := range result {
					if arg == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected command to contain '%s', got: %v", want, result)
				}
			}
			// Default port 22 should not have -p
			if tc.cfg.SSHPort == 22 {
				for _, arg := range result {
					if arg == "-p" {
						t.Error("should not include -p for default port 22")
					}
				}
			}
			// knownHostsFile must not coexist with accept-new
			if tc.cfg.SSHKnownHostsFile != "" {
				for _, arg := range result {
					if arg == "StrictHostKeyChecking=accept-new" {
						t.Error("StrictHostKeyChecking=accept-new must not appear when knownHostsFile is set")
					}
				}
			}
			// -J must appear before user@host
			if tc.cfg.SSHJumpHost != "" {
				jIdx, hostIdx := -1, -1
				for i, arg := range result {
					if arg == "-J" {
						jIdx = i
					}
					if arg == tc.cfg.SSHUser+"@"+tc.cfg.SSHHost {
						hostIdx = i
					}
				}
				if jIdx == -1 {
					t.Error("expected -J flag in command")
				}
				if hostIdx != -1 && jIdx > hostIdx {
					t.Errorf("-J (pos %d) must appear before user@host (pos %d)", jIdx, hostIdx)
				}
			}
		})
	}
}

func TestGateway_Status_SortsAlphabetically(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	clientB := setupMockAgentClient(ctrl, "bravo", []Tool{{Name: "t1"}})
	clientA := setupMockAgentClient(ctrl, "alpha", []Tool{{Name: "t2"}})
	g.Router().AddClient(clientB)
	g.Router().AddClient(clientA)
	g.SetServerMeta(MCPServerConfig{Name: "bravo"})
	g.SetServerMeta(MCPServerConfig{Name: "alpha"})

	statuses := g.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].Name != "alpha" {
		t.Errorf("expected first status 'alpha', got '%s'", statuses[0].Name)
	}
	if statuses[1].Name != "bravo" {
		t.Errorf("expected second status 'bravo', got '%s'", statuses[1].Name)
	}
}

func TestGateway_Status_ExcludesNonMCPClients(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a client without server metadata (e.g., an A2A adapter)
	client := setupMockAgentClient(ctrl, "adapter1", []Tool{{Name: "t1"}})
	g.Router().AddClient(client)
	// Don't call SetServerMeta for adapter1

	statuses := g.Status()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses (non-MCP client excluded), got %d", len(statuses))
	}
}

func TestGateway_Status_OpenAPISpec(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "api-server", []Tool{{Name: "get"}})
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{
		Name:    "api-server",
		OpenAPI: true,
		OpenAPIConfig: &OpenAPIClientConfig{
			Spec: "https://api.example.com/spec.json",
		},
	})

	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].OpenAPI {
		t.Error("expected OpenAPI=true")
	}
	if statuses[0].OpenAPISpec != "https://api.example.com/spec.json" {
		t.Errorf("expected OpenAPI spec URL, got '%s'", statuses[0].OpenAPISpec)
	}
}

func TestSearchIndex_ToolCount(t *testing.T) {
	tools := []Tool{
		{Name: "tool1"},
		{Name: "tool2"},
		{Name: "tool3"},
	}
	idx := NewSearchIndex(tools)
	if idx.ToolCount() != 3 {
		t.Errorf("expected ToolCount=3, got %d", idx.ToolCount())
	}

	emptyIdx := NewSearchIndex(nil)
	if emptyIdx.ToolCount() != 0 {
		t.Errorf("expected ToolCount=0 for empty index, got %d", emptyIdx.ToolCount())
	}
}

func TestCodeMode_HandleCallWithScope_UnknownTool(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	// A tool name that is neither search nor execute
	params := ToolCallParams{
		Name:      "unknown_meta_tool",
		Arguments: map[string]any{},
	}

	result, err := cm.HandleCallWithScope(context.Background(), params, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for unknown code mode tool")
	}
	if !strings.Contains(result.Content[0].Text, "Unknown code mode tool") {
		t.Errorf("expected 'Unknown code mode tool' message, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_SyntaxError(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "const x = {;"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for syntax error")
	}
	if !strings.Contains(result.Content[0].Text, "syntax") {
		t.Errorf("expected syntax error hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_AccessDenied(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			t.Fatal("should not call tool")
			return nil, nil
		},
	}

	allowedTools := []Tool{{Name: "server__allowed"}}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": `mcp.callTool("server", "forbidden", {});`},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, allowedTools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for access denied")
	}
	if !strings.Contains(result.Content[0].Text, "access denied") {
		t.Errorf("expected 'access denied' hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_Timeout(t *testing.T) {
	cm := NewCodeMode(100 * time.Millisecond)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "while(true) {}"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for timeout")
	}
}

func TestCodeMode_HandleExecute_CodeTooLarge(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": strings.Repeat("x", MaxCodeSize+1)},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for code too large")
	}
	if !strings.Contains(result.Content[0].Text, "code too large") {
		t.Errorf("expected 'code too large' hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_NoOutput(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "undefined;"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success for undefined result")
	}
	if result.Content[0].Text != "(no output)" {
		t.Errorf("expected '(no output)', got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_WithConsole(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": `console.log("hello"); "result";`},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success")
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "result") {
		t.Errorf("expected 'result' in output, got: %s", text)
	}
	if !strings.Contains(text, "Console Output") {
		t.Errorf("expected 'Console Output' in output, got: %s", text)
	}
}

func TestCodeMode_HandleExecute_MissingCodeParam(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{}, // no "code" key
	}

	result, err := cm.HandleCall(context.Background(), params, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for missing code param")
	}
	if !strings.Contains(result.Content[0].Text, "'code' parameter is required") {
		t.Errorf("expected 'code parameter required' message, got: %s", result.Content[0].Text)
	}
}

func TestGateway_SetDockerClient(t *testing.T) {
	g := NewGateway()
	g.SetDockerClient(nil) // Should not panic
}

func TestGateway_Close_WithoutStartCleanup(t *testing.T) {
	g := NewGateway()
	// Close without StartCleanup should not panic (cancel is nil)
	g.Close()
}

func TestGateway_Close_WithStartCleanup(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()
	g.StartCleanup(ctx)
	// Close should cancel the cleanup goroutine
	g.Close()
}

func TestCodeMode_HandleSearch_NoQuery(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	tools := []Tool{
		{Name: "server__tool1", Description: "Tool 1"},
		{Name: "server__tool2", Description: "Tool 2"},
	}

	params := ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{},
	}

	result, err := cm.HandleCall(context.Background(), params, nil, tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success for search with no query")
	}
	if !strings.Contains(result.Content[0].Text, "Found 2 tool(s)") {
		t.Errorf("expected all tools returned, got: %s", result.Content[0].Text)
	}
}

// --- Format Conversion Tests ---

func TestGateway_ResolveOutputFormat(t *testing.T) {
	g := NewGateway()

	// Default: no config → json
	if got := g.resolveOutputFormat("server1"); got != "json" {
		t.Errorf("default = %q, want %q", got, "json")
	}

	// Gateway default set
	g.SetDefaultOutputFormat("toon")
	if got := g.resolveOutputFormat("server1"); got != "toon" {
		t.Errorf("with gateway default = %q, want %q", got, "toon")
	}

	// Server override takes precedence
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})
	if got := g.resolveOutputFormat("server1"); got != "csv" {
		t.Errorf("with server override = %q, want %q", got, "csv")
	}

	// Other servers still use gateway default
	if got := g.resolveOutputFormat("server2"); got != "toon" {
		t.Errorf("other server = %q, want %q", got, "toon")
	}

	// Server with empty format uses gateway default
	g.SetServerMeta(MCPServerConfig{Name: "server3", OutputFormat: ""})
	if got := g.resolveOutputFormat("server3"); got != "toon" {
		t.Errorf("empty server format = %q, want %q", got, "toon")
	}
}

func TestGateway_HandleToolsCall_FormatConversion_TOON(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`{"name":"John","age":30,"active":true}`)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}

	text := result.Content[0].Text
	// TOON output should contain key-value pairs (not JSON braces)
	if strings.Contains(text, "{") {
		t.Errorf("expected TOON format (no braces), got: %s", text)
	}
	if !strings.Contains(text, "name: John") {
		t.Errorf("expected 'name: John' in TOON output, got: %s", text)
	}
	if !strings.Contains(text, "age: 30") {
		t.Errorf("expected 'age: 30' in TOON output, got: %s", text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_CSV(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "list", Description: "List items"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`[{"name":"Alice","age":25},{"name":"Bob","age":30}]`)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].Text
	// CSV should have header row with sorted keys
	if !strings.Contains(text, "age,name") {
		t.Errorf("expected CSV header 'age,name', got: %s", text)
	}
	if !strings.Contains(text, "25,Alice") {
		t.Errorf("expected CSV row '25,Alice', got: %s", text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_NonJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "say", Description: "Say something"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("Hello, this is plain text")},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__say",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-JSON text should pass through unchanged
	if result.Content[0].Text != "Hello, this is plain text" {
		t.Errorf("expected unchanged text, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_LargePayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetLogger(logging.NewDiscardLogger())
	// Disable truncation so this test focuses on format conversion skip behavior.
	g.SetMaxToolResultBytes(maxFormatPayloadSize * 10)
	ctx := context.Background()

	// Create a payload > 1MB
	largeJSON := `{"data":"` + strings.Repeat("x", maxFormatPayloadSize+1) + `"}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "big", Description: "Big response"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(largeJSON)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__big",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Large payload should be left unchanged
	if result.Content[0].Text != largeJSON {
		t.Error("expected large payload to be left unchanged")
	}
}

func TestGateway_HandleToolsCall_Truncation(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(100)
	ctx := context.Background()

	largeText := strings.Repeat("a", 500)

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(largeText)}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].Text
	if len(text) >= len(largeText) {
		t.Errorf("expected result to be truncated, got length %d", len(text))
	}
	if !strings.Contains(text, "[truncated: 500 bytes, showing first 100 bytes]") {
		t.Errorf("expected truncation suffix in result, got: %s", text)
	}
}

func TestGateway_HandleToolsCall_Truncation_UnderLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(1000)
	ctx := context.Background()

	smallText := "small result"

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(smallText)}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content[0].Text != smallText {
		t.Errorf("expected unchanged result, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_ServerOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	jsonContent := `{"key":"value"}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Server overrides to json — should skip conversion
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "json"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JSON override: content unchanged
	if result.Content[0].Text != jsonContent {
		t.Errorf("expected JSON passthrough, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_ErrorResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`{"error":"something broke"}`)},
				IsError: true,
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fail",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Error results should not be format-converted
	if !result.IsError {
		t.Error("expected error result")
	}
	if result.Content[0].Text != `{"error":"something broke"}` {
		t.Errorf("expected unchanged error content, got: %s", result.Content[0].Text)
	}
}

// mockFormatSavingsRecorder captures RecordWithSavings calls for testing.
type mockFormatSavingsRecorder struct {
	calls []formatSavingsCall
}

type formatSavingsCall struct {
	serverName      string
	originalTokens  int
	formattedTokens int
}

func (m *mockFormatSavingsRecorder) RecordFormatSavings(serverName string, originalTokens, formattedTokens int) {
	m.calls = append(m.calls, formatSavingsCall{serverName: serverName, originalTokens: originalTokens, formattedTokens: formattedTokens})
}

func TestGateway_HandleToolsCall_FormatConversion_RecordsSavings(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	counter := token.NewHeuristicCounter(4)
	g.SetTokenCounter(counter)
	recorder := &mockFormatSavingsRecorder{}
	g.SetFormatSavingsRecorder(recorder)
	ctx := context.Background()

	jsonContent := `{"name":"John Doe","email":"john@example.com","active":true,"count":42}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get user"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	_, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{"id": "123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 RecordWithSavings call, got %d", len(recorder.calls))
	}

	call := recorder.calls[0]
	if call.serverName != "server1" {
		t.Errorf("serverName = %q, want %q", call.serverName, "server1")
	}
	if call.originalTokens <= 0 {
		t.Error("expected positive originalTokens")
	}
	if call.formattedTokens <= 0 {
		t.Error("expected positive formattedTokens")
	}
	// TOON is typically shorter than JSON
	if call.originalTokens <= call.formattedTokens {
		t.Errorf("expected originalTokens (%d) > formattedTokens (%d) for TOON conversion",
			call.originalTokens, call.formattedTokens)
	}
}

func TestGateway_SetDefaultOutputFormat(t *testing.T) {
	g := NewGateway()

	g.SetDefaultOutputFormat("toon")
	if got := g.resolveOutputFormat("any-server"); got != "toon" {
		t.Errorf("after SetDefaultOutputFormat = %q, want %q", got, "toon")
	}

	g.SetDefaultOutputFormat("")
	if got := g.resolveOutputFormat("any-server"); got != "json" {
		t.Errorf("empty format = %q, want %q", got, "json")
	}
}

func TestGateway_HandleToolsCall_FormatConversion_CSVNonTabular(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	ctx := context.Background()

	// Non-tabular JSON (object, not array) should fail CSV and leave unchanged
	jsonContent := `{"key":"value","nested":{"a":1}}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CSV conversion should fail for non-tabular data, leaving content unchanged
	if result.Content[0].Text != jsonContent {
		t.Errorf("expected unchanged content on CSV failure, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleInitialize_Instructions(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	c1 := setupMockAgentClient(ctrl, "server-a", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})
	c2 := setupMockAgentClient(ctrl, "server-b", []Tool{
		{Name: "tool3", Description: "Tool 3"},
	})
	g.SetServerMeta(MCPServerConfig{Name: "server-a"})
	g.SetServerMeta(MCPServerConfig{Name: "server-b"})
	g.Router().AddClient(c1)
	g.Router().AddClient(c2)
	g.Router().RefreshTools()

	result, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Instructions == "" {
		t.Fatal("expected Instructions to be non-empty")
	}
	if !strings.Contains(result.Instructions, "server-a") {
		t.Errorf("Instructions missing server-a: %q", result.Instructions)
	}
	if !strings.Contains(result.Instructions, "server-b") {
		t.Errorf("Instructions missing server-b: %q", result.Instructions)
	}
	if !strings.Contains(result.Instructions, "__") {
		t.Errorf("Instructions missing prefixed name example: %q", result.Instructions)
	}
	if strings.Contains(result.Instructions, "code mode") {
		t.Errorf("Instructions should not mention code mode when off: %q", result.Instructions)
	}
}

func TestGateway_HandleInitialize_InstructionsCodeMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	c1 := setupMockAgentClient(ctrl, "server-a", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.SetServerMeta(MCPServerConfig{Name: "server-a"})
	g.Router().AddClient(c1)
	g.Router().RefreshTools()

	result, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Instructions, "search") {
		t.Errorf("Code mode Instructions missing 'search': %q", result.Instructions)
	}
	if !strings.Contains(result.Instructions, "execute") {
		t.Errorf("Code mode Instructions missing 'execute': %q", result.Instructions)
	}
}

func TestGateway_HandleInitialize_InstructionsNoServers(t *testing.T) {
	g := NewGateway()

	result, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Instructions != "" {
		t.Errorf("expected empty Instructions with no servers, got: %q", result.Instructions)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	if strings.Contains(string(data), "instructions") {
		t.Errorf("JSON should omit instructions field when empty, got: %s", data)
	}
}

func TestGateway_HandleInitialize_InstructionsFiltersNonMCP(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// MCP server: registered in both serverMeta and router
	mcpClient := setupMockAgentClient(ctrl, "mcp-server", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.SetServerMeta(MCPServerConfig{Name: "mcp-server"})
	g.Router().AddClient(mcpClient)

	// A2A adapter: registered in router only, no serverMeta entry
	a2aClient := setupMockAgentClient(ctrl, "a2a-adapter", []Tool{
		{Name: "skill1", Description: "Skill 1"},
	})
	g.Router().AddClient(a2aClient)
	g.Router().RefreshTools()

	result, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Instructions, "mcp-server") {
		t.Errorf("Instructions missing MCP server name: %q", result.Instructions)
	}
	if strings.Contains(result.Instructions, "a2a-adapter") {
		t.Errorf("Instructions must not include A2A adapter: %q", result.Instructions)
	}
}

// slowMCPServer returns an httptest.Server whose GET handler sleeps `delay`
// before responding 200. It deliberately ignores the request body so it works
// for both the client's Ping (HEAD-like GET) and a full MCP initialize probe.
func slowMCPServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	}))
}

func TestClient_Ping_RespectsConfiguredTimeout(t *testing.T) {
	srv := slowMCPServer(t, 6*time.Second)
	defer srv.Close()

	// Default (zero) PingTimeout falls back to DefaultPingTimeout=5s and must
	// fail against a 6s server.
	cDefault := NewClient("slow", srv.URL)
	start := time.Now()
	err := cDefault.Ping(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("default timeout: expected DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > DefaultPingTimeout+2*time.Second {
		t.Fatalf("default timeout: ping ran too long (%v)", elapsed)
	}

	// Configured 10s PingTimeout succeeds against the same 6s server.
	cTuned := NewClient("slow", srv.URL)
	cTuned.SetPingTimeout(10 * time.Second)
	if err := cTuned.Ping(context.Background()); err != nil {
		t.Fatalf("configured timeout: expected success, got %v", err)
	}
}

func TestWaitForHTTPServer_RespectsCustomTimeout(t *testing.T) {
	// Server would respond eventually, but the per-call timeout is much shorter.
	srv := slowMCPServer(t, 5*time.Second)
	defer srv.Close()

	g := NewGateway()
	client := NewClient("slow", srv.URL)

	start := time.Now()
	err := g.waitForHTTPServer(context.Background(), client, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrReadyTimeout) {
		t.Fatalf("expected ErrReadyTimeout, got %v", err)
	}
	// The poll interval is 500ms, so the timeoutCh (100ms) wins before the first poll.
	// Allow generous slack for CI jitter.
	if elapsed > 2*time.Second {
		t.Fatalf("timeout fired too late: %v", elapsed)
	}
	if !strings.Contains(err.Error(), "ready_timeout=") {
		t.Errorf("error text should mention ready_timeout hint, got: %v", err)
	}
}

func TestWaitForHTTPServer_FallsBackToDefaultWhenZero(t *testing.T) {
	// Fast server: a zero timeout should still succeed via DefaultReadyTimeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	g := NewGateway()
	client := NewClient("fast", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := g.waitForHTTPServer(ctx, client, 0); err != nil {
		t.Fatalf("expected success with zero timeout (falls back to default), got %v", err)
	}
}

func TestWaitForHTTPServer_CancelNotReadyTimeout(t *testing.T) {
	// Canceling the caller ctx must not be reported as a ready-timeout,
	// because the gateway skips cleanup on cancel but runs it on timeout.
	srv := slowMCPServer(t, 5*time.Second)
	defer srv.Close()

	g := NewGateway()
	client := NewClient("cancelled", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := g.waitForHTTPServer(ctx, client, time.Hour)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if errors.Is(err, ErrReadyTimeout) {
		t.Fatalf("cancellation must not surface as ErrReadyTimeout, got %v", err)
	}
}

func TestHandleReadyFailure_InvokesCleanupOnTimeout(t *testing.T) {
	var called int32
	cfg := MCPServerConfig{
		Name:         "slow-http",
		Transport:    TransportHTTP,
		ReadyTimeout: 10 * time.Millisecond,
		CleanupOnReadyFailure: func(ctx context.Context) error {
			atomic.AddInt32(&called, 1)
			return nil
		},
	}

	timeoutErr := fmt.Errorf("wrapped: %w", ErrReadyTimeout)

	g := NewGateway()
	g.handleReadyFailure(context.Background(), cfg, timeoutErr)

	if got := atomic.LoadInt32(&called); got != 1 {
		t.Fatalf("expected cleanup to be invoked exactly once on ready-timeout, got %d", got)
	}
}

func TestHandleReadyFailure_SkipsCleanupOnNonTimeout(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"context cancelled", context.Canceled},
		{"generic error", errors.New("boom")},
		{"nil cleanup on timeout", nil}, // ensures we do not panic on nil cleanup
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var called int32
			cleanup := func(ctx context.Context) error {
				atomic.AddInt32(&called, 1)
				return nil
			}
			cfg := MCPServerConfig{
				Name:                  "srv",
				CleanupOnReadyFailure: cleanup,
			}
			inputErr := tc.err
			if tc.name == "nil cleanup on timeout" {
				cfg.CleanupOnReadyFailure = nil
				inputErr = ErrReadyTimeout
			}

			g := NewGateway()
			g.handleReadyFailure(context.Background(), cfg, inputErr)

			if got := atomic.LoadInt32(&called); got != 0 {
				t.Fatalf("cleanup must not fire for %s, got call count %d", tc.name, got)
			}
		})
	}
}

func TestHandleReadyFailure_SwallowsCleanupError(t *testing.T) {
	// A cleanup error must not panic or re-bubble — the original timeout
	// error is what the caller will report.
	cfg := MCPServerConfig{
		Name:         "srv",
		ReadyTimeout: 10 * time.Millisecond,
		CleanupOnReadyFailure: func(ctx context.Context) error {
			return errors.New("runtime unavailable")
		},
	}
	g := NewGateway()
	g.handleReadyFailure(context.Background(), cfg, fmt.Errorf("%w: waited 10ms", ErrReadyTimeout))
	// No assertions: if this returns cleanly, the test passes.
}

func TestRegisterMCPServer_DoesNotCleanupOnInitializeError(t *testing.T) {
	// Stand up a fast HTTP server so waitForHTTPServer succeeds. The client
	// will then fail Initialize (the endpoint returns 200 to Ping but does not
	// speak MCP). That error path must NOT invoke the cleanup callback — only
	// ready-timeouts should trigger cleanup.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var cleanupCalled int32
	cfg := MCPServerConfig{
		Name:         "init-fails",
		Transport:    TransportHTTP,
		Endpoint:     srv.URL,
		ReadyTimeout: time.Second,
		CleanupOnReadyFailure: func(ctx context.Context) error {
			atomic.AddInt32(&cleanupCalled, 1)
			return nil
		},
	}

	g := NewGateway()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := g.RegisterMCPServer(ctx, cfg)
	if err == nil {
		t.Fatal("expected Initialize/RefreshTools to fail against the fake HTTP server")
	}
	if errors.Is(err, ErrReadyTimeout) {
		t.Fatalf("registration error should not carry ErrReadyTimeout: %v", err)
	}
	if got := atomic.LoadInt32(&cleanupCalled); got != 0 {
		t.Errorf("cleanup must NOT fire when waitForHTTPServer succeeded and a later step failed, got %d calls", got)
	}
}

// Phase-2 replicas: multi-replica health isolation and backoff.

// installReplicaSet builds a fake multi-replica set using reconnectableClient
// fakes so tests can drive per-replica Ping/Reconnect behavior.
func installReplicaSet(t *testing.T, g *Gateway, name string, clients []AgentClient) *ReplicaSet {
	t.Helper()
	set := NewReplicaSet(name, ReplicaPolicyRoundRobin, clients)
	g.Router().AddReplicaSet(set)
	g.SetServerMeta(MCPServerConfig{Name: name, Transport: TransportStdio})
	return set
}

func TestGateway_HealthMonitor_MultiReplica_IsolatesFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var pings [3]atomic.Int32
	makeReplica := func(id int, pingFn func(context.Context) error) AgentClient {
		mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
		return &reconnectableClient{
			AgentClient: mock,
			pingFn: func(ctx context.Context) error {
				pings[id].Add(1)
				return pingFn(ctx)
			},
			reconnectFn: func(ctx context.Context) error { return fmt.Errorf("still broken") },
		}
	}

	clients := []AgentClient{
		makeReplica(0, func(ctx context.Context) error { return nil }),
		makeReplica(1, func(ctx context.Context) error { return fmt.Errorf("replica-1 down") }),
		makeReplica(2, func(ctx context.Context) error { return nil }),
	}
	set := installReplicaSet(t, g, "svc", clients)

	g.checkHealth(context.Background())

	// Replica-1 must be marked unhealthy and excluded from dispatch.
	if set.Replicas()[1].Healthy() {
		t.Error("replica-1 should be unhealthy")
	}
	if !set.Replicas()[0].Healthy() || !set.Replicas()[2].Healthy() {
		t.Error("replicas 0 and 2 should be healthy")
	}

	// Server-level rollup is healthy because replicas 0 and 2 are up.
	hs := g.GetHealthStatus("svc")
	if hs == nil || !hs.Healthy {
		t.Errorf("rollup should be healthy when any replica is healthy: %+v", hs)
	}

	// Tool-call routing must never return replica-1 while it's unhealthy.
	for i := 0; i < 20; i++ {
		client, _, err := g.Router().RouteToolCall("svc__t")
		if err != nil {
			t.Fatalf("RouteToolCall %d: %v", i, err)
		}
		if client == clients[1] {
			t.Fatal("router returned unhealthy replica-1")
		}
	}
}

func TestGateway_HealthMonitor_MultiReplica_RecoversReplica(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var (
		pingMu       sync.Mutex
		replica1Down = true
	)
	replica1Ping := func(ctx context.Context) error {
		pingMu.Lock()
		defer pingMu.Unlock()
		if replica1Down {
			return fmt.Errorf("replica-1 down")
		}
		return nil
	}

	mk := func(pingFn func(context.Context) error) AgentClient {
		mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
		return &reconnectableClient{
			AgentClient: mock,
			pingFn:      pingFn,
			// Reconnect fails so replica-1 stays excluded until Ping itself
			// succeeds — this isolates the "recovery via successful Ping"
			// path from the "recovery via successful Reconnect" path.
			reconnectFn: func(ctx context.Context) error { return fmt.Errorf("reconnect failed") },
		}
	}

	clients := []AgentClient{
		mk(func(ctx context.Context) error { return nil }),
		mk(replica1Ping),
	}
	set := installReplicaSet(t, g, "svc", clients)

	ctx := context.Background()
	g.checkHealth(ctx)
	if set.Replicas()[1].Healthy() {
		t.Fatal("replica-1 should be unhealthy after first check (ping + reconnect both fail)")
	}
	if attempts := set.Replicas()[1].Restart().Attempts(); attempts != 1 {
		t.Errorf("expected backoff attempts=1 after failed reconnect, got %d", attempts)
	}

	// Simulate the replica coming back: flip Ping to succeed and bypass the
	// backoff window by resetting it. The next health check sees a healthy
	// replica directly (no reconnect needed) and puts it back in rotation.
	pingMu.Lock()
	replica1Down = false
	pingMu.Unlock()
	set.Replicas()[1].Restart().Reset()

	g.checkHealth(ctx)

	if !set.Replicas()[1].Healthy() {
		t.Error("replica-1 should be healthy again after Ping recovers")
	}
	if attempts := set.Replicas()[1].Restart().Attempts(); attempts != 0 {
		t.Errorf("backoff should remain reset after recovery, got attempts=%d", attempts)
	}
}

func TestGateway_HealthMonitor_BackoffGatesReconnect(t *testing.T) {
	// A replica whose ping always fails and reconnect always fails must NOT
	// spin: within one health check the reconnect fires at most once, and the
	// backoff has advanced afterwards.
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var reconnectCount atomic.Int32
	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("dead") },
		reconnectFn: func(ctx context.Context) error {
			reconnectCount.Add(1)
			return fmt.Errorf("still dead")
		},
	}
	set := installReplicaSet(t, g, "svc", []AgentClient{client})

	ctx := context.Background()
	// Two consecutive health checks — backoff should gate the second attempt
	// since the first just advanced the delay above zero.
	g.checkHealth(ctx)
	g.checkHealth(ctx)

	got := reconnectCount.Load()
	if got != 1 {
		t.Errorf("expected exactly 1 reconnect attempt (backoff gates the second), got %d", got)
	}
	if attempts := set.Replicas()[0].Restart().Attempts(); attempts != 1 {
		t.Errorf("expected backoff attempts=1 after first failure, got %d", attempts)
	}
	if !set.Replicas()[0].Restart().NextAt().After(time.Now()) {
		t.Error("backoff NextAt should be in the future after a failed reconnect")
	}
}

func TestGateway_HealthMonitor_SingleReplica_UnchangedBehavior(t *testing.T) {
	// A single-replica server must produce the same public health rollup as
	// pre-replicas gridctl: healthy rollup, no stray per-replica leakage in
	// the GetHealthStatus API.
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "svc", Transport: TransportHTTP})

	g.checkHealth(context.Background())

	hs := g.GetHealthStatus("svc")
	if hs == nil || !hs.Healthy {
		t.Fatal("single-replica rollup should be healthy")
	}
	if hs.Error != "" {
		t.Errorf("healthy rollup should have empty Error, got %q", hs.Error)
	}
}

func TestGateway_ReplicaStatuses_PopulatesFromReplicaSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	c0 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	c1 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{c0, c1})
	// Simulate unhealthy replica-1 with a backoff in flight.
	set.Replicas()[1].SetHealthy(false)
	set.Replicas()[1].Restart().Advance(time.Now())
	set.Replicas()[0].IncInFlight()

	g.Router().AddReplicaSet(set)
	g.SetServerMeta(MCPServerConfig{Name: "svc", Transport: TransportHTTP})

	statuses := g.ReplicaStatuses("svc")
	if len(statuses) != 2 {
		t.Fatalf("expected 2 replica statuses, got %d", len(statuses))
	}
	if statuses[0].ReplicaID != 0 || !statuses[0].Healthy || statuses[0].State != "healthy" {
		t.Errorf("replica 0 unexpected: %+v", statuses[0])
	}
	if statuses[0].InFlight != 1 {
		t.Errorf("replica 0 inflight = %d, want 1", statuses[0].InFlight)
	}
	if statuses[1].ReplicaID != 1 || statuses[1].Healthy {
		t.Errorf("replica 1 should be unhealthy: %+v", statuses[1])
	}
	if statuses[1].State != "restarting" {
		t.Errorf("replica 1 state = %q, want restarting", statuses[1].State)
	}
	if statuses[1].RestartAttempts == 0 {
		t.Error("replica 1 should carry restart attempts after Advance()")
	}
	if statuses[1].NextRetryAt == nil {
		t.Error("replica 1 should carry NextRetryAt after Advance()")
	}

	// Status() should thread the same per-replica info through the API shape.
	srvStatuses := g.Status()
	if len(srvStatuses) != 1 {
		t.Fatalf("expected 1 server status, got %d", len(srvStatuses))
	}
	if len(srvStatuses[0].Replicas) != 2 {
		t.Fatalf("expected 2 replicas on MCPServerStatus, got %d", len(srvStatuses[0].Replicas))
	}
}

func TestGateway_ReplicaStatuses_UnknownServer(t *testing.T) {
	g := NewGateway()
	if got := g.ReplicaStatuses("nope"); got != nil {
		t.Errorf("unknown server should return nil, got %+v", got)
	}
}

