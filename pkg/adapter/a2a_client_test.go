package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/a2a"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// writeJSON encodes v as JSON to w, failing the test on error.
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to encode JSON response: %v", err)
	}
}

// readJSON decodes the request body into v, returning false on error.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

// mockA2AServer creates an httptest server that responds to A2A protocol requests.
// The card is returned for GET /.well-known/agent.json requests.
// POST requests are handled as JSON-RPC message/send, returning a completed task.
func mockA2AServer(t *testing.T, card a2a.AgentCard) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent.json" && r.Method == http.MethodGet:
			writeJSON(t, w, card)

		case r.Method == http.MethodPost:
			var req map[string]any
			if !readJSON(w, r, &req) {
				return
			}

			method, _ := req["method"].(string)
			switch method {
			case "message/send":
				writeJSON(t, w, map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"result": a2a.SendMessageResult{
						Task: &a2a.Task{
							ID:     "task-1",
							Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
							Messages: []a2a.Message{
								{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("done")}},
							},
						},
					},
				})

			case "tasks/get":
				writeJSON(t, w, map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"result": a2a.Task{
						ID:     "task-1",
						Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
						Messages: []a2a.Message{
							{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("done")}},
						},
					},
				})

			default:
				writeJSON(t, w, map[string]any{
					"jsonrpc": "2.0",
					"id":      req["id"],
					"error":   map[string]any{"code": -32601, "message": "method not found"},
				})
			}

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

// --- skillsToTools tests ---

func TestSkillsToTools(t *testing.T) {
	skills := []a2a.Skill{
		{ID: "code-review", Name: "Code Review", Description: "Reviews code"},
		{ID: "summarize", Name: "Summarize", Description: "Summarizes text"},
	}
	tools := skillsToTools(skills)

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "code-review" {
		t.Errorf("expected tool name 'code-review', got %q", tools[0].Name)
	}
	if tools[0].Title != "Code Review" {
		t.Errorf("expected tool title 'Code Review', got %q", tools[0].Title)
	}
	if tools[0].Description != "Reviews code" {
		t.Errorf("expected description 'Reviews code', got %q", tools[0].Description)
	}
	if tools[1].Name != "summarize" {
		t.Errorf("expected tool name 'summarize', got %q", tools[1].Name)
	}

	// Verify input schema has message property with required
	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil {
		t.Fatalf("failed to unmarshal input schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected 'properties' in schema")
	}
	if _, ok := props["message"]; !ok {
		t.Error("expected 'message' property in input schema")
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected 'required' array in schema")
	}
	if len(required) != 1 || required[0] != "message" {
		t.Errorf("expected required=['message'], got %v", required)
	}
}

func TestSkillsToTools_Empty(t *testing.T) {
	tools := skillsToTools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for nil skills, got %d", len(tools))
	}

	tools = skillsToTools([]a2a.Skill{})
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for empty skills, got %d", len(tools))
	}
}

// --- NewA2AClientAdapter tests ---

func TestNewA2AClientAdapter(t *testing.T) {
	adapter := NewA2AClientAdapter("test-agent", "http://localhost:9000")

	if adapter.Name() != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", adapter.Name())
	}
	if adapter.IsInitialized() {
		t.Error("expected not initialized before Initialize()")
	}
	if len(adapter.Tools()) != 0 {
		t.Errorf("expected 0 tools before init, got %d", len(adapter.Tools()))
	}
}

// --- Initialize tests ---

func TestA2AClientAdapter_Initialize(t *testing.T) {
	card := a2a.AgentCard{
		Name:    "test-agent",
		Version: "1.0.0",
		Skills: []a2a.Skill{
			{ID: "skill-1", Name: "Skill 1", Description: "Test skill"},
		},
	}
	server := mockA2AServer(t, card)
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if !adapter.IsInitialized() {
		t.Error("expected initialized after Initialize()")
	}
	if adapter.ServerInfo().Name != "test-agent" {
		t.Errorf("expected server name 'test-agent', got %q", adapter.ServerInfo().Name)
	}
	if adapter.ServerInfo().Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", adapter.ServerInfo().Version)
	}
	if len(adapter.Tools()) != 1 {
		t.Errorf("expected 1 tool, got %d", len(adapter.Tools()))
	}
	if adapter.Tools()[0].Name != "skill-1" {
		t.Errorf("expected tool name 'skill-1', got %q", adapter.Tools()[0].Name)
	}
}

func TestA2AClientAdapter_Initialize_MultipleSkills(t *testing.T) {
	card := a2a.AgentCard{
		Name:    "multi-agent",
		Version: "2.0.0",
		Skills: []a2a.Skill{
			{ID: "s1", Name: "S1", Description: "Skill 1"},
			{ID: "s2", Name: "S2", Description: "Skill 2"},
			{ID: "s3", Name: "S3", Description: "Skill 3"},
		},
	}
	server := mockA2AServer(t, card)
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if len(adapter.Tools()) != 3 {
		t.Errorf("expected 3 tools, got %d", len(adapter.Tools()))
	}
}

func TestA2AClientAdapter_Initialize_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	err := adapter.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from Initialize with failing server")
	}
	if !strings.Contains(err.Error(), "fetching agent card") {
		t.Errorf("expected 'fetching agent card' in error, got: %v", err)
	}
	if adapter.IsInitialized() {
		t.Error("expected not initialized after failed Initialize()")
	}
}

func TestA2AClientAdapter_Initialize_Unreachable(t *testing.T) {
	adapter := NewA2AClientAdapter("test", "http://127.0.0.1:1")
	err := adapter.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from Initialize with unreachable server")
	}
}

// --- InitializeFromSkills tests ---

func TestA2AClientAdapter_InitializeFromSkills(t *testing.T) {
	adapter := NewA2AClientAdapter("test", "http://unused")
	skills := []a2a.Skill{
		{ID: "s1", Name: "S1", Description: "Skill 1"},
		{ID: "s2", Name: "S2", Description: "Skill 2"},
	}
	adapter.InitializeFromSkills("2.0.0", skills)

	if !adapter.IsInitialized() {
		t.Error("expected initialized after InitializeFromSkills()")
	}
	if len(adapter.Tools()) != 2 {
		t.Errorf("expected 2 tools, got %d", len(adapter.Tools()))
	}
	if adapter.ServerInfo().Name != "test" {
		t.Errorf("expected server name 'test' (adapter name), got %q", adapter.ServerInfo().Name)
	}
	if adapter.ServerInfo().Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", adapter.ServerInfo().Version)
	}
}

func TestA2AClientAdapter_InitializeFromSkills_Empty(t *testing.T) {
	adapter := NewA2AClientAdapter("test", "http://unused")
	adapter.InitializeFromSkills("1.0.0", nil)

	if !adapter.IsInitialized() {
		t.Error("expected initialized even with nil skills")
	}
	if len(adapter.Tools()) != 0 {
		t.Errorf("expected 0 tools, got %d", len(adapter.Tools()))
	}
}

// --- RefreshTools tests ---

func TestA2AClientAdapter_RefreshTools(t *testing.T) {
	// Start with one skill, then update the card to have three
	initialSkills := []a2a.Skill{
		{ID: "skill-a", Name: "Skill A"},
	}
	updatedSkills := []a2a.Skill{
		{ID: "skill-a", Name: "Skill A"},
		{ID: "skill-b", Name: "Skill B"},
		{ID: "skill-c", Name: "Skill C"},
	}

	var useUpdated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			card := a2a.AgentCard{Name: "test-agent", Version: "1.0.0"}
			if useUpdated.Load() {
				card.Skills = updatedSkills
			} else {
				card.Skills = initialSkills
			}
			writeJSON(t, w, card)
		}
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if len(adapter.Tools()) != 1 {
		t.Fatalf("expected 1 tool after init, got %d", len(adapter.Tools()))
	}
	if adapter.Tools()[0].Name != "skill-a" {
		t.Errorf("expected tool name 'skill-a', got %q", adapter.Tools()[0].Name)
	}

	// Update the server to return more skills
	useUpdated.Store(true)
	if err := adapter.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}
	if len(adapter.Tools()) != 3 {
		t.Fatalf("expected 3 tools after refresh, got %d", len(adapter.Tools()))
	}
	if adapter.Tools()[2].Name != "skill-c" {
		t.Errorf("expected third tool name 'skill-c', got %q", adapter.Tools()[2].Name)
	}
}

func TestA2AClientAdapter_RefreshTools_ServerError(t *testing.T) {
	card := a2a.AgentCard{Name: "test", Skills: []a2a.Skill{{ID: "s1"}}}
	server := mockA2AServer(t, card)

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Close server to make refresh fail
	server.Close()
	err := adapter.RefreshTools(context.Background())
	if err == nil {
		t.Fatal("expected error from RefreshTools with closed server")
	}
	if !strings.Contains(err.Error(), "refreshing tools") {
		t.Errorf("expected 'refreshing tools' in error, got: %v", err)
	}

	// Tools should remain unchanged after failed refresh
	if len(adapter.Tools()) != 1 {
		t.Errorf("expected tools unchanged after failed refresh, got %d", len(adapter.Tools()))
	}
}

// --- CallTool tests ---

func TestA2AClientAdapter_CallTool(t *testing.T) {
	card := a2a.AgentCard{
		Name:   "test",
		Skills: []a2a.Skill{{ID: "s1", Name: "S1", Description: "Skill 1"}},
	}
	server := mockA2AServer(t, card)
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := adapter.CallTool(context.Background(), "s1", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Error("expected success result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	if result.Content[0].Text != "done" {
		t.Errorf("expected content text 'done', got %q", result.Content[0].Text)
	}
}

func TestA2AClientAdapter_CallTool_NilArguments(t *testing.T) {
	card := a2a.AgentCard{
		Name:   "test",
		Skills: []a2a.Skill{{ID: "s1"}},
	}
	server := mockA2AServer(t, card)
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := adapter.CallTool(context.Background(), "s1", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Error("expected success result with nil arguments")
	}
}

func TestA2AClientAdapter_CallTool_ServerError(t *testing.T) {
	// Server that returns JSON-RPC errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			writeJSON(t, w, a2a.AgentCard{Name: "test", Skills: []a2a.Skill{{ID: "s1"}}})
			return
		}
		var req map[string]any
		if !readJSON(w, r, &req) {
			return
		}
		writeJSON(t, w, map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"error":   map[string]any{"code": -32000, "message": "skill execution failed"},
		})
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// CallTool returns error as result content, not as Go error
	result, err := adapter.CallTool(context.Background(), "s1", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool returned unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result from failing server")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected error content")
	}
	if !strings.Contains(result.Content[0].Text, "Error") {
		t.Errorf("expected error message in content, got %q", result.Content[0].Text)
	}
}

func TestA2AClientAdapter_CallTool_AsyncTask(t *testing.T) {
	// Server returns working task first, then completed on tasks/get
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			writeJSON(t, w, a2a.AgentCard{Name: "test", Skills: []a2a.Skill{{ID: "s1"}}})
			return
		}

		var req map[string]any
		if !readJSON(w, r, &req) {
			return
		}
		method, _ := req["method"].(string)

		switch method {
		case "message/send":
			writeJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": a2a.SendMessageResult{
					Task: &a2a.Task{
						ID:     "async-task-1",
						Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
					},
				},
			})

		case "tasks/get":
			count := callCount.Add(1)
			var state a2a.TaskState
			var messages []a2a.Message
			if count >= 2 {
				state = a2a.TaskStateCompleted
				messages = []a2a.Message{
					{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("async result")}},
				}
			} else {
				state = a2a.TaskStateWorking
			}
			writeJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": a2a.Task{
					ID:       "async-task-1",
					Status:   a2a.TaskStatus{State: state},
					Messages: messages,
				},
			})
		}
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := adapter.CallTool(context.Background(), "s1", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Error("expected success result for async task")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content from completed async task")
	}
	if result.Content[0].Text != "async result" {
		t.Errorf("expected 'async result', got %q", result.Content[0].Text)
	}
}

// --- a2aResultToMCPResult tests ---

func TestA2AResultToMCPResult_Completed(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Messages: []a2a.Message{
				{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("output")}},
			},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected success result")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "output" {
		t.Errorf("expected 'output', got %q", mcpResult.Content[0].Text)
	}
}

func TestA2AResultToMCPResult_Failed(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateFailed, Message: "boom"},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if !mcpResult.IsError {
		t.Error("expected error result")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "boom" {
		t.Errorf("expected error message 'boom', got %q", mcpResult.Content[0].Text)
	}
}

func TestA2AResultToMCPResult_Empty(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected success result")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 default content, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "Task completed" {
		t.Errorf("expected 'Task completed', got %q", mcpResult.Content[0].Text)
	}
}

func TestA2AResultToMCPResult_NilTask(t *testing.T) {
	result := &a2a.SendMessageResult{}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected success result for nil task")
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 default content, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "Task completed" {
		t.Errorf("expected 'Task completed', got %q", mcpResult.Content[0].Text)
	}
}

func TestA2AResultToMCPResult_MultipleMessages(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Messages: []a2a.Message{
				{Role: a2a.RoleUser, Parts: []a2a.Part{a2a.NewTextPart("user input")}},
				{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("response 1")}},
				{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("response 2")}},
			},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected success result")
	}
	// Only agent messages should be extracted (not user messages)
	if len(mcpResult.Content) != 2 {
		t.Fatalf("expected 2 agent contents, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "response 1" {
		t.Errorf("expected 'response 1', got %q", mcpResult.Content[0].Text)
	}
	if mcpResult.Content[1].Text != "response 2" {
		t.Errorf("expected 'response 2', got %q", mcpResult.Content[1].Text)
	}
}

func TestA2AResultToMCPResult_WithArtifacts(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Messages: []a2a.Message{
				{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("message text")}},
			},
			Artifacts: []a2a.Artifact{
				{
					ID:    "art-1",
					Parts: []a2a.Part{a2a.NewTextPart("artifact text")},
				},
			},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected success result")
	}
	// Should include both message content and artifact content
	if len(mcpResult.Content) != 2 {
		t.Fatalf("expected 2 contents (message + artifact), got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "message text" {
		t.Errorf("expected 'message text', got %q", mcpResult.Content[0].Text)
	}
	if mcpResult.Content[1].Text != "artifact text" {
		t.Errorf("expected 'artifact text', got %q", mcpResult.Content[1].Text)
	}
}

func TestA2AResultToMCPResult_NonTextPartsSkipped(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Messages: []a2a.Message{
				{
					Role: a2a.RoleAgent,
					Parts: []a2a.Part{
						{Type: a2a.PartTypeFile, File: &a2a.FilePart{Name: "test.txt"}},
						a2a.NewTextPart("text content"),
					},
				},
			},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	// Only text parts should be included
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected 1 text content, got %d", len(mcpResult.Content))
	}
	if mcpResult.Content[0].Text != "text content" {
		t.Errorf("expected 'text content', got %q", mcpResult.Content[0].Text)
	}
}

// --- WaitForReady tests ---

func TestA2AClientAdapter_WaitForReady(t *testing.T) {
	card := a2a.AgentCard{Name: "test"}
	server := mockA2AServer(t, card)
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	err := adapter.WaitForReady(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}
}

func TestA2AClientAdapter_WaitForReady_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	err := adapter.WaitForReady(context.Background(), 1*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected 'timeout' in error, got: %v", err)
	}
}

func TestA2AClientAdapter_WaitForReady_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	adapter := NewA2AClientAdapter("test", server.URL)
	err := adapter.WaitForReady(ctx, 10*time.Second)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestA2AClientAdapter_WaitForReady_EventualSuccess(t *testing.T) {
	// Server fails initially, then succeeds
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count < 3 {
			http.Error(w, "starting up", http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, w, a2a.AgentCard{Name: "test"})
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	err := adapter.WaitForReady(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}
}

func TestA2AClientAdapter_CallTool_AsyncTask_GetTaskError(t *testing.T) {
	// Server returns working task, then errors on tasks/get
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent.json" {
			writeJSON(t, w, a2a.AgentCard{Name: "test", Skills: []a2a.Skill{{ID: "s1"}}})
			return
		}

		var req map[string]any
		if !readJSON(w, r, &req) {
			return
		}
		method, _ := req["method"].(string)

		switch method {
		case "message/send":
			writeJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": a2a.SendMessageResult{
					Task: &a2a.Task{
						ID:     "async-task-1",
						Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
					},
				},
			})

		case "tasks/get":
			writeJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"error":   map[string]any{"code": -32001, "message": "task not found"},
			})
		}
	}))
	defer server.Close()

	adapter := NewA2AClientAdapter("test", server.URL)
	if err := adapter.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	result, err := adapter.CallTool(context.Background(), "s1", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool returned unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when GetTask fails during polling")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected error content")
	}
	if !strings.Contains(result.Content[0].Text, "waiting for completion") {
		t.Errorf("expected 'waiting for completion' in error, got %q", result.Content[0].Text)
	}
}

// --- a2aResultToMCPResult terminal state tests ---

func TestA2AResultToMCPResult_Cancelled(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCancelled},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	// Cancelled tasks fall through to message extraction; with no messages, get default
	if mcpResult.IsError {
		t.Error("expected non-error result for cancelled task (only 'failed' sets isError)")
	}
	if len(mcpResult.Content) != 1 || mcpResult.Content[0].Text != "Task completed" {
		t.Errorf("expected default content for cancelled task, got %v", mcpResult.Content)
	}
}

func TestA2AResultToMCPResult_Rejected(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateRejected},
			Messages: []a2a.Message{
				{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.NewTextPart("rejected reason")}},
			},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if mcpResult.IsError {
		t.Error("expected non-error result for rejected task (only 'failed' sets isError)")
	}
	if len(mcpResult.Content) != 1 || mcpResult.Content[0].Text != "rejected reason" {
		t.Errorf("expected 'rejected reason' content, got %v", mcpResult.Content)
	}
}

func TestA2AResultToMCPResult_FailedEmptyMessage(t *testing.T) {
	result := &a2a.SendMessageResult{
		Task: &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateFailed, Message: ""},
		},
	}
	mcpResult := a2aResultToMCPResult(result)
	if !mcpResult.IsError {
		t.Error("expected error result for failed task")
	}
	// Empty error message still produces content (empty string text)
	if len(mcpResult.Content) < 1 {
		t.Fatal("expected at least 1 content entry")
	}
}

// --- Interface compliance ---

// Verify A2AClientAdapter implements mcp.AgentClient at compile time.
var _ mcp.AgentClient = (*A2AClientAdapter)(nil)
