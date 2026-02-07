package a2a

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL mismatch: got %s, want http://localhost:8080", h.baseURL)
	}
}

func TestHandler_RegisterLocalAgent(t *testing.T) {
	h := NewHandler("http://localhost:8080")

	agent := &LocalAgent{
		Card: AgentCard{
			Name:        "test-agent",
			Description: "Test description",
			Skills:      []Skill{{ID: "skill-1", Name: "Skill One"}},
		},
	}

	h.RegisterLocalAgent("test-agent", agent)

	cards := h.ListLocalAgents()
	if len(cards) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(cards))
	}
	if cards[0].Name != "test-agent" {
		t.Errorf("Name mismatch: got %s, want test-agent", cards[0].Name)
	}
}

func TestHandler_ServeAgentCardList(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("agent-1", &LocalAgent{
		Card: AgentCard{
			Name:   "agent-1",
			Skills: []Skill{{ID: "skill-1", Name: "Skill One"}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	agents, ok := response["agents"]
	if !ok {
		t.Fatal("Expected 'agents' key in response")
	}

	agentList, ok := agents.([]interface{})
	if !ok {
		t.Fatal("Expected agents to be an array")
	}

	if len(agentList) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(agentList))
	}
}

func TestHandler_ServeSpecificAgentCard(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("my-agent", &LocalAgent{
		Card: AgentCard{
			Name:        "my-agent",
			Description: "My agent description",
			Skills: []Skill{
				{ID: "skill-1", Name: "Skill One"},
				{ID: "skill-2", Name: "Skill Two"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/a2a/my-agent", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	var card AgentCard
	if err := json.NewDecoder(rec.Body).Decode(&card); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if card.Name != "my-agent" {
		t.Errorf("Card name mismatch: got %s, want my-agent", card.Name)
	}
	if len(card.Skills) != 2 {
		t.Errorf("Skills count mismatch: got %d, want 2", len(card.Skills))
	}
}

func TestHandler_ServeAgentNotFound(t *testing.T) {
	h := NewHandler("http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/a2a/nonexistent", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandler_MessageSend(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	reqBody := Request{
		JSONRPC: "2.0",
		ID:      mustMarshalRaw("1"),
		Method:  MethodSendMessage,
		Params: mustMarshal(SendMessageParams{
			Message: Message{
				Role: "user",
				Parts: []Part{
					{Text: "Hello"},
				},
			},
		}),
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC version mismatch: got %s, want 2.0", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Error("Expected result, got nil")
	}
}

func TestHandler_TasksGet(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	// First create a task via message/send
	sendReq := Request{
		JSONRPC: "2.0",
		ID:      mustMarshalRaw("1"),
		Method:  MethodSendMessage,
		Params: mustMarshal(SendMessageParams{
			Message: Message{
				Role:  "user",
				Parts: []Part{{Text: "Hello"}},
			},
		}),
	}

	body, _ := json.Marshal(sendReq)
	req := httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Extract task ID from response
	var sendResp struct {
		Result SendMessageResult `json:"result"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&sendResp); err != nil {
		t.Fatalf("Failed to decode send response: %v", err)
	}
	taskID := sendResp.Result.Task.ID

	// Now get the task
	getReq := Request{
		JSONRPC: "2.0",
		ID:      mustMarshalRaw("2"),
		Method:  MethodGetTask,
		Params:  mustMarshal(GetTaskParams{ID: taskID}),
	}

	body, _ = json.Marshal(getReq)
	req = httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	var getResp Response
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode get response: %v", err)
	}

	if getResp.Error != nil {
		t.Errorf("Unexpected error: %v", getResp.Error)
	}
}

func TestHandler_TasksList(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	listReq := Request{
		JSONRPC: "2.0",
		ID:      mustMarshalRaw("1"),
		Method:  MethodListTasks,
		Params:  json.RawMessage(`{}`),
	}

	body, _ := json.Marshal(listReq)
	req := httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusOK)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}
}

func TestHandler_InvalidMethod(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	reqBody := Request{
		JSONRPC: "2.0",
		ID:      mustMarshalRaw("1"),
		Method:  "invalid/method",
		Params:  json.RawMessage(`{}`),
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Error == nil {
		t.Error("Expected error for invalid method")
	}
	if resp.Error != nil && resp.Error.Code != MethodNotFound {
		t.Errorf("Error code mismatch: got %d, want %d", resp.Error.Code, MethodNotFound)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	req := httptest.NewRequest(http.MethodPost, "/a2a/test-agent", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Error == nil {
		t.Error("Expected error for invalid JSON")
	}
	if resp.Error != nil && resp.Error.Code != ParseError {
		t.Errorf("Error code mismatch: got %d, want %d", resp.Error.Code, ParseError)
	}
}

func TestHandler_CORSHandledByMiddleware(t *testing.T) {
	h := NewHandler("http://localhost:8080")

	// CORS is handled by the central corsMiddleware in api.go, not the A2A handler.
	// The handler itself returns 405 for OPTIONS since it doesn't handle that method.
	req := httptest.NewRequest(http.MethodOptions, "/a2a/test-agent", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status code mismatch: got %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandler_UnregisterLocalAgent(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	// Verify registered
	if len(h.ListLocalAgents()) != 1 {
		t.Fatal("Agent not registered")
	}

	h.UnregisterLocalAgent("test-agent")

	// Verify unregistered
	if len(h.ListLocalAgents()) != 0 {
		t.Error("Agent not unregistered")
	}
}

func TestHandler_GetLocalAgent(t *testing.T) {
	h := NewHandler("http://localhost:8080")
	h.RegisterLocalAgent("test-agent", &LocalAgent{
		Card: AgentCard{Name: "test-agent"},
	})

	agent := h.GetLocalAgent("test-agent")
	if agent == nil {
		t.Error("Expected agent, got nil")
	}

	agent = h.GetLocalAgent("nonexistent")
	if agent != nil {
		t.Error("Expected nil for nonexistent agent")
	}
}

// Helper function to marshal to json.RawMessage
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// Helper to create a json.RawMessage pointer for ID
func mustMarshalRaw(v interface{}) *json.RawMessage {
	data := mustMarshal(v)
	return &data
}
