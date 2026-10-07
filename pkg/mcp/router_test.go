package mcp

import (
	"strings"
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

func TestRouter_OnChangeFiresAfterUnlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "srv", []Tool{{Name: "tool"}})
	r.AddClient(client)

	var calls int
	r.SetOnChange(func() {
		calls++
		if !r.mu.TryLock() {
			t.Error("onChange ran while the router lock was held")
			return
		}
		r.mu.Unlock()
	})

	r.RefreshTools()
	r.RefreshTools()
	r.RemoveClient("srv")
	if calls != 3 {
		t.Fatalf("onChange calls = %d, want 3", calls)
	}

	r.SetOnChange(nil)
	r.RefreshTools()
	if calls != 3 {
		t.Fatalf("nil callback still fired, calls = %d", calls)
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
	// A downstream title distinct from the bare name is a display string and passes through.
	if tool.Title != "My Tool" {
		t.Errorf("expected downstream title 'My Tool', got '%s'", tool.Title)
	}
	expectedDesc := `MCP server: myagent. Call using the exact tool name "myagent__mytool". A test tool`
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

	// An empty downstream title is synthesized as the prefixed name, never the bare name.
	if tools[0].Title != "agent__notitle" {
		t.Errorf("expected title 'agent__notitle', got '%s'", tools[0].Title)
	}
	if tools[0].Title == "notitle" {
		t.Error("Title must not leak the un-prefixed tool name")
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

func TestRouter_RouteToolCall_UnknownServer(t *testing.T) {
	r := NewRouter()

	_, _, err := r.RouteToolCall("unknown__tool1")
	if err == nil {
		t.Fatal("expected error for unknown server")
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
		server   string
		tool     string
		expected string
	}{
		{"server1", "tool1", "server1__tool1"},
		{"my-server", "my-tool", "my-server__my-tool"},
		{"a", "b", "a__b"},
	}

	for _, tc := range tests {
		got := PrefixTool(tc.server, tc.tool)
		if got != tc.expected {
			t.Errorf("PrefixTool(%s, %s) = %s, want %s", tc.server, tc.tool, got, tc.expected)
		}
	}
}

func TestParsePrefixedTool(t *testing.T) {
	tests := []struct {
		input      string
		wantServer string
		wantTool   string
		wantErr    bool
	}{
		{"server1__tool1", "server1", "tool1", false},
		{"my-server__my-tool", "my-server", "my-tool", false},
		{"a__b__c", "a", "b__c", false}, // SplitN with 2 preserves extra __
		{"invalidformat", "", "", true},
		{"single-dash", "", "", true},
		{"single:colon", "", "", true},
		{"", "", "", true},
	}

	for _, tc := range tests {
		server, tool, err := ParsePrefixedTool(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParsePrefixedTool(%s) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if !tc.wantErr {
			if server != tc.wantServer {
				t.Errorf("ParsePrefixedTool(%s) server = %s, want %s", tc.input, server, tc.wantServer)
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

func TestRouter_AggregatedTools_TitleNeverLeaks(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	c1 := setupMockAgentClient(ctrl, "server-a", []Tool{
		{Name: "tool_one", Description: "Tool one"},
		{Name: "tool_two", Title: "tool_two", Description: "Tool two"},
		{Name: "tool_four", Title: "Friendly four", Description: "Tool four"},
	})
	c2 := setupMockAgentClient(ctrl, "server-b", []Tool{
		{Name: "tool_three", Description: "Tool three"},
	})

	r.AddClient(c1)
	r.AddClient(c2)
	r.RefreshTools()

	tools := r.AggregatedTools()
	unprefixed := map[string]bool{"tool_one": true, "tool_two": true, "tool_three": true, "tool_four": true}
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
		if unprefixed[tool.Title] {
			t.Errorf("Title %q leaks the un-prefixed tool name", tool.Title)
		}
	}
	if got := byName["server-a__tool_one"].Title; got != "server-a__tool_one" {
		t.Errorf("empty title = %q, want prefixed name", got)
	}
	// title equal to the bare name is an alias and must be rewritten.
	if got := byName["server-a__tool_two"].Title; got != "server-a__tool_two" {
		t.Errorf("alias title = %q, want prefixed name", got)
	}
	if got := byName["server-a__tool_four"].Title; got != "Friendly four" {
		t.Errorf("distinct title = %q, want downstream title", got)
	}
	if got := byName["server-b__tool_three"].Title; got != "server-b__tool_three" {
		t.Errorf("empty title = %q, want prefixed name", got)
	}
}

func TestRouter_AddReplicaSet_RoutesAcrossReplicas(t *testing.T) {
	ctrl := gomock.NewController(t)
	c0 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	c1 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	c2 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{c0, c1, c2})

	r := NewRouter()
	r.AddReplicaSet(set)
	r.RefreshTools()

	// Route the same prefixed tool name repeatedly; every replica should
	// eventually return as the chosen client.
	seen := map[AgentClient]bool{}
	for i := 0; i < 10; i++ {
		client, tool, err := r.RouteToolCall("svc__t")
		if err != nil {
			t.Fatalf("RouteToolCall: %v", err)
		}
		if tool != "t" {
			t.Errorf("tool = %q, want %q", tool, "t")
		}
		seen[client] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected all 3 replicas to be routed to, got %d distinct clients", len(seen))
	}
}

func TestRouter_ReplicaSet_TopologyHidden(t *testing.T) {
	// Tool namespace must not leak replica ids.
	ctrl := gomock.NewController(t)
	c0 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	c1 := setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}})
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{c0, c1})

	r := NewRouter()
	r.AddReplicaSet(set)
	r.RefreshTools()

	tools := r.AggregatedTools()
	if len(tools) != 1 {
		t.Fatalf("replicas must not multiply advertised tools: got %d, want 1", len(tools))
	}
	if tools[0].Name != "svc__t" {
		t.Errorf("leaked replica id in tool name: %q", tools[0].Name)
	}
}

func TestRouter_RemoveClient_RemovesSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{
		setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}}),
		setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}}),
	})

	r := NewRouter()
	r.AddReplicaSet(set)
	r.RefreshTools()

	if r.GetReplicaSet("svc") == nil {
		t.Fatal("set should exist before removal")
	}
	r.RemoveClient("svc")
	if r.GetReplicaSet("svc") != nil {
		t.Error("set should be removed")
	}
	if len(r.AggregatedTools()) != 0 {
		t.Error("tools should be cleared after removal")
	}
}

func TestRouter_RouteToolCall_AllReplicasUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	set := NewReplicaSet("svc", ReplicaPolicyRoundRobin, []AgentClient{
		setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}}),
		setupMockAgentClient(ctrl, "svc", []Tool{{Name: "t"}}),
	})
	for _, rep := range set.Replicas() {
		rep.SetHealthy(false)
	}

	r := NewRouter()
	r.AddReplicaSet(set)
	r.RefreshTools()

	_, _, err := r.RouteToolCall("svc__t")
	if err == nil {
		t.Fatal("expected error when all replicas unhealthy")
	}
}

func TestRouter_AggregatedTools_DescriptionComplete(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "gridctl-local", []Tool{
		{Name: "list_devices", Description: "List all devices"},
	})

	r.AddClient(client)
	r.RefreshTools()

	tools := r.AggregatedTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if !strings.Contains(tool.Description, "gridctl-local") {
		t.Errorf("Description missing server name: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "gridctl-local__list_devices") {
		t.Errorf("Description missing prefixed tool name: %q", tool.Description)
	}
	if !strings.Contains(tool.Description, "List all devices") {
		t.Errorf("Description missing original description text: %q", tool.Description)
	}
}
