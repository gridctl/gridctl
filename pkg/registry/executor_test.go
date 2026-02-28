package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// mockToolCaller records calls and returns configurable results/errors.
type mockToolCaller struct {
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
	m.calls = append(m.calls, mockCall{Name: name, Arguments: arguments})
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
	if len(caller.calls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(caller.calls))
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
	if len(caller.calls) != 4 {
		t.Fatalf("expected 4 tool calls, got %d", len(caller.calls))
	}

	// Verify execution order: step-a must come before step-b and step-c,
	// step-b and step-c must come before step-d
	callOrder := make(map[string]int)
	for i, c := range caller.calls {
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

	if len(caller.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(caller.calls))
	}
	// The resolved arg should be the default value (5)
	if caller.calls[0].Arguments["n"] != 5 {
		t.Errorf("expected default value 5, got: %v", caller.calls[0].Arguments["n"])
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

	if len(caller.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(caller.calls))
	}
	if caller.calls[0].Arguments["target"] != "10.1.1.1" {
		t.Errorf("expected host to resolve, got: %v", caller.calls[0].Arguments["target"])
	}
	if caller.calls[1].Arguments["status"] != "ok" {
		t.Errorf("expected status 'ok', got: %v", caller.calls[1].Arguments["status"])
	}
}

func TestExecutor_OnErrorFail(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("connection refused")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "fail-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", OnError: "fail"},
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	// step-b should not have been called
	if len(caller.calls) != 1 {
		t.Errorf("expected only 1 call (step-a), got %d", len(caller.calls))
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
	if len(caller.calls) != 2 {
		t.Errorf("expected 2 calls (step-a, step-c), got %d: %v", len(caller.calls), caller.calls)
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
	// Both should have been called
	if len(caller.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(caller.calls))
	}
	// Result should not be an error since step-b succeeded
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
	if len(caller.calls) != 1 {
		t.Errorf("expected 1 call (step-a only), got %d", len(caller.calls))
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
	// Generate a result that exceeds 1MB
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
	// The result in template context should be truncated at maxResultSize
	// but the output should still work
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

	// Simulate a call stack where cycle-skill is already in progress
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

	// Simulate a call stack at max depth
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

	// Simulate a call stack with different skills (no cycle)
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
	if !strings.Contains(err.Error(), "cancelled") {
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
	// Default on_error is "fail", so result should be an error
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
	// step-b should have received the is_error flag from step-a
	if len(caller.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(caller.calls))
	}
	if caller.calls[1].Arguments["prev_error"] != true {
		t.Errorf("expected is_error=true, got: %v", caller.calls[1].Arguments["prev_error"])
	}
}

func TestExecutor_DefaultOnErrorIsFail(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["server__tool-a"] = fmt.Errorf("boom")

	exec := NewExecutor(caller, nil)
	skill := &AgentSkill{
		Name:        "default-error-skill",
		Description: "test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"}, // on_error not set, defaults to "fail"
			{ID: "step-b", Tool: "server__tool-b"},
		},
	}

	result, err := exec.Execute(context.Background(), skill, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result with default fail policy")
	}
	if len(caller.calls) != 1 {
		t.Errorf("expected only 1 call, got %d", len(caller.calls))
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

	// Parse the schema
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

	// Check name property
	nameProp := props["name"].(map[string]any)
	if nameProp["type"] != "string" {
		t.Errorf("name type = %v, want 'string'", nameProp["type"])
	}
	if nameProp["description"] != "The name" {
		t.Errorf("name description = %v", nameProp["description"])
	}

	// Check required
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("required = %v, want ['name']", required)
	}

	// Check enum
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
