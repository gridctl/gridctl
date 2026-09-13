package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/execution"
)

func TestProcessClient_ExecutionInheritance(t *testing.T) {
	t.Setenv("EXECUTION_TEST_AMBIENT", "synthetic-ambient-value")
	t.Setenv("EXECUTION_TEST_EXPLICIT", "ambient")
	t.Setenv("OP_CONNECT_TOKEN", "synthetic-internal-value")
	for _, names := range [][]string{{}, {"EXECUTION_TEST_AMBIENT", "OP_CONNECT_TOKEN"}} {
		contract := &execution.ExecutionContract{Mode: "local", Lookup: "absolute", Inherit: &names}
		client := newProcessClient("fixture", []string{"/bin/cat"}, "", map[string]string{"EXECUTION_TEST_EXPLICIT": "explicit-value", "OP_CONNECT_TOKEN": "forbidden"}, contract)
		joined := strings.Join(client.env, "\n")
		if strings.Contains(joined, "OP_CONNECT_TOKEN") {
			t.Fatal("internal credential inherited")
		}
		if strings.Contains(joined, "EXECUTION_TEST_AMBIENT=") != (len(names) != 0) {
			t.Fatal("inheritance selection ignored")
		}
		if !strings.Contains(joined, "EXECUTION_TEST_EXPLICIT=explicit-value") {
			t.Fatal("explicit delivery lost")
		}
		body, err := json.Marshal(client.ExecutionReport())
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"synthetic-ambient-value", "explicit-value", "forbidden"} {
			if strings.Contains(string(body), secret) {
				t.Fatal("report leaked environment value")
			}
		}
	}
}

func TestProcessClient_PlatformEnvironmentKeys(t *testing.T) {
	if processEnvKey("op_connect_token", true, "windows") != "OP_CONNECT_TOKEN" {
		t.Fatal("case-insensitive internal credential alias escaped policy")
	}
	if processEnvKey("Path", true, "windows") != "PATH" {
		t.Fatal("Windows inheritance lookup is not canonical")
	}
	if processEnvKey("Path", false, "windows") != "Path" || processEnvKey("Path", true, "linux") != "Path" {
		t.Fatal("compatibility environment changed")
	}
}

func TestProcessClient_ExecutionLookup(t *testing.T) {
	client := newProcessClient("fixture", []string{"cat"}, "", nil, &execution.ExecutionContract{Mode: "local", Lookup: "absolute"})
	if err := client.Connect(context.Background()); err == nil {
		client.Close()
		t.Fatal("bare executable accepted")
	}
	// Go's ErrDot remains enforced even when ambient PATH is explicitly allowed.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", ".")
	client = newProcessClient("fixture", []string{"fixture"}, "", nil, &execution.ExecutionContract{Mode: "local", Lookup: "ambient_path"})
	if err := client.Connect(context.Background()); !errors.Is(err, exec.ErrDot) {
		client.Close()
		t.Fatalf("ErrDot protection lost: %v", err)
	}
}

func TestExecutionClient_AdmissionBlocksDispatch(t *testing.T) {
	client := &executionClient{check: func(context.Context) (*execution.Report, error) { return &execution.Report{Outcome: "unknown"}, nil }}
	if _, err := client.CallTool(context.Background(), "test", nil); err == nil {
		t.Fatal("unknown evidence routed")
	}
	if err := client.Ping(context.Background()); err == nil {
		t.Fatal("unknown evidence became healthy")
	}
	if err := client.Reconnect(context.Background()); err == nil {
		t.Fatal("unknown evidence reconnected")
	}
	if _, err := client.RelayRaw(context.Background(), "tools/call", nil); err == nil {
		t.Fatal("raw relay bypassed evidence")
	}
	client.check = func(context.Context) (*execution.Report, error) { return nil, exec.ErrDot }
	if _, err := client.CallTool(context.Background(), "test", nil); !errors.Is(err, exec.ErrDot) {
		t.Fatal("admission error swallowed")
	}
}

func TestExecutionClient_ReplicaSelection(t *testing.T) {
	for _, policy := range []string{ReplicaPolicyRoundRobin, ReplicaPolicyLeastConnections} {
		good := &executionClient{AgentClient: NewProcessClient("fixture", nil, "", nil), report: &execution.Report{Eligible: true}}
		bad := &executionClient{AgentClient: NewProcessClient("fixture", nil, "", nil), report: &execution.Report{Outcome: "unknown"}}
		set := NewReplicaSet("fixture", policy, []AgentClient{good, bad})
		for range 10 {
			picked, err := set.Pick()
			if err != nil || picked.ID() != 0 {
				t.Fatal("execution-ineligible replica selected")
			}
		}
		if set.HealthyCount() != 1 || !set.Replicas()[1].Healthy() {
			t.Fatal("execution eligibility collapsed into MCP health")
		}
		good.report.Eligible = false
		if _, err := set.Pick(); !errors.Is(err, ErrNoHealthyReplicas) {
			t.Fatal("all-ineligible set remained routable")
		}
	}
}

func TestGateway_ProcessOwnershipSurvivesOperation(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, err := NewGateway().BuildAgentClient(ctx, MCPServerConfig{Name: "fixture", LocalProcess: true, Command: []string{bin, "-test.run=^TestProcessLifecycleHelper$"}, Env: map[string]string{"PROCESS_LIFECYCLE_FIXTURE": "serve"}, ProtocolGeneration: GenerationHandshake, Execution: &execution.ExecutionConfig{Mode: "local"}})
	if err != nil {
		t.Fatal(err)
	}
	process := client.(*ProcessClient)
	defer process.Close()
	cancel()
	select {
	case <-process.done:
		t.Fatal("completed registration context terminated the owned child")
	case <-time.After(50 * time.Millisecond):
	}
	check, done := context.WithTimeout(t.Context(), 2*time.Second)
	defer done()
	if err := process.Ping(check); err != nil {
		t.Fatal(err)
	}
	reconnect, stopReconnect := context.WithCancel(t.Context())
	if err := process.Reconnect(reconnect); err != nil {
		stopReconnect()
		t.Fatal(err)
	}
	stopReconnect()
	select {
	case <-process.done:
		t.Fatal("completed reconnect context terminated the owned child")
	case <-time.After(50 * time.Millisecond):
	}
	if err := process.Ping(check); err != nil {
		t.Fatal(err)
	}
}

func TestGateway_UnregisterReapsOwnedProcesses(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	gateway := NewGateway()
	client, err := gateway.BuildAgentClient(t.Context(), MCPServerConfig{Name: "fixture", LocalProcess: true, Command: []string{bin, "-test.run=^TestProcessLifecycleHelper$"}, Env: map[string]string{"PROCESS_LIFECYCLE_FIXTURE": "serve"}, ProtocolGeneration: GenerationHandshake})
	if err != nil {
		t.Fatal(err)
	}
	process := client.(*ProcessClient)
	defer process.Close()
	gateway.Router().AddClient(client)
	gateway.SetServerMeta(MCPServerConfig{Name: "fixture", LocalProcess: true})
	gateway.UnregisterMCPServer("fixture")
	select {
	case <-process.done:
	case <-time.After(time.Second):
		t.Fatal("unregistered process was not reaped")
	}
	if err := process.Reconnect(t.Context()); err == nil {
		t.Fatal("stale health recovery resurrected a retired process")
	}
}

func TestExecutionClient_CancellationPreservesLastObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := &executionClient{report: &execution.Report{Eligible: true, Outcome: "observed"}, check: func(context.Context) (*execution.Report, error) { cancel(); return nil, context.Canceled }}
	if err := client.admit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if !client.ExecutionEligible() {
		t.Fatal("request cancellation was misreported as enforcement failure")
	}
}

func TestGateway_ProcessRestartPreservesReplicaPool(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	gateway := NewGateway()
	cfg := MCPServerConfig{Name: "fixture", LocalProcess: true, Command: []string{bin, "-test.run=^TestProcessLifecycleHelper$"}, Env: map[string]string{"PROCESS_LIFECYCLE_FIXTURE": "serve"}, ProtocolGeneration: GenerationHandshake, Execution: &execution.ExecutionConfig{Mode: "local"}}
	if err := gateway.RegisterMCPReplicaSet(t.Context(), "fixture", ReplicaPolicyRoundRobin, []MCPServerConfig{cfg, cfg}); err != nil {
		t.Fatal(err)
	}
	defer gateway.UnregisterMCPServer("fixture")
	before := gateway.ReplicaStatuses("fixture")
	ctx, cancel := context.WithCancel(t.Context())
	if err := gateway.RestartMCPServer(ctx, "fixture"); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	after := gateway.ReplicaStatuses("fixture")
	if len(after) != 2 {
		t.Fatal("restart collapsed the process replica pool")
	}
	for i, replica := range after {
		if replica.PID == 0 || replica.PID == before[i].PID || !replica.StartedAt.After(before[i].StartedAt) {
			t.Fatal("restart retained stale process identity or uptime")
		}
	}
}
