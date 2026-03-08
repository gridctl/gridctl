//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // register factory
)

// TestRuntimeDetection_AutoDetect verifies auto-detection finds the available runtime.
func TestRuntimeDetection_AutoDetect(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	if info.Type != runtime.RuntimeDocker && info.Type != runtime.RuntimePodman {
		t.Errorf("unexpected runtime type: %s", info.Type)
	}
	if info.SocketPath == "" {
		t.Error("expected non-empty socket path")
	}
	if info.HostAlias == "" {
		t.Error("expected non-empty host alias")
	}
	t.Logf("Detected runtime: %s (socket: %s, version: %s, host: %s)", info.DisplayName(), info.SocketPath, info.Version, info.HostAlias)
}

// TestRuntimeDetection_ExplicitInvalid verifies explicit selection with invalid runtime errors.
func TestRuntimeDetection_ExplicitInvalid(t *testing.T) {
	_, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: "invalid"})
	if err == nil {
		t.Error("expected error for invalid runtime")
	}
}

// TestRuntimeDetection_EnvVar verifies GRIDCTL_RUNTIME env var selection.
func TestRuntimeDetection_EnvVar(t *testing.T) {
	// Detect what's available first
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	// Set env var to the detected runtime type
	origEnv := os.Getenv("GRIDCTL_RUNTIME")
	os.Setenv("GRIDCTL_RUNTIME", string(info.Type))
	defer os.Setenv("GRIDCTL_RUNTIME", origEnv)

	envInfo, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatalf("DetectRuntime with GRIDCTL_RUNTIME=%s failed: %v", info.Type, err)
	}
	if envInfo.Type != info.Type {
		t.Errorf("expected type %s via env var, got %s", info.Type, envInfo.Type)
	}
}

// TestNewWithInfo_CreateOrchestrator verifies NewWithInfo creates a working orchestrator.
func TestNewWithInfo_CreateOrchestrator(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	orch, err := runtime.NewWithInfo(info)
	if err != nil {
		t.Fatalf("NewWithInfo() error: %v", err)
	}
	defer orch.Close()

	// Verify runtime info is stored
	if orch.RuntimeInfo() == nil {
		t.Error("expected RuntimeInfo to be set")
	}
	if orch.RuntimeInfo().Type != info.Type {
		t.Errorf("expected type %s, got %s", info.Type, orch.RuntimeInfo().Type)
	}

	// Verify the orchestrator can ping the runtime
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := orch.Runtime().Ping(ctx); err != nil {
		t.Errorf("Ping() failed: %v", err)
	}
}

// TestContainerCleanup_CreatedNeverStarted verifies that containers in "created"
// state (never started) are correctly cleaned up by Down().
func TestContainerCleanup_CreatedNeverStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Container runtime not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "integration-cleanup"

	// Ensure clean state
	_ = rt.Down(ctx, stackName)

	// Create network
	if err := rt.Runtime().EnsureNetwork(ctx, stackName+"-net", runtime.NetworkOptions{
		Driver: "bridge",
		Stack:  stackName,
	}); err != nil {
		t.Fatalf("EnsureNetwork() error: %v", err)
	}

	// Start a container that will exit immediately (simulating "created" state)
	cfg := runtime.WorkloadConfig{
		Name:        "cleanup-test",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"true"}, // exits immediately
		NetworkName: stackName + "-net",
		Labels: map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      stackName,
			"gridctl.mcp-server": "cleanup-test",
		},
	}

	// Ensure image is available
	if err := rt.Runtime().EnsureImage(ctx, "alpine:latest"); err != nil {
		t.Fatalf("EnsureImage() error: %v", err)
	}

	_, err = rt.Runtime().Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Brief pause to let container exit
	time.Sleep(2 * time.Second)

	// Verify container exists (in stopped/exited state)
	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("expected at least 1 container in status before cleanup")
	}

	// Down() should clean up even exited/stopped containers
	if err := rt.Down(ctx, stackName); err != nil {
		t.Fatalf("Down() error: %v", err)
	}

	// Verify everything is cleaned up
	statuses, err = rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status() after Down() error: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 containers after cleanup, got %d", len(statuses))
	}
}

// TestOrchestrator_RuntimeAwareHostAlias verifies MCP_ENDPOINT uses the correct
// host alias based on RuntimeInfo when starting agents.
func TestOrchestrator_RuntimeAwareHostAlias(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	// Set Podman 5.0.0 runtime info
	orch.SetRuntimeInfo(&runtime.RuntimeInfo{
		Type:       runtime.RuntimePodman,
		SocketPath: "/run/podman/podman.sock",
		HostAlias:  "host.containers.internal",
		Version:    "5.0.0",
	})

	topo := &config.Stack{
		Version: "1",
		Name:    "test-hostalias",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Agents: []config.Agent{
			{
				Name:  "test-agent",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "some-server"}},
			},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000, GatewayPort: 8180})
	if err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	// Verify the MCP_ENDPOINT contains host.containers.internal (Podman alias)
	if len(mockRT.StartedWorkloads) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(mockRT.StartedWorkloads))
	}

	env := mockRT.StartedWorkloads[0].Env
	endpoint, ok := env["MCP_ENDPOINT"]
	if !ok {
		t.Fatal("MCP_ENDPOINT not set in agent env")
	}
	if endpoint != "http://host.containers.internal:8180?agent=test-agent" {
		t.Errorf("expected Podman host alias in MCP_ENDPOINT, got %q", endpoint)
	}
}

// TestOrchestrator_DockerHostAlias verifies MCP_ENDPOINT uses Docker host alias
// when RuntimeInfo indicates Docker.
func TestOrchestrator_DockerHostAlias(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	orch.SetRuntimeInfo(&runtime.RuntimeInfo{
		Type:       runtime.RuntimeDocker,
		SocketPath: "/var/run/docker.sock",
		HostAlias:  "host.docker.internal",
		Version:    "27.3.1",
	})

	topo := &config.Stack{
		Version: "1",
		Name:    "test-docker-alias",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Agents: []config.Agent{
			{
				Name:  "test-agent",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "some-server"}},
			},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000, GatewayPort: 8180})
	if err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	env := mockRT.StartedWorkloads[0].Env
	endpoint := env["MCP_ENDPOINT"]
	if endpoint != "http://host.docker.internal:8180?agent=test-agent" {
		t.Errorf("expected Docker host alias in MCP_ENDPOINT, got %q", endpoint)
	}
}

// TestOrchestrator_NilRuntimeInfoFallback verifies MCP_ENDPOINT falls back to
// host.docker.internal when RuntimeInfo is nil.
func TestOrchestrator_NilRuntimeInfoFallback(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)
	// No SetRuntimeInfo call — nil runtimeInfo

	topo := &config.Stack{
		Version: "1",
		Name:    "test-nil-ri",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Agents: []config.Agent{
			{
				Name:  "test-agent",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "some-server"}},
			},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000, GatewayPort: 8180})
	if err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	env := mockRT.StartedWorkloads[0].Env
	endpoint := env["MCP_ENDPOINT"]
	if endpoint != "http://host.docker.internal:8180?agent=test-agent" {
		t.Errorf("expected fallback host.docker.internal, got %q", endpoint)
	}
}
