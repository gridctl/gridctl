package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// mockToolCaller records calls and returns configurable results/errors.
// Thread-safe for concurrent access from parallel step execution.
type mockToolCaller struct {
	mu      sync.Mutex
	calls   []mockCall
	results map[string]*mcp.ToolCallResult
	errors  map[string]error
}

type mockCall struct {
	Name      string
	Arguments map[string]any
}

func newMockToolCaller() *mockToolCaller {
	return &mockToolCaller{
		results: make(map[string]*mcp.ToolCallResult),
		errors:  make(map[string]error),
	}
}

func (m *mockToolCaller) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{Name: name, Arguments: arguments})
	m.mu.Unlock()

	if err, ok := m.errors[name]; ok {
		return nil, err
	}
	if result, ok := m.results[name]; ok {
		return result, nil
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("ok")},
	}, nil
}

func (m *mockToolCaller) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockToolCaller) getCalls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// timedMockCaller adds configurable delays per tool to verify parallelism.
type timedMockCaller struct {
	mu      sync.Mutex
	delays  map[string]time.Duration
	results map[string]*mcp.ToolCallResult
	errors  map[string]error
	order   []string
}

func newTimedMockCaller() *timedMockCaller {
	return &timedMockCaller{
		delays:  make(map[string]time.Duration),
		results: make(map[string]*mcp.ToolCallResult),
		errors:  make(map[string]error),
	}
}

func (m *timedMockCaller) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	if d, ok := m.delays[name]; ok {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	m.order = append(m.order, name)
	m.mu.Unlock()

	if err, ok := m.errors[name]; ok {
		return nil, err
	}
	if result, ok := m.results[name]; ok {
		return result, nil
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("ok")},
	}, nil
}

func (m *timedMockCaller) getOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.order))
	copy(cp, m.order)
	return cp
}

func textResult(text string) *mcp.ToolCallResult {
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(text)},
	}
}

func errorResult(text string) *mcp.ToolCallResult {
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(text)},
		IsError: true,
	}
}

func TestExecutor_SimpleSequential(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("result-a")
	caller.results["server__tool-b"] = textResult("result-b")
	caller.results["server__tool-c"] = textResult("result-c")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "test-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
			{ID: "step-c", Tool: "server__tool-c"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].Text)
	}
	if caller.callCount() != 3 {
		t.Fatalf("expected 3 tool calls, got %d", caller.callCount())
	}
	// Verify merged output
	text := result.Content[0].Text
	if !strings.Contains(text, "result-a") || !strings.Contains(text, "result-b") || !strings.Contains(text, "result-c") {
		t.Errorf("expected merged output, got: %s", text)
	}
}

func TestExecutor_WithDependencies(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("a-out")
	caller.results["server__tool-b"] = textResult("b-out")
	caller.results["server__tool-c"] = textResult("c-out")
	caller.results["server__tool-d"] = textResult("d-out")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "dep-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-c", Tool: "server__tool-c", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-d", Tool: "server__tool-d", DependsOn: StringOrSlice{"step-b", "step-c"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].Text)
	}
	if caller.callCount() != 4 {
		t.Fatalf("expected 4 tool calls, got %d", caller.callCount())
	}

	// Verify execution order: step-a must come before step-b and step-c,
	// step-b and step-c must come before step-d
	calls := caller.getCalls()
	callOrder := make(map[string]int)
	for i, c := range calls {
		callOrder[c.Name] = i
	}
	if callOrder["server__tool-a"] >= callOrder["server__tool-b"] {
		t.Error("step-a should execute before step-b")
	}
	if callOrder["server__tool-a"] >= callOrder["server__tool-c"] {
		t.Error("step-a should execute before step-c")
	}
	if callOrder["server__tool-b"] >= callOrder["server__tool-d"] {
		t.Error("step-b should execute before step-d")
	}
	if callOrder["server__tool-c"] >= callOrder["server__tool-d"] {
		t.Error("step-c should execute before step-d")
	}
}

func TestExecutor_RequiredInputMissing(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "input-skill",
		Description: "test",
		Inputs: map[string]SkillInput{
			"device_ip": {Type: "string", Required: true},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	_, err := exec.Execute(context.Background(), skill, nil)
	if err == nil {
		t.Fatal("expected error for missing required input")
	}
	if !strings.Contains(err.Error(), "device_ip") {
		t.Errorf("error should mention missing input, got: %v", err)
	}
}

func TestExecutor_OptionalInputDefault(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "default-skill",
		Description: "test",
		Inputs: map[string]SkillInput{
			"count": {Type: "number", Default: 5},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Args: map[string]any{
				"n": "{{ inputs.count }}",
			}},
		},
	}

	_, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := caller.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Arguments["n"] != 5 {
		t.Errorf("expected default value 5, got: %v", calls[0].Arguments["n"])
	}
}

func TestExecutor_TemplateResolution(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult(`{"status":"ok"}`)

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "template-skill",
		Description: "test",
		Inputs: map[string]SkillInput{
			"host": {Type: "string", Required: true},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Args: map[string]any{
				"target": "{{ inputs.host }}",
			}},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}, Args: map[string]any{
				"result": "{{ steps.step-a.result }}",
				"status": "{{ steps.step-a.json.status }}",
			}},
		},
	}

	_, err := exec.Execute(context.Background(), skill, map[string]any{"host": "10.1.1.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := caller.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Arguments["target"] != "10.1.1.1" {
		t.Errorf("expected host to resolve, got: %v", calls[0].Arguments["target"])
	}
	if calls[1].Arguments["status"] != "ok" {
		t.Errorf("expected status 'ok', got: %v", calls[1].Arguments["status"])
	}
}

func TestExecutor_OnErrorFail(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("connection refused")

	exec := NewExecutor(caller, nil)
	// step-b depends on step-a so it's in a later DAG level
	skill := &AgentSkill{
		Name:        "fail-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "fail"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	// step-b should not have been called (it's in a later level)
	if caller.callCount() != 1 {
		t.Errorf("expected only 1 call (step-a), got %d", caller.callCount())
	}
}

func TestExecutor_OnErrorSkip(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("connection refused")
	caller.results["server__tool-c"] = textResult("c-result")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "skip-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "skip"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-c", Tool: "server__tool-c"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result with skip policy")
	}
	// step-a fails, step-b should be skipped (depends on step-a), step-c should run
	if caller.callCount() != 2 {
		t.Errorf("expected 2 calls (step-a, step-c), got %d", caller.callCount())
	}
}

func TestExecutor_OnErrorContinue(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("connection refused")
	caller.results["server__tool-b"] = textResult("b-result")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "continue-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "continue"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both should have been called (same level, parallel)
	if caller.callCount() != 2 {
		t.Errorf("expected 2 calls, got %d", caller.callCount())
	}
	if result.IsError {
		t.Error("expected non-error result with continue policy")
	}
}

func TestExecutor_ConditionFalse(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult(`{"valid":false}`)

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "cond-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"},
				Condition: "{{ steps.step-a.json.valid == true }}"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	// step-b should have been skipped (condition false)
	if caller.callCount() != 1 {
		t.Errorf("expected 1 call (step-a only), got %d", caller.callCount())
	}
}

func TestExecutor_OutputMerged(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("first")
	caller.results["server__tool-b"] = textResult("second")
	caller.results["server__tool-c"] = textResult("third")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "merged-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
			{ID: "step-c", Tool: "server__tool-c"},
		},
		Output: &WorkflowOutput{Format: "merged"},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") || !strings.Contains(text, "third") {
		t.Errorf("expected all results merged, got: %s", text)
	}
	if !strings.Contains(text, "---") {
		t.Error("expected separator in merged output")
	}
}

func TestExecutor_OutputMergedWithInclude(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("first")
	caller.results["server__tool-b"] = textResult("second")
	caller.results["server__tool-c"] = textResult("third")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "include-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
			{ID: "step-c", Tool: "server__tool-c"},
		},
		Output: &WorkflowOutput{Format: "merged", Include: []string{"step-a", "step-c"}},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "first") || !strings.Contains(text, "third") {
		t.Errorf("expected included results, got: %s", text)
	}
	if strings.Contains(text, "second") {
		t.Error("step-b should be excluded from output")
	}
}

func TestExecutor_OutputLast(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("first")
	caller.results["server__tool-b"] = textResult("last-result")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "last-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
		Output: &WorkflowOutput{Format: "last"},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].Text
	if text != "last-result" {
		t.Errorf("expected 'last-result', got: %s", text)
	}
}

func TestExecutor_OutputCustom(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("42")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "custom-skill",
		Description: "test",
		Inputs: map[string]SkillInput{
			"name": {Type: "string", Required: true},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
		Output: &WorkflowOutput{
			Format:   "custom",
			Template: "Hello {{ inputs.name }}, result: {{ steps.step-a.result }}",
		},
	}

	result, err := exec.Execute(context.Background(), skill, map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Content[0].Text
	if text != "Hello World, result: 42" {
		t.Errorf("expected custom output, got: %s", text)
	}
}

func TestExecutor_MemoryGuard(t *testing.T) {
	bigResult := strings.Repeat("x", maxResultSize+100)
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult(bigResult)

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "memory-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	text := result.Content[0].Text
	if len(text) > maxResultSize+100 {
		t.Error("output should not exceed reasonable size")
	}
}

func TestExecutor_EmptyWorkflow(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "empty-skill",
		Description: "test",
		Workflow:    []WorkflowStep{},
	}

	_, err := exec.Execute(context.Background(), skill, nil)
	if err == nil {
		t.Fatal("expected error for empty workflow")
	}
	if !strings.Contains(err.Error(), "no workflow steps") {
		t.Errorf("expected 'no workflow steps' error, got: %v", err)
	}
}

func TestExecutor_CircularDependency(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "cycle-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	ctx := withCallStack(context.Background(), []string{"cycle-skill"})

	_, err := exec.Execute(ctx, skill, nil)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("expected circular dependency error, got: %v", err)
	}
}

func TestExecutor_DepthLimitExceeded(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	exec.maxDepth = 3
	skill := &AgentSkill{
		Name:        "deep-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	ctx := withCallStack(context.Background(), []string{"skill-a", "skill-b", "skill-c"})

	_, err := exec.Execute(ctx, skill, nil)
	if err == nil {
		t.Fatal("expected depth exceeded error")
	}
	if !strings.Contains(err.Error(), "max workflow depth") {
		t.Errorf("expected depth exceeded error, got: %v", err)
	}
}

func TestExecutor_NormalNestedCall(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "nested-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	ctx := withCallStack(context.Background(), []string{"parent-skill"})

	result, err := exec.Execute(ctx, skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result for normal nested call")
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "cancel-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := exec.Execute(ctx, skill, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

func TestExecutor_ToolErrorFlag(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = errorResult("tool error message")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "tool-error-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when tool returns isError")
	}
}

func TestExecutor_OnErrorContinueStoresError(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("tool failed")
	caller.results["server__tool-b"] = textResult("b-done")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "continue-error-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "continue"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}, Args: map[string]any{
				"prev_error": "{{ steps.step-a.is_error }}",
			}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result with continue policy")
	}
	calls := caller.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Arguments["prev_error"] != true {
		t.Errorf("expected is_error=true, got: %v", calls[1].Arguments["prev_error"])
	}
}

func TestExecutor_DefaultOnErrorIsFail(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("boom")

	exec := NewExecutor(caller, nil)
	// step-b depends on step-a, putting it in a later DAG level
	skill := &AgentSkill{
		Name:        "default-error-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"}, // on_error not set, defaults to "fail"
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result with default fail policy")
	}
	if caller.callCount() != 1 {
		t.Errorf("expected only 1 call, got %d", caller.callCount())
	}
}

// --- Parallel execution tests ---

func TestExecutor_ParallelSameLevel(t *testing.T) {
	// Two steps with no dependencies execute in parallel
	caller := newTimedMockCaller()
	caller.delays["server__tool-a"] = 50 * time.Millisecond
	caller.delays["server__tool-b"] = 50 * time.Millisecond
	caller.results["server__tool-a"] = textResult("a-result")
	caller.results["server__tool-b"] = textResult("b-result")

	exec := NewExecutor(caller, nil, WithMaxParallel(4))
	skill := &AgentSkill{
		Name:        "parallel-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	start := time.Now()
	result, err := exec.Execute(context.Background(), skill, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	// Parallel: should complete in ~50ms, not ~100ms
	if elapsed > 150*time.Millisecond {
		t.Errorf("expected parallel execution (~50ms), got %v", elapsed)
	}
	// Both should have completed
	order := caller.getOrder()
	if len(order) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(order))
	}
}

func TestExecutor_FanOut(t *testing.T) {
	// A -> [B, C, D] (3 parallel after single setup)
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("setup")
	caller.results["server__tool-b"] = textResult("b")
	caller.results["server__tool-c"] = textResult("c")
	caller.results["server__tool-d"] = textResult("d")

	exec := NewExecutor(caller, nil, WithMaxParallel(4))
	skill := &AgentSkill{
		Name:        "fanout-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-c", Tool: "server__tool-c", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-d", Tool: "server__tool-d", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if caller.callCount() != 4 {
		t.Fatalf("expected 4 calls, got %d", caller.callCount())
	}

	// step-a must be first
	calls := caller.getCalls()
	if calls[0].Name != "server__tool-a" {
		t.Errorf("expected step-a first, got %s", calls[0].Name)
	}
}

func TestExecutor_FanIn(t *testing.T) {
	// [A, B] -> C (merge step after 2 parallel)
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("a-result")
	caller.results["server__tool-b"] = textResult("b-result")
	caller.results["server__tool-c"] = textResult("merged")

	exec := NewExecutor(caller, nil, WithMaxParallel(4))
	skill := &AgentSkill{
		Name:        "fanin-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
			{ID: "step-c", Tool: "server__tool-c", DependsOn: StringOrSlice{"step-a", "step-b"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if caller.callCount() != 3 {
		t.Fatalf("expected 3 calls, got %d", caller.callCount())
	}

	// step-c must be last
	calls := caller.getCalls()
	if calls[2].Name != "server__tool-c" {
		t.Errorf("expected step-c last, got %s", calls[2].Name)
	}
}

func TestExecutor_DiamondPattern(t *testing.T) {
	// A -> [B, C] -> D
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("a")
	caller.results["server__tool-b"] = textResult("b")
	caller.results["server__tool-c"] = textResult("c")
	caller.results["server__tool-d"] = textResult("d")

	exec := NewExecutor(caller, nil, WithMaxParallel(4))
	skill := &AgentSkill{
		Name:        "diamond-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-c", Tool: "server__tool-c", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-d", Tool: "server__tool-d", DependsOn: StringOrSlice{"step-b", "step-c"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	if caller.callCount() != 4 {
		t.Fatalf("expected 4 calls, got %d", caller.callCount())
	}

	// Verify ordering constraints
	calls := caller.getCalls()
	callOrder := make(map[string]int)
	for i, c := range calls {
		callOrder[c.Name] = i
	}
	if callOrder["server__tool-a"] >= callOrder["server__tool-b"] {
		t.Error("A should execute before B")
	}
	if callOrder["server__tool-a"] >= callOrder["server__tool-c"] {
		t.Error("A should execute before C")
	}
	if callOrder["server__tool-b"] >= callOrder["server__tool-d"] {
		t.Error("B should execute before D")
	}
	if callOrder["server__tool-c"] >= callOrder["server__tool-d"] {
		t.Error("C should execute before D")
	}
}

func TestExecutor_MaxParallelLimit(t *testing.T) {
	// With maxParallel=1, steps in same level execute sequentially
	caller := newTimedMockCaller()
	caller.delays["server__tool-a"] = 30 * time.Millisecond
	caller.delays["server__tool-b"] = 30 * time.Millisecond
	caller.results["server__tool-a"] = textResult("a")
	caller.results["server__tool-b"] = textResult("b")

	exec := NewExecutor(caller, nil, WithMaxParallel(1))
	skill := &AgentSkill{
		Name:        "serial-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	start := time.Now()
	result, err := exec.Execute(context.Background(), skill, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	// Sequential: should take at least 60ms (30+30)
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected sequential execution (>50ms), got %v", elapsed)
	}
}

func TestExecutor_StepTimeout(t *testing.T) {
	caller := newTimedMockCaller()
	caller.delays["server__tool-a"] = 500 * time.Millisecond
	caller.results["server__tool-a"] = textResult("slow")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "timeout-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Timeout: "50ms"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for timed out step")
	}
	if !strings.Contains(result.Content[0].Text, "timed out") {
		t.Errorf("expected timeout error message, got: %s", result.Content[0].Text)
	}
}

func TestExecutor_StepRetrySuccess(t *testing.T) {
	// Fails twice, succeeds on third attempt
	callCount := 0
	var mu sync.Mutex
	caller := &funcToolCaller{fn: func(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()
		if n <= 2 {
			return nil, fmt.Errorf("transient error %d", n)
		}
		return textResult("success"), nil
	}}

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "retry-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Retry: &RetryPolicy{
				MaxAttempts: 3,
				Backoff:     "1ms",
			}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success after retries, got error: %s", result.Content[0].Text)
	}
	mu.Lock()
	if callCount != 3 {
		t.Errorf("expected 3 attempts, got %d", callCount)
	}
	mu.Unlock()
}

func TestExecutor_StepRetryExhausted(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("persistent error")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "retry-exhaust-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Retry: &RetryPolicy{
				MaxAttempts: 3,
				Backoff:     "1ms",
			}, OnError: "fail"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error after exhausted retries")
	}
	if !strings.Contains(result.Content[0].Text, "failed after 3 attempts") {
		t.Errorf("expected retry exhaustion message, got: %s", result.Content[0].Text)
	}
	// Should have been called 3 times
	if caller.callCount() != 3 {
		t.Errorf("expected 3 calls, got %d", caller.callCount())
	}
}

func TestExecutor_SkipPropagation(t *testing.T) {
	// A(skip) -> B -> C should skip B and C
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("failed")
	caller.results["server__tool-d"] = textResult("d-result")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "skip-prop-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "skip"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
			{ID: "step-c", Tool: "server__tool-c", DependsOn: StringOrSlice{"step-b"}},
			{ID: "step-d", Tool: "server__tool-d"}, // independent, should still run
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	// Only step-a and step-d should be called; step-b and step-c are skipped
	if caller.callCount() != 2 {
		t.Errorf("expected 2 calls (step-a, step-d), got %d", caller.callCount())
	}
}

func TestExecutor_ContinuePropagation(t *testing.T) {
	// step-a fails with continue, step-b (downstream) still executes
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("non-fatal error")
	caller.results["server__tool-b"] = textResult("b-ok")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "continue-prop-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "continue"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"},
				Condition: "{{ steps.step-a.is_error == true }}",
				Args: map[string]any{
					"error_msg": "{{ steps.step-a.result }}",
				}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	// Both should have been called
	if caller.callCount() != 2 {
		t.Errorf("expected 2 calls, got %d", caller.callCount())
	}
	// step-b should have received the error info
	calls := caller.getCalls()
	if len(calls) < 2 {
		t.Fatal("not enough calls")
	}
	if !strings.Contains(fmt.Sprintf("%v", calls[1].Arguments["error_msg"]), "non-fatal error") {
		t.Errorf("expected error message in args, got: %v", calls[1].Arguments["error_msg"])
	}
}

func TestExecutor_WorkflowTimeout(t *testing.T) {
	caller := newTimedMockCaller()
	caller.delays["server__tool-a"] = 500 * time.Millisecond
	caller.results["server__tool-a"] = textResult("slow")

	exec := NewExecutor(caller, nil, WithWorkflowTimeout(50*time.Millisecond))
	skill := &AgentSkill{
		Name:        "wf-timeout-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	// Should fail with timeout error
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for workflow timeout")
	}
}

func TestExecutor_ContextCancellationStopsExecution(t *testing.T) {
	caller := newTimedMockCaller()
	caller.delays["server__tool-a"] = 50 * time.Millisecond
	caller.delays["server__tool-b"] = 50 * time.Millisecond
	caller.results["server__tool-a"] = textResult("a")
	caller.results["server__tool-b"] = textResult("b")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "cancel-exec-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, err := exec.Execute(ctx, skill, nil)
	// Context cancellation may surface as a Go error or a failed result
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for cancelled context")
	}
	// step-b should not have been called since step-a failed from timeout
	order := caller.getOrder()
	if len(order) > 1 {
		t.Errorf("expected at most 1 call, got %d", len(order))
	}
}

func TestExecutor_StepExecutionResultFields(t *testing.T) {
	// Verify StepExecutionResult has all expected fields
	caller := newMockToolCaller()
	caller.results["server__tool-a"] = textResult("a")
	caller.results["server__tool-b"] = textResult("b")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "fields-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}

	// Verify StepExecutionResult struct includes all new fields
	_ = StepExecutionResult{
		ID:         "test",
		Tool:       "test-tool",
		Status:     "success",
		StartedAt:  time.Now(),
		DurationMs: 100,
		Attempts:   1,
		SkipReason: "",
		Level:      0,
	}
}

func TestExecutor_WithOptions(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil,
		WithMaxParallel(2),
		WithMaxResultSize(500),
		WithWorkflowTimeout(10*time.Second),
	)

	if exec.maxParallel != 2 {
		t.Errorf("expected maxParallel=2, got %d", exec.maxParallel)
	}
	if exec.maxResultSize != 500 {
		t.Errorf("expected maxResultSize=500, got %d", exec.maxResultSize)
	}
	if exec.workflowTimeout != 10*time.Second {
		t.Errorf("expected workflowTimeout=10s, got %v", exec.workflowTimeout)
	}
}

func TestExecutor_InvalidStepTimeout(t *testing.T) {
	caller := newMockToolCaller()
	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "bad-timeout-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", Timeout: "not-a-duration"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(result.Content[0].Text, "invalid timeout") {
		t.Errorf("expected invalid timeout error, got: %s", result.Content[0].Text)
	}
}

func TestToMCPTool(t *testing.T) {
	skill := &AgentSkill{
		Name:        "test-tool",
		Description: "A test tool",
		Inputs: map[string]SkillInput{
			"name":  {Type: "string", Description: "The name", Required: true},
			"count": {Type: "number", Default: 5},
			"mode":  {Type: "string", Enum: []string{"fast", "slow"}},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	tool := skill.ToMCPTool()
	if tool.Name != "test-tool" {
		t.Errorf("Name = %q, want %q", tool.Name, "test-tool")
	}
	if tool.Description != "A test tool" {
		t.Errorf("Description = %q, want %q", tool.Description, "A test tool")
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want 'object'", schema["type"])
	}

	props := schema["properties"].(map[string]any)
	if len(props) != 3 {
		t.Errorf("expected 3 properties, got %d", len(props))
	}

	nameProp := props["name"].(map[string]any)
	if nameProp["type"] != "string" {
		t.Errorf("name type = %v, want 'string'", nameProp["type"])
	}
	if nameProp["description"] != "The name" {
		t.Errorf("name description = %v", nameProp["description"])
	}

	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("required = %v, want ['name']", required)
	}

	modeProp := props["mode"].(map[string]any)
	enumVals := modeProp["enum"].([]any)
	if len(enumVals) != 2 {
		t.Errorf("expected 2 enum values, got %d", len(enumVals))
	}
}

func TestToMCPTool_NoInputs(t *testing.T) {
	skill := &AgentSkill{
		Name:        "no-inputs",
		Description: "No inputs",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
		},
	}

	tool := skill.ToMCPTool()
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want 'object'", schema["type"])
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 0 {
		t.Errorf("expected 0 properties, got %d", len(props))
	}
	if _, ok := schema["required"]; ok {
		t.Error("expected no 'required' field when there are no required inputs")
	}
}

// funcToolCaller wraps a function to implement ToolCaller.
type funcToolCaller struct {
	fn func(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error)
}

func (f *funcToolCaller) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	return f.fn(ctx, name, args)
}
