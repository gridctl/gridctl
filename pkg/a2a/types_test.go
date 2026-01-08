package a2a

import (
	"encoding/json"
	"testing"
)

func TestAgentCard_JSON(t *testing.T) {
	card := AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		URL:         "http://localhost:8080",
		Version:     "1.0.0",
		Skills: []Skill{
			{ID: "skill-1", Name: "Skill One", Description: "First skill"},
			{ID: "skill-2", Name: "Skill Two", Description: "Second skill"},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	// Test marshaling
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Failed to marshal AgentCard: %v", err)
	}

	// Test unmarshaling
	var decoded AgentCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal AgentCard: %v", err)
	}

	if decoded.Name != card.Name {
		t.Errorf("Name mismatch: got %s, want %s", decoded.Name, card.Name)
	}
	if decoded.URL != card.URL {
		t.Errorf("URL mismatch: got %s, want %s", decoded.URL, card.URL)
	}
	if len(decoded.Skills) != len(card.Skills) {
		t.Errorf("Skills count mismatch: got %d, want %d", len(decoded.Skills), len(card.Skills))
	}
}

func TestTaskState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    TaskState
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateInputRequired, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCancelled, true},
		{TaskStateRejected, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestTask_JSON(t *testing.T) {
	task := Task{
		ID:     "test-task",
		Status: TaskStatus{State: TaskStateWorking},
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Failed to marshal task: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal task: %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID mismatch: got %s, want %s", decoded.ID, task.ID)
	}
	if decoded.Status.State != task.Status.State {
		t.Errorf("State mismatch: got %s, want %s", decoded.Status.State, task.Status.State)
	}
}

func TestMessage_Parts(t *testing.T) {
	msg := Message{
		Role: RoleUser,
		Parts: []Part{
			{Type: PartTypeText, Text: "Hello, agent!"},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if decoded.Role != msg.Role {
		t.Errorf("Role mismatch: got %s, want %s", decoded.Role, msg.Role)
	}
	if len(decoded.Parts) != 1 {
		t.Fatalf("Parts count mismatch: got %d, want 1", len(decoded.Parts))
	}
	if decoded.Parts[0].Text != "Hello, agent!" {
		t.Errorf("Text mismatch: got %s, want %s", decoded.Parts[0].Text, "Hello, agent!")
	}
}

func TestRequest_JSON(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"message/send", MethodSendMessage},
		{"tasks/get", MethodGetTask},
		{"tasks/list", MethodListTasks},
		{"tasks/cancel", MethodCancelTask},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := json.RawMessage(`"1"`)
			request := Request{
				JSONRPC: "2.0",
				ID:      &id,
				Method:  tt.method,
				Params:  json.RawMessage(`{}`),
			}

			data, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			var decoded Request
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal request: %v", err)
			}

			if decoded.JSONRPC != "2.0" {
				t.Errorf("JSONRPC version mismatch: got %s, want 2.0", decoded.JSONRPC)
			}
			if decoded.Method != tt.method {
				t.Errorf("Method mismatch: got %s, want %s", decoded.Method, tt.method)
			}
		})
	}
}

func TestResponse_Success(t *testing.T) {
	id := json.RawMessage(`"1"`)
	resp := NewSuccessResponse(&id, map[string]string{"key": "value"})

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC mismatch: got %s, want 2.0", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Errorf("Unexpected error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Error("Expected result, got nil")
	}
}

func TestResponse_Error(t *testing.T) {
	id := json.RawMessage(`"1"`)
	resp := NewErrorResponse(&id, MethodNotFound, "Unknown method")

	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC mismatch: got %s, want 2.0", resp.JSONRPC)
	}
	if resp.Error == nil {
		t.Fatal("Expected error, got nil")
	}
	if resp.Error.Code != MethodNotFound {
		t.Errorf("Error code mismatch: got %d, want %d", resp.Error.Code, MethodNotFound)
	}
	if resp.Error.Message != "Unknown method" {
		t.Errorf("Error message mismatch: got %s, want 'Unknown method'", resp.Error.Message)
	}
}

func TestA2AAgentStatus(t *testing.T) {
	status := A2AAgentStatus{
		Name:        "test-agent",
		Role:        "local",
		Available:   true,
		SkillCount:  3,
		Skills:      []string{"skill-1", "skill-2", "skill-3"},
		Description: "Test agent description",
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal status: %v", err)
	}

	var decoded A2AAgentStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal status: %v", err)
	}

	if decoded.Name != status.Name {
		t.Errorf("Name mismatch: got %s, want %s", decoded.Name, status.Name)
	}
	if decoded.Role != status.Role {
		t.Errorf("Role mismatch: got %s, want %s", decoded.Role, status.Role)
	}
	if decoded.SkillCount != status.SkillCount {
		t.Errorf("SkillCount mismatch: got %d, want %d", decoded.SkillCount, status.SkillCount)
	}
}

func TestNewTextPart(t *testing.T) {
	part := NewTextPart("Hello")
	if part.Type != PartTypeText {
		t.Errorf("Type mismatch: got %s, want %s", part.Type, PartTypeText)
	}
	if part.Text != "Hello" {
		t.Errorf("Text mismatch: got %s, want Hello", part.Text)
	}
}

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage(RoleUser, "Hello")
	if msg.Role != RoleUser {
		t.Errorf("Role mismatch: got %s, want %s", msg.Role, RoleUser)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts count mismatch: got %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Text != "Hello" {
		t.Errorf("Text mismatch: got %s, want Hello", msg.Parts[0].Text)
	}
}
