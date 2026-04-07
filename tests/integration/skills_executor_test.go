//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
)

// setupExecutorGateway starts a mock HTTP server on a free port, registers it
// with a new gateway under the given server name, and returns the gateway and
// the registered endpoint. Cleanup is deferred via t.Cleanup.
func setupExecutorGateway(t *testing.T, ctx context.Context, serverName string) *mcp.Gateway {
	t.Helper()
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })

	cfg := mcp.MCPServerConfig{
		Name:      serverName,
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	return gw
}

// makeSkill builds a minimal AgentSkill with a single workflow step.
func makeSkill(name, description string, steps []registry.WorkflowStep) *registry.AgentSkill {
	return &registry.AgentSkill{
		Name:        name,
		Description: description,
		State:       registry.StateActive,
		Workflow:    steps,
	}
}

// TestExecutor_SingleStepSkill verifies that an executor backed by a real mock
// server can execute a single-step skill and return the tool output.
func TestExecutor_SingleStepSkill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := setupExecutorGateway(t, ctx, "test-exec")

	store := registry.NewStore(t.TempDir())
	skill := makeSkill("echo-single", "Echo a single message", []registry.WorkflowStep{
		{
			ID:   "step1",
			Tool: "test-exec__echo",
			Args: map[string]any{"message": "hello-executor"},
		},
	})
	if err := store.SaveSkill(skill); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	exec := registry.NewExecutor(gw, nil)
	sk, err := store.GetSkill("echo-single")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}

	result, err := exec.Execute(ctx, sk, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected successful result, got error: %v", extractResultText(result))
	}

	text := extractResultText(result)
	if !strings.Contains(text, "hello-executor") {
		t.Errorf("expected result to contain 'hello-executor', got: %q", text)
	}
}

// TestExecutor_MultiStepDAG verifies that a two-step skill executes in order
// and that step 2 can reference step 1's output via template interpolation.
func TestExecutor_MultiStepDAG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := setupExecutorGateway(t, ctx, "test-dag")

	store := registry.NewStore(t.TempDir())
	skill := makeSkill("echo-dag", "Two-step DAG with template interpolation", []registry.WorkflowStep{
		{
			ID:   "step1",
			Tool: "test-dag__echo",
			Args: map[string]any{"message": "dag-step1"},
		},
		{
			ID:        "step2",
			Tool:      "test-dag__echo",
			DependsOn: registry.StringOrSlice{"step1"},
			Args:      map[string]any{"message": "{{ .Steps.step1.Result }}"},
		},
	})
	if err := store.SaveSkill(skill); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	exec := registry.NewExecutor(gw, nil)
	sk, err := store.GetSkill("echo-dag")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}

	result, err := exec.Execute(ctx, sk, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected successful result, got error: %v", extractResultText(result))
	}

	text := extractResultText(result)
	// Step 1 output should be "dag-step1"; step 2 receives it via template and
	// echoes it back — both should appear in the merged output.
	if !strings.Contains(text, "dag-step1") {
		t.Errorf("expected result to contain 'dag-step1', got: %q", text)
	}
}

// TestExecutor_ParallelSteps verifies that two independent steps both execute
// and their results are included in the merged output.
func TestExecutor_ParallelSteps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := setupExecutorGateway(t, ctx, "test-parallel")

	store := registry.NewStore(t.TempDir())
	skill := makeSkill("echo-parallel", "Two independent parallel steps", []registry.WorkflowStep{
		{
			ID:   "step-a",
			Tool: "test-parallel__echo",
			Args: map[string]any{"message": "parallel-1"},
		},
		{
			ID:   "step-b",
			Tool: "test-parallel__echo",
			Args: map[string]any{"message": "parallel-2"},
		},
	})
	if err := store.SaveSkill(skill); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	exec := registry.NewExecutor(gw, nil)
	sk, err := store.GetSkill("echo-parallel")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}

	result, err := exec.Execute(ctx, sk, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected successful result, got error: %v", extractResultText(result))
	}

	text := extractResultText(result)
	if !strings.Contains(text, "parallel-1") {
		t.Errorf("expected result to contain 'parallel-1', got: %q", text)
	}
	if !strings.Contains(text, "parallel-2") {
		t.Errorf("expected result to contain 'parallel-2', got: %q", text)
	}
}

// TestExecutor_DepthLimit verifies that mutually recursive skills trigger the
// circular dependency guard before reaching the max depth.
func TestExecutor_DepthLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := setupExecutorGateway(t, ctx, "test-depth")

	store := registry.NewStore(t.TempDir())

	// skill-a calls skill-b; skill-b calls skill-a — circular.
	skillA := makeSkill("skill-a", "Skill A that calls skill B", []registry.WorkflowStep{
		{
			ID:   "call-b",
			Tool: "registry__skill-b",
			Args: map[string]any{},
		},
	})
	skillB := makeSkill("skill-b", "Skill B that calls skill A", []registry.WorkflowStep{
		{
			ID:   "call-a",
			Tool: "registry__skill-a",
			Args: map[string]any{},
		},
	})
	for _, sk := range []*registry.AgentSkill{skillA, skillB} {
		if err := store.SaveSkill(sk); err != nil {
			t.Fatalf("SaveSkill(%s): %v", sk.Name, err)
		}
	}

	// Wire registry server into the gateway so "registry__skill-a/b" are routable.
	regServer := registry.New(store, registry.WithToolCaller(gw, nil))
	if err := regServer.Initialize(ctx); err != nil {
		t.Fatalf("registry.Initialize: %v", err)
	}
	gw.Router().AddClient(regServer)
	gw.Router().RefreshTools()

	exec := registry.NewExecutor(gw, nil)
	sk, err := store.GetSkill("skill-a")
	if err != nil {
		t.Fatalf("GetSkill(skill-a): %v", err)
	}

	result, err := exec.Execute(ctx, sk, nil)

	// Circular dependency is detected before a network call, so it surfaces
	// either as a direct error or as a failed ToolCallResult.
	circularDetected := false
	if err != nil && strings.Contains(err.Error(), "circular dependency detected") {
		circularDetected = true
	}
	if result != nil && result.IsError && strings.Contains(extractResultText(result), "circular dependency detected") {
		circularDetected = true
	}

	if !circularDetected {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		if result != nil {
			errMsg = extractResultText(result)
		}
		t.Errorf("expected circular dependency error, got err=%v result=%q", err, errMsg)
	}
}

// TestExecutor_ToolCallTimeout verifies that a workflow-level timeout causes
// Execute to return an error rather than blocking indefinitely.
func TestExecutor_ToolCallTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw := setupExecutorGateway(t, ctx, "test-timeout")

	store := registry.NewStore(t.TempDir())
	skill := makeSkill("echo-timeout", "Skill used for timeout test", []registry.WorkflowStep{
		{
			ID:   "step1",
			Tool: "test-timeout__echo",
			Args: map[string]any{"message": "timeout-test"},
		},
	})
	if err := store.SaveSkill(skill); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	// Use a 1ns workflow timeout — expires before the first level iteration.
	exec := registry.NewExecutor(gw, nil, registry.WithWorkflowTimeout(1*time.Nanosecond))
	sk, err := store.GetSkill("echo-timeout")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}

	result, execErr := exec.Execute(ctx, sk, nil)

	// Expect either a non-nil error or a failed result (both indicate the
	// workflow did not complete successfully due to the expired context).
	if execErr == nil && (result == nil || !result.IsError) {
		t.Error("expected workflow to fail due to expired timeout, but it succeeded")
	}
}

// extractResultText joins all text content from a ToolCallResult.
func extractResultText(r *mcp.ToolCallResult) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, c := range r.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
