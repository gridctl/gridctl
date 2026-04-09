//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // register factory
)

// TestPodmanRootless_MultiContainerNetworking is the graduation gate for stable Podman
// support. It verifies that two containers on a shared named bridge network can resolve
// each other by DNS alias — the mechanism used for inter-container communication in
// gridctl stacks running under rootless Podman with netavark + aardvark-dns.
func TestPodmanRootless_MultiContainerNetworking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}
	if info.Type != runtime.RuntimePodman {
		t.Skip("skipping: requires Podman runtime (Docker detected)")
	}
	t.Logf("Podman version=%s rootless=%v socket=%s", info.Version, info.IsRootless(), info.SocketPath)

	rt, err := runtime.NewWithInfo(info)
	if err != nil {
		t.Fatalf("NewWithInfo() error: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const stackName = "integration-mc-net"
	const netName = stackName + "-net"

	// Ensure clean state before and after.
	_ = rt.Down(ctx, stackName)
	defer func() { _ = rt.Down(ctx, stackName) }()

	// Create a named bridge network. In Podman 4.0+ this uses netavark,
	// which enables inter-container DNS via aardvark-dns.
	if err := rt.Runtime().EnsureNetwork(ctx, netName, runtime.NetworkOptions{
		Driver: "bridge",
		Stack:  stackName,
	}); err != nil {
		t.Fatalf("EnsureNetwork() error: %v", err)
	}

	if err := rt.Runtime().EnsureImage(ctx, "alpine:latest"); err != nil {
		t.Fatalf("EnsureImage() error: %v", err)
	}

	managedLabels := func(name string) map[string]string {
		return map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      stackName,
			"gridctl.mcp-server": name,
		}
	}

	// Start container-b — the DNS lookup target.
	// Its DNS alias on the network will be "container-b" (WorkloadConfig.Name).
	statusB, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name:        "container-b",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", "sleep 30"},
		NetworkName: netName,
		Labels:      managedLabels("container-b"),
	})
	if err != nil {
		t.Fatalf("Start(container-b) error: %v", err)
	}
	t.Logf("container-b started: id=%s state=%s", statusB.ID, statusB.State)

	// Allow container-b to register with the network's DNS before container-a queries it.
	time.Sleep(2 * time.Second)

	// Start container-a — runs nslookup and exits.
	// BusyBox nslookup (included in alpine:latest) resolves via the container network's DNS.
	statusA, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name:        "container-a",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", "nslookup container-b"},
		NetworkName: netName,
		Labels:      managedLabels("container-a"),
	})
	if err != nil {
		t.Fatalf("Start(container-a) error: %v", err)
	}
	t.Logf("container-a started: id=%s state=%s", statusA.ID, statusA.State)

	// Poll until container-a completes (nslookup exits when it gets a response or times out).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := rt.Runtime().Status(ctx, statusA.ID)
		if err != nil {
			t.Fatalf("Status(container-a): %v", err)
		}
		if st.State == runtime.WorkloadStateStopped || st.State == runtime.WorkloadStateFailed {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Verify exit code 0: nslookup exits 0 only on successful resolution.
	dockerCli := rt.DockerClient()
	if dockerCli == nil {
		t.Fatal("DockerClient() returned nil — cannot verify exit code")
	}
	result, err := dockerCli.ContainerInspect(ctx, string(statusA.ID))
	if err != nil {
		t.Fatalf("ContainerInspect(container-a): %v", err)
	}
	t.Logf("container-a exit_code=%d status=%s", result.State.ExitCode, result.State.Status)

	if result.State.ExitCode != 0 {
		t.Errorf("inter-container DNS resolution failed: container-a exited %d (expected 0)\n"+
			"container-a could not resolve 'container-b' by DNS alias on network %q\n"+
			"ensure netavark and aardvark-dns are installed for rootless Podman networking",
			result.State.ExitCode, netName)
	}
}

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

