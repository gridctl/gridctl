package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// mockToolCaller implements ToolCaller for testing.
type mockToolCaller struct {
	callFn func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error)
}

func (m *mockToolCaller) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	return m.callFn(ctx, name, arguments)
}

func TestTranspile_ValidCode(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"arrow functions", `const add = (a, b) => a + b;`},
		{"template literals", "const msg = `hello ${1+1}`;"},
		{"destructuring", `const { a, b } = { a: 1, b: 2 };`},
		{"let/const", `let x = 1; const y = 2;`},
		{"spread operator", `const arr = [1, ...[2, 3]];`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Transpile(tt.code)
			if err != nil {
				t.Fatalf("Transpile failed: %v", err)
			}
			if result == "" {
				t.Fatal("Transpile returned empty result")
			}
		})
	}
}

func TestTranspile_SyntaxError(t *testing.T) {
	code := `const x = {;`
	_, err := Transpile(code)
	if err == nil {
		t.Fatal("Transpile should have returned error for syntax error")
	}
	if !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("Error should contain 'syntax error', got: %v", err)
	}
}

func TestSearchIndex_MatchByName(t *testing.T) {
	tools := []Tool{
		{Name: "server__get_workflows", Description: "Get workflows"},
		{Name: "server__delete_user", Description: "Delete a user"},
	}
	idx := NewSearchIndex(tools)
	matches := idx.Search("workflow")
	if len(matches) != 1 {
		t.Fatalf("Expected 1 match, got %d", len(matches))
	}
	if matches[0].Name != "server__get_workflows" {
		t.Errorf("Expected 'server__get_workflows', got '%s'", matches[0].Name)
	}
}

func TestSearchIndex_MatchByDescription(t *testing.T) {
	tools := []Tool{
		{Name: "server__tool1", Description: "Manages user accounts"},
		{Name: "server__tool2", Description: "Handles payments"},
	}
	idx := NewSearchIndex(tools)
	matches := idx.Search("payment")
	if len(matches) != 1 {
		t.Fatalf("Expected 1 match, got %d", len(matches))
	}
	if matches[0].Name != "server__tool2" {
		t.Errorf("Expected 'server__tool2', got '%s'", matches[0].Name)
	}
}

func TestSearchIndex_MatchByParameterName(t *testing.T) {
	schema, _ := json.Marshal(InputSchemaObject{
		Type: "object",
		Properties: map[string]Property{
			"workflow_id": {Type: "string", Description: "The workflow ID"},
		},
	})
	tools := []Tool{
		{Name: "server__run_task", Description: "Run a task", InputSchema: schema},
		{Name: "server__get_info", Description: "Get info"},
	}
	idx := NewSearchIndex(tools)
	matches := idx.Search("workflow_id")
	if len(matches) != 1 {
		t.Fatalf("Expected 1 match, got %d", len(matches))
	}
	if matches[0].Name != "server__run_task" {
		t.Errorf("Expected 'server__run_task', got '%s'", matches[0].Name)
	}
}

func TestSearchIndex_EmptyQuery(t *testing.T) {
	tools := []Tool{
		{Name: "tool1"},
		{Name: "tool2"},
	}
	idx := NewSearchIndex(tools)
	matches := idx.Search("")
	if len(matches) != 2 {
		t.Fatalf("Empty query should return all tools, got %d", len(matches))
	}
}

func TestSearchIndex_CaseInsensitive(t *testing.T) {
	tools := []Tool{
		{Name: "server__GetWorkflow", Description: "Get a WORKFLOW"},
	}
	idx := NewSearchIndex(tools)

	matches := idx.Search("getworkflow")
	if len(matches) != 1 {
		t.Error("Search should be case insensitive for names")
	}

	matches = idx.Search("WORKFLOW")
	if len(matches) != 1 {
		t.Error("Search should be case insensitive for descriptions")
	}
}

func TestSandbox_BasicExecution(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{Content: []Content{NewTextContent(`{"status":"ok"}`)}}, nil
		},
	}

	result, err := sandbox.Execute(context.Background(), `1 + 2`, caller, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Value != "3" {
		t.Errorf("Expected '3', got '%s'", result.Value)
	}
}

func TestSandbox_ConsoleCapture(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	result, err := sandbox.Execute(context.Background(), `console.log("hello"); console.warn("warning"); 42`, caller, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result.Console) != 2 {
		t.Fatalf("Expected 2 console entries, got %d", len(result.Console))
	}
	if result.Console[0] != "hello" {
		t.Errorf("Expected 'hello', got '%s'", result.Console[0])
	}
	if result.Console[1] != "warning" {
		t.Errorf("Expected 'warning', got '%s'", result.Console[1])
	}
}

func TestSandbox_ToolCall(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)

	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			if name != "myserver__get_data" {
				t.Errorf("Expected tool name 'myserver__get_data', got '%s'", name)
			}
			return &ToolCallResult{
				Content: []Content{NewTextContent(`{"items": [1, 2, 3]}`)},
			}, nil
		},
	}

	allowedTools := []Tool{
		{Name: "myserver__get_data"},
	}

	code := `var result = mcp.callTool("myserver", "get_data", {}); result.items.length;`
	result, err := sandbox.Execute(context.Background(), code, caller, allowedTools)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Value != "3" {
		t.Errorf("Expected '3', got '%s'", result.Value)
	}
}

func TestSandbox_ACLEnforcement(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			t.Fatal("Tool should not have been called")
			return nil, nil
		},
	}

	// Allow only one tool, but try to call a different one
	allowedTools := []Tool{
		{Name: "server__allowed_tool"},
	}

	code := `mcp.callTool("server", "forbidden_tool", {});`
	_, err := sandbox.Execute(context.Background(), code, caller, allowedTools)
	if err == nil {
		t.Fatal("Expected error for forbidden tool access")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("Expected 'access denied' error, got: %v", err)
	}
}

func TestSandbox_Timeout(t *testing.T) {
	sandbox := NewSandbox(100 * time.Millisecond)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	code := `while(true) {}`
	_, err := sandbox.Execute(context.Background(), code, caller, nil)
	if err == nil {
		t.Fatal("Expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "interrupt") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestSandbox_CodeTooLarge(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	code := strings.Repeat("x", MaxCodeSize+1)
	_, err := sandbox.Execute(context.Background(), code, caller, nil)
	if err == nil {
		t.Fatal("Expected error for oversized code")
	}
	if !strings.Contains(err.Error(), "code too large") {
		t.Errorf("Expected 'code too large' error, got: %v", err)
	}
}

func TestSandbox_ModernJS(t *testing.T) {
	sandbox := NewSandbox(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	// Arrow function + destructuring + template literal
	code := "const add = (a, b) => a + b; const { x, y } = { x: 3, y: 4 }; add(x, y);"
	result, err := sandbox.Execute(context.Background(), code, caller, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Value != "7" {
		t.Errorf("Expected '7', got '%s'", result.Value)
	}
}

func TestCodeMode_ToolsList(t *testing.T) {
	cm := NewCodeMode(30 * time.Second)
	result := cm.ToolsList()

	if len(result.Tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(result.Tools))
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	if !names[MetaToolSearch] {
		t.Error("Missing 'search' meta-tool")
	}
	if !names[MetaToolExecute] {
		t.Error("Missing 'execute' meta-tool")
	}
}

func TestCodeMode_IsMetaTool(t *testing.T) {
	cm := NewCodeMode(30 * time.Second)

	if !cm.IsMetaTool("search") {
		t.Error("'search' should be a meta-tool")
	}
	if !cm.IsMetaTool("execute") {
		t.Error("'execute' should be a meta-tool")
	}
	if cm.IsMetaTool("other") {
		t.Error("'other' should not be a meta-tool")
	}
}

func TestCodeMode_HandleSearch(t *testing.T) {
	cm := NewCodeMode(30 * time.Second)
	tools := []Tool{
		{Name: "server__get_users", Description: "Get all users"},
		{Name: "server__delete_user", Description: "Delete a user"},
		{Name: "server__list_items", Description: "List items"},
	}

	params := ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": "user"},
	}

	result, err := cm.HandleCall(context.Background(), params, nil, tools)
	if err != nil {
		t.Fatalf("HandleCall failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("Unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Found 2 tool(s)") {
		t.Errorf("Expected 2 matches, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`[{"id":1},{"id":2}]`)},
			}, nil
		},
	}

	allowedTools := []Tool{
		{Name: "server__list_items"},
	}

	params := ToolCallParams{
		Name: MetaToolExecute,
		Arguments: map[string]any{
			"code": `var items = mcp.callTool("server", "list_items", {}); items.length;`,
		},
	}

	result, err := cm.HandleCallWithScope(context.Background(), params, caller, allowedTools)
	if err != nil {
		t.Fatalf("HandleCallWithScope failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("Unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "2") {
		t.Errorf("Expected result to contain '2', got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_EmptyCode(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": ""},
	}

	result, err := cm.HandleCall(context.Background(), params, nil, nil)
	if err != nil {
		t.Fatalf("HandleCall failed: %v", err)
	}
	if !result.IsError {
		t.Error("Expected error for empty code")
	}
}

func TestGateway_CodeMode_ToolsList(t *testing.T) {
	gw := NewGateway()
	gw.SetCodeMode(30 * time.Second)

	result, err := gw.HandleToolsList()
	if err != nil {
		t.Fatalf("HandleToolsList failed: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("Expected 2 meta-tools, got %d", len(result.Tools))
	}
}

func TestGateway_CodeMode_Off(t *testing.T) {
	gw := NewGateway()
	// Code mode not enabled — should return aggregated tools as usual

	result, err := gw.HandleToolsList()
	if err != nil {
		t.Fatalf("HandleToolsList failed: %v", err)
	}
	// No tools registered, should return empty
	if len(result.Tools) != 0 {
		t.Fatalf("Expected 0 tools, got %d", len(result.Tools))
	}
}

func TestGateway_CodeModeStatus(t *testing.T) {
	gw := NewGateway()
	if gw.CodeModeStatus() != "off" {
		t.Errorf("Expected 'off', got '%s'", gw.CodeModeStatus())
	}

	gw.SetCodeMode(30 * time.Second)
	if gw.CodeModeStatus() != "on" {
		t.Errorf("Expected 'on', got '%s'", gw.CodeModeStatus())
	}
}
