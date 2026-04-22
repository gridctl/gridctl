package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

// gatewayFakeSpawner is the minimum Spawner implementation the gateway-level
// tests need: it returns a pre-seeded AgentClient and tracks Spawn/Reap
// invocations, with optional failure injection.
type gatewayFakeSpawner struct {
	ctrl    *gomock.Controller
	spawns  atomic.Int32
	reaps   atomic.Int32
	failErr error
}

func (f *gatewayFakeSpawner) Spawn(_ context.Context) (AgentClient, error) {
	f.spawns.Add(1)
	if f.failErr != nil {
		return nil, f.failErr
	}
	tools := []Tool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	m := setupMockAgentClient(f.ctrl, "replica", tools)
	m.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil).
		AnyTimes()
	return m, nil
}
func (f *gatewayFakeSpawner) Reap(_ context.Context, _ *Replica) error {
	f.reaps.Add(1)
	return nil
}

func newGatewayFakeSpawner(t *testing.T) *gatewayFakeSpawner {
	return &gatewayFakeSpawner{ctrl: gomock.NewController(t)}
}

func TestGateway_RegisterAutoscaler_StoresMetadataAndScaler(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	template := MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"irrelevant"}}
	sp := newGatewayFakeSpawner(t)
	policy := AutoscalePolicy{Min: 1, Max: 3, TargetInFlight: 3}

	if err := gw.RegisterAutoscaler(context.Background(), template, ReplicaPolicyRoundRobin, sp, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}

	if got := gw.GetAutoscaler("svc"); got == nil {
		t.Error("GetAutoscaler returned nil for registered server")
	}
	if got := gw.Autoscalers(); len(got) != 1 || got[0].Name() != "svc" {
		t.Errorf("Autoscalers = %v, want [svc]", got)
	}
	if got := sp.spawns.Load(); got != 1 {
		t.Errorf("initial tick spawned %d replicas, want 1 (Min=1)", got)
	}
}

func TestGateway_RegisterAutoscaler_Rejects_NilSpawner(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc"}, ReplicaPolicyRoundRobin, nil,
		AutoscalePolicy{Max: 1})
	if err == nil {
		t.Fatal("expected error for nil spawner")
	}
}

func TestGateway_RegisterAutoscaler_Rejects_EmptyName(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: ""}, ReplicaPolicyRoundRobin,
		newGatewayFakeSpawner(t), AutoscalePolicy{Max: 1})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestGateway_RegisterAutoscaler_Rejects_MaxZero(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc"}, ReplicaPolicyRoundRobin,
		newGatewayFakeSpawner(t), AutoscalePolicy{Max: 0})
	if err == nil {
		t.Fatal("expected error for Max=0 policy")
	}
}

func TestGateway_RegisterAutoscaler_IdleToZero_SkipsInitialSpawn(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	sp := newGatewayFakeSpawner(t)
	policy := AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 3, IdleToZero: true}
	if err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}
	if got := sp.spawns.Load(); got != 0 {
		t.Errorf("idle_to_zero + Min=0 + WarmPool=0 spawned %d replicas, want 0", got)
	}
}

func TestGateway_HandleToolsCall_ColdStartViaAutoscaler(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	sp := newGatewayFakeSpawner(t)
	policy := AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 3, IdleToZero: true}
	if err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}
	if got := gw.Router().GetReplicaSet("svc").Size(); got != 0 {
		t.Fatalf("precondition: size = %d, want 0", got)
	}

	result, err := gw.HandleToolsCall(context.Background(), ToolCallParams{
		Name:      PrefixTool("svc", "echo"),
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call returned IsError; content=%v", result.Content)
	}
	if got := sp.spawns.Load(); got != 1 {
		t.Errorf("cold start spawn count = %d, want 1", got)
	}
}

func TestGateway_StartAutoscaler_TicksPeriodically(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping time-based test in short mode")
	}
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	sp := newGatewayFakeSpawner(t)
	policy := AutoscalePolicy{Min: 1, Max: 2, TargetInFlight: 3}
	if err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp, policy); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	gw.StartAutoscaler(ctx, 100*time.Millisecond)

	// After a few tick windows the scaler has run at least once beyond the
	// initial synchronous tick in RegisterAutoscaler.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sp.spawns.Load() >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if sp.spawns.Load() < 1 {
		t.Errorf("tick loop did not run: spawns = %d, want >= 1", sp.spawns.Load())
	}
}

func TestGateway_UnregisterAutoscaler_ViaUnregisterMCPServer(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)

	sp := newGatewayFakeSpawner(t)
	if err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp,
		AutoscalePolicy{Min: 1, Max: 2, TargetInFlight: 3}); err != nil {
		t.Fatalf("RegisterAutoscaler: %v", err)
	}
	if gw.GetAutoscaler("svc") == nil {
		t.Fatal("precondition: scaler should be registered")
	}
	gw.UnregisterMCPServer("svc")
	if gw.GetAutoscaler("svc") != nil {
		t.Error("UnregisterMCPServer did not drop the scaler")
	}
}

func TestGateway_RegisterAutoscaler_InitialTickError_DoesNotFailRegistration(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	sp := &gatewayFakeSpawner{ctrl: gomock.NewController(t), failErr: errors.New("kaboom")}
	err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp,
		AutoscalePolicy{Min: 1, Max: 2, TargetInFlight: 3})
	if err != nil {
		t.Fatalf("RegisterAutoscaler should succeed even when initial tick fails: %v", err)
	}
	if gw.GetAutoscaler("svc") == nil {
		t.Error("scaler not registered despite initial tick failure")
	}
}

func TestGateway_BuildAgentClient_LocalProcess(t *testing.T) {
	// BuildAgentClient is exported so Spawner implementations in pkg/controller
	// can reuse the transport switch. With LocalProcess + an invalid command
	// it must surface the connection error rather than panic.
	gw := NewGateway()
	t.Cleanup(gw.Close)
	gw.SetLogger(slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	_, err := gw.BuildAgentClient(context.Background(), MCPServerConfig{
		Name:         "svc",
		LocalProcess: true,
		Command:      []string{"/this/command/does/not/exist"},
	})
	if err == nil {
		t.Fatal("expected error from BuildAgentClient with invalid command")
	}
}
