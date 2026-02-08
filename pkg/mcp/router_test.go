package mcp

import (
	"sync"
	"testing"

	"go.uber.org/mock/gomock"
)

func TestNewRouter(t *testing.T) {
	r := NewRouter()
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}
	if len(r.Clients()) != 0 {
		t.Errorf("new router should have no clients, got %d", len(r.Clients()))
	}
	if len(r.AggregatedTools()) != 0 {
		t.Errorf("new router should have no tools, got %d", len(r.AggregatedTools()))
	}
}

func TestRouter_AddClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "test-agent", []Tool{
		{Name: "tool1", Description: "Test tool 1"},
	})

	r.AddClient(client)

	got := r.GetClient("test-agent")
	if got == nil {
		t.Fatal("GetClient returned nil after AddClient")
	}
	if got.Name() != "test-agent" {
		t.Errorf("expected client name 'test-agent', got '%s'", got.Name())
	}
}

func TestRouter_RemoveClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "test-agent", []Tool{
		{Name: "tool1", Description: "Test tool 1"},
	})

	r.AddClient(client)
	r.RefreshTools()

	// Verify client and tools exist
	if r.GetClient("test-agent") == nil {
		t.Fatal("client should exist before removal")
	}
	if len(r.AggregatedTools()) != 1 {
		t.Fatalf("expected 1 tool before removal, got %d", len(r.AggregatedTools()))
	}

	r.RemoveClient("test-agent")

	if r.GetClient("test-agent") != nil {
		t.Error("client should be nil after removal")
	}
	// Tools should be cleared for removed client
	if len(r.AggregatedTools()) != 0 {
		t.Errorf("expected 0 tools after removal, got %d", len(r.AggregatedTools()))
	}
}

func TestRouter_GetClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "existing", nil)
	r.AddClient(client)

	// Existing client
	if got := r.GetClient("existing"); got == nil {
		t.Error("expected to get existing client")
	}

	// Non-existing client
	if got := r.GetClient("nonexistent"); got != nil {
		t.Error("expected nil for nonexistent client")
	}
}

func TestRouter_Clients(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client1 := setupMockAgentClient(ctrl, "agent1", nil)
	client2 := setupMockAgentClient(ctrl, "agent2", nil)

	r.AddClient(client1)
	r.AddClient(client2)

	clients := r.Clients()
	if len(clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(clients))
	}

	names := make(map[string]bool)
	for _, c := range clients {
		names[c.Name()] = true
	}
	if !names["agent1"] || !names["agent2"] {
		t.Error("expected both agent1 and agent2 in clients list")
	}
}

func TestRouter_RefreshTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})

	r.AddClient(client)
	r.RefreshTools()

	tools := r.AggregatedTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestRouter_AggregatedTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "myagent", []Tool{
		{Name: "mytool", Title: "My Tool", Description: "A test tool"},
	})

	r.AddClient(client)
	r.RefreshTools()

	tools := r.AggregatedTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	expectedName := "myagent__mytool"
	if tool.Name != expectedName {
		t.Errorf("expected prefixed name '%s', got '%s'", expectedName, tool.Name)
	}
	if tool.Title != "My Tool" {
		t.Errorf("expected title 'My Tool', got '%s'", tool.Title)
	}
	expectedDesc := "[myagent] A test tool"
	if tool.Description != expectedDesc {
		t.Errorf("expected description '%s', got '%s'", expectedDesc, tool.Description)
	}
}

func TestRouter_AggregatedTools_NoTitle(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "agent", []Tool{
		{Name: "notitle", Description: "No title tool"},
	})

	r.AddClient(client)
	r.RefreshTools()

	tools := r.AggregatedTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	// When no title, should use original name as title
	if tools[0].Title != "notitle" {
		t.Errorf("expected title 'notitle' (from name), got '%s'", tools[0].Title)
	}
}

func TestRouter_RouteToolCall(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})

	r.AddClient(client)
	r.RefreshTools()

	gotClient, gotTool, err := r.RouteToolCall("agent1__tool1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotClient.Name() != "agent1" {
		t.Errorf("expected client 'agent1', got '%s'", gotClient.Name())
	}
	if gotTool != "tool1" {
		t.Errorf("expected tool 'tool1', got '%s'", gotTool)
	}
}

func TestRouter_RouteToolCall_UnknownAgent(t *testing.T) {
	r := NewRouter()

	_, _, err := r.RouteToolCall("unknown__tool1")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestRouter_RouteToolCall_InvalidFormat(t *testing.T) {
	r := NewRouter()

	_, _, err := r.RouteToolCall("invalidformat")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestPrefixTool(t *testing.T) {
	tests := []struct {
		agent    string
		tool     string
		expected string
	}{
		{"agent1", "tool1", "agent1__tool1"},
		{"my-agent", "my-tool", "my-agent__my-tool"},
		{"a", "b", "a__b"},
	}

	for _, tc := range tests {
		got := PrefixTool(tc.agent, tc.tool)
		if got != tc.expected {
			t.Errorf("PrefixTool(%s, %s) = %s, want %s", tc.agent, tc.tool, got, tc.expected)
		}
	}
}

func TestParsePrefixedTool(t *testing.T) {
	tests := []struct {
		input     string
		wantAgent string
		wantTool  string
		wantErr   bool
	}{
		{"agent1__tool1", "agent1", "tool1", false},
		{"my-agent__my-tool", "my-agent", "my-tool", false},
		{"a__b__c", "a", "b__c", false}, // SplitN with 2 preserves extra __
		{"invalidformat", "", "", true},
		{"single-dash", "", "", true},
		{"single:colon", "", "", true},
		{"", "", "", true},
	}

	for _, tc := range tests {
		agent, tool, err := ParsePrefixedTool(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParsePrefixedTool(%s) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if !tc.wantErr {
			if agent != tc.wantAgent {
				t.Errorf("ParsePrefixedTool(%s) agent = %s, want %s", tc.input, agent, tc.wantAgent)
			}
			if tool != tc.wantTool {
				t.Errorf("ParsePrefixedTool(%s) tool = %s, want %s", tc.input, tool, tc.wantTool)
			}
		}
	}
}

func TestRouter_Concurrent(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent adds
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := setupMockAgentClient(ctrl, "agent"+string(rune('A'+i)), []Tool{
				{Name: "tool", Description: "Tool"},
			})
			r.AddClient(client)
		}(i)
	}
	wg.Wait()

	r.RefreshTools()

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Clients()
			_ = r.AggregatedTools()
		}()
	}
	wg.Wait()

	// Concurrent route calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = r.RouteToolCall("agentA__tool")
		}()
	}
	wg.Wait()

	// If we get here without deadlock or panic, test passes
}
