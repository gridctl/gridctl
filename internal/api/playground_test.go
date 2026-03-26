package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime/agent"
)

// --- inferProviderFromModel ---

func TestInferProviderFromModel(t *testing.T) {
	cases := []struct {
		model    string
		expected string
	}{
		{"claude-3-5-sonnet-latest", "anthropic"},
		{"claude-opus-4", "anthropic"},
		{"gpt-4o", "openai"},
		{"gpt-3.5-turbo", "openai"},
		{"o1-mini", "openai"},
		{"o3-mini", "openai"},
		{"o4-preview", "openai"},
		{"gemini-1.5-pro", "gemini"},
		{"llama3", "anthropic"}, // unknown defaults to anthropic
		{"phi3", "anthropic"},
	}
	for _, tc := range cases {
		got := inferProviderFromModel(tc.model)
		if got != tc.expected {
			t.Errorf("inferProviderFromModel(%q) = %q; want %q", tc.model, got, tc.expected)
		}
	}
}

// --- extractTextFromContent ---

func TestExtractTextFromContent(t *testing.T) {
	cases := []struct {
		name     string
		content  []mcp.Content
		expected string
	}{
		{
			name:     "empty",
			content:  nil,
			expected: "",
		},
		{
			name:     "single text block",
			content:  []mcp.Content{{Text: "hello"}},
			expected: "hello",
		},
		{
			name:     "multiple text blocks joined with newline",
			content:  []mcp.Content{{Text: "line1"}, {Text: "line2"}},
			expected: "line1\nline2",
		},
		{
			name:     "empty text blocks skipped",
			content:  []mcp.Content{{Text: ""}, {Text: "real"}, {Text: ""}},
			expected: "real",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTextFromContent(tc.content)
			if got != tc.expected {
				t.Errorf("got %q; want %q", got, tc.expected)
			}
		})
	}
}

// --- handlePlayground routing ---

func TestHandlePlayground_UnknownPath(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playground/unknown", nil)
	rec := httptest.NewRecorder()
	srv.handlePlayground(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandlePlayground_RoutesAuth(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/playground/auth", nil)
	rec := httptest.NewRecorder()
	srv.handlePlayground(rec, req)
	// handlePlaygroundAuth runs; any 2xx or specific error is fine — we just check routing works
	if rec.Code == http.StatusNotFound {
		t.Error("auth path should not return 404")
	}
}

// --- handlePlaygroundAuth ---

func TestHandlePlaygroundAuth_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playground/auth", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundAuth(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandlePlaygroundAuth_NoVaultNoEnv(t *testing.T) {
	srv := newTestServer(t)
	// Without vault and without env vars, all API key providers should be unavailable.
	// Ollama will also be unreachable in CI, and claude CLI likely not present.
	req := httptest.NewRequest(http.MethodPost, "/api/playground/auth", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundAuth(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp playgroundAuthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Providers == nil {
		t.Fatal("providers map should not be nil")
	}
	// Anthropic should be unavailable (no key)
	if p, ok := resp.Providers["anthropic"]; !ok || p.Available {
		t.Errorf("anthropic should be unavailable without env var; got %+v", resp.Providers["anthropic"])
	}
}

// --- handlePlaygroundChat ---

func TestHandlePlaygroundChat_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playground/chat", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandlePlaygroundChat_MissingSessionID(t *testing.T) {
	srv := newTestServer(t)
	body := `{"message":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePlaygroundChat_MissingMessage(t *testing.T) {
	srv := newTestServer(t)
	body := `{"sessionId":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePlaygroundChat_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePlaygroundChat_NoAPIKey(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"sessionId": "sess-2",
		"message":   "hello",
		"authMode":  string(agent.AuthModeAPIKey),
		"model":     "claude-3-5-sonnet-latest",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	// Expect 400 (no API key in vault or env) or 500 (agent resolution error).
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 400 or 500, got %d", rec.Code)
	}
}

func TestHandlePlaygroundChat_UnsupportedAuthMode(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"sessionId": "sess-3",
		"message":   "hello",
		"authMode":  "CLI_PROXY", // not yet supported in buildLLMClient
		"model":     "claude-3-5-sonnet-latest",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported auth mode, got %d", rec.Code)
	}
}

// --- handlePlaygroundStream ---

func TestHandlePlaygroundStream_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/playground/stream", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundStream(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandlePlaygroundStream_MissingSessionID(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playground/stream", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePlaygroundStream_ClientDisconnect(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel to simulate immediate disconnect
	req := httptest.NewRequest(http.MethodGet, "/api/playground/stream?sessionId=sess-stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	// httptest.ResponseRecorder implements http.Flusher, so SSE setup proceeds.
	// The cancelled context causes the select to immediately exit.
	srv.handlePlaygroundStream(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (SSE headers sent before disconnect), got %d", rec.Code)
	}
}

// --- handlePlaygroundSessionDelete ---

func TestHandlePlaygroundSessionDelete_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/playground/session/abc", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundSessionDelete(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandlePlaygroundSessionDelete_MissingID(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/playground/session/", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundSessionDelete(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePlaygroundSessionDelete_ValidID(t *testing.T) {
	srv := newTestServer(t)
	// Pre-create a session so Delete has something to clean up.
	srv.playgroundSessions.GetOrCreate("to-delete")
	req := httptest.NewRequest(http.MethodDelete, "/api/playground/session/to-delete", nil)
	rec := httptest.NewRecorder()
	srv.handlePlaygroundSessionDelete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	_, ok := srv.playgroundSessions.Get("to-delete")
	if ok {
		t.Error("session should have been deleted")
	}
}

// --- buildLLMClient ---

func TestBuildLLMClient_LocalLLM(t *testing.T) {
	srv := newTestServer(t)
	client, err := srv.buildLLMClient(agent.AuthModeLocalLLM, "llama3", "http://localhost:11434")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	client.Close()
}

func TestBuildLLMClient_UnsupportedMode(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.buildLLMClient("UNKNOWN_MODE", "model", "")
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}

// --- resolveAgentSystemPrompt ---

func TestResolveAgentSystemPrompt_EmptyStackFile(t *testing.T) {
	srv := newTestServer(t)
	// No stack file set — should return empty string without error.
	prompt, err := srv.resolveAgentSystemPrompt("my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

func TestResolveAgentSystemPrompt_EmptyAgentID(t *testing.T) {
	srv := newTestServer(t)
	srv.stackFile = "some/path.yaml"
	prompt, err := srv.resolveAgentSystemPrompt("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt for empty agentID, got %q", prompt)
	}
}

// --- gatewayToolCaller ---

func TestGatewayToolCaller_ImplementsInterface(t *testing.T) {
	// Verify the struct satisfies the ToolCaller interface at compile time.
	var _ agent.ToolCaller = &gatewayToolCaller{gateway: nil}
}

func TestGatewayToolCaller_CallTool_UnknownTool(t *testing.T) {
	gw := mcp.NewGateway()
	caller := &gatewayToolCaller{gateway: gw}
	// With no servers registered, calling any tool returns an error.
	_, err := caller.CallTool(context.Background(), "server__unknown_tool", map[string]any{"x": 1})
	// Either an error is returned OR IsError is set — either way, no panic.
	if err != nil {
		// expected: gateway has no server for this tool
		return
	}
}


// --- handlePlayground session route ---

func TestHandlePlayground_RoutesSessionDelete(t *testing.T) {
	srv := newTestServer(t)
	srv.playgroundSessions.GetOrCreate("route-test")
	req := httptest.NewRequest(http.MethodDelete, "/api/playground/session/route-test", nil)
	rec := httptest.NewRecorder()
	srv.handlePlayground(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

// --- handlePlaygroundChat LocalLLM mode ---

func TestHandlePlaygroundChat_LocalLLM_ReturnsProcessing(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"sessionId": "local-sess",
		"message":   "hello",
		"authMode":  string(agent.AuthModeLocalLLM),
		"model":     "llama3",
		"ollamaUrl": "http://localhost:11434",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlaygroundChat(rec, req)
	// Handler returns 200 with "processing" before the goroutine connects to Ollama.
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "processing" {
		t.Errorf("expected status 'processing', got %q", resp["status"])
	}
}

// --- agentTools ---

func TestAgentTools_EmptyRegistry(t *testing.T) {
	srv := newTestServer(t)
	// gateway is non-nil (set by newTestServer), so HandleToolsListForAgent is called.
	// For an unknown agent, it should return an empty list.
	tools, err := srv.agentTools("nonexistent-agent")
	if err != nil {
		t.Logf("agentTools returned error (acceptable for unknown agent): %v", err)
	}
	_ = tools
}

// --- buildAPIKeyClient ---

func TestBuildAPIKeyClient_OpenAINoKey(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.buildAPIKeyClient("gpt-4o")
	if err == nil {
		t.Fatal("expected error for missing OpenAI key")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("expected OPENAI_API_KEY in error, got: %v", err)
	}
}

func TestBuildAPIKeyClient_GeminiModel(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.buildAPIKeyClient("gemini-1.5-pro")
	// gemini falls through to the default case which returns "cannot determine provider"
	if err == nil {
		t.Fatal("expected error for gemini model (not yet supported)")
	}
}

func TestBuildAPIKeyClient_DefaultModel(t *testing.T) {
	srv := newTestServer(t)
	// Empty model defaults to "claude-3-5-sonnet-latest" → anthropic provider → no key error.
	_, err := srv.buildAPIKeyClient("")
	if err == nil {
		t.Fatal("expected error for missing Anthropic key")
	}
}

// --- agentTools ---

func TestAgentTools_NilGateway(t *testing.T) {
	srv := NewServer(nil, nil)
	tools, err := srv.agentTools("any-agent")
	if err != nil {
		t.Errorf("nil gateway should return nil, nil; got err: %v", err)
	}
	if tools != nil {
		t.Errorf("nil gateway should return nil tools, got %v", tools)
	}
}

// --- resolveAgentSystemPrompt ---

func TestResolveAgentSystemPrompt_InvalidStackFile(t *testing.T) {
	srv := newTestServer(t)
	srv.stackFile = "/nonexistent/path.yaml"
	_, err := srv.resolveAgentSystemPrompt("some-agent")
	if err == nil {
		t.Fatal("expected error for nonexistent stack file")
	}
}

func TestResolveAgentSystemPrompt_AgentFound(t *testing.T) {
	srv := newTestServer(t)
	content := "name: test\nnetwork:\n  name: test-net\nagents:\n  - name: my-agent\n    image: nginx:latest\n    prompt: \"You are helpful\"\n"
	tmpFile := t.TempDir() + "/stack.yaml"
	if err := writeFileForTest(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp stack: %v", err)
	}
	srv.stackFile = tmpFile
	prompt, err := srv.resolveAgentSystemPrompt("my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "You are helpful" {
		t.Errorf("expected prompt 'You are helpful', got %q", prompt)
	}
}

func TestResolveAgentSystemPrompt_AgentEmptyPrompt(t *testing.T) {
	srv := newTestServer(t)
	content := "name: test\nnetwork:\n  name: test-net\nagents:\n  - name: no-prompt-agent\n    image: nginx:latest\n"
	tmpFile := t.TempDir() + "/stack.yaml"
	if err := writeFileForTest(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp stack: %v", err)
	}
	srv.stackFile = tmpFile
	prompt, err := srv.resolveAgentSystemPrompt("no-prompt-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt for agent with no prompt field, got %q", prompt)
	}
}

func TestResolveAgentSystemPrompt_AgentNotFound(t *testing.T) {
	srv := newTestServer(t)
	// Write a minimal valid stack YAML to a temp file.
	content := "name: test\nnetwork:\n  name: test-net\nagents:\n  - name: other-agent\n    image: nginx:latest\n    prompt: hello\n"
	tmpFile := t.TempDir() + "/stack.yaml"
	if err := writeFileForTest(tmpFile, content); err != nil {
		t.Fatalf("failed to write temp stack: %v", err)
	}
	srv.stackFile = tmpFile
	prompt, err := srv.resolveAgentSystemPrompt("missing-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt for unmatched agent, got %q", prompt)
	}
}

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// --- handlePlayground routing coverage ---

func TestHandlePlayground_RoutesChatPost(t *testing.T) {
	srv := newTestServer(t)
	body := `{"sessionId":"r","message":"hi","authMode":"LOCAL_LLM","model":"llama3"}`
	req := httptest.NewRequest(http.MethodPost, "/api/playground/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handlePlayground(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Error("chat path should not return 404")
	}
}

func TestHandlePlayground_RoutesStreamGet(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/playground/stream?sessionId=x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.handlePlayground(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Error("stream path should not return 404")
	}
}
