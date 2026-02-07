package a2a

import (
	"context"
	"testing"
)

func TestNewGateway(t *testing.T) {
	gw := NewGateway("http://localhost:8080")
	if gw == nil {
		t.Fatal("NewGateway returned nil")
	}
	if gw.handler == nil {
		t.Error("Gateway handler is nil")
	}
	if gw.remoteAgents == nil {
		t.Error("Gateway remoteAgents map is nil")
	}
}

func TestGateway_RegisterLocalAgent(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	card := AgentCard{
		Name:        "test-agent",
		Description: "Test agent description",
		Skills: []Skill{
			{ID: "skill-1", Name: "Skill One", Description: "First skill"},
			{ID: "skill-2", Name: "Skill Two", Description: "Second skill"},
		},
	}

	gw.RegisterLocalAgent("test-agent", card, nil)

	// Verify the agent was registered
	statuses := gw.Status()
	if len(statuses) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(statuses))
	}

	status := statuses[0]
	if status.Name != "test-agent" {
		t.Errorf("Name mismatch: got %s, want test-agent", status.Name)
	}
	if status.Role != "local" {
		t.Errorf("Role mismatch: got %s, want local", status.Role)
	}
	if status.SkillCount != 2 {
		t.Errorf("SkillCount mismatch: got %d, want 2", status.SkillCount)
	}
	if !status.Available {
		t.Error("Expected agent to be available")
	}
}

func TestGateway_RegisterMultipleLocalAgents(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	// Register first agent
	gw.RegisterLocalAgent("agent-1", AgentCard{
		Name:        "agent-1",
		Description: "First agent",
		Skills:      []Skill{{ID: "skill-1", Name: "Skill One"}},
	}, nil)

	// Register second agent
	gw.RegisterLocalAgent("agent-2", AgentCard{
		Name:        "agent-2",
		Description: "Second agent",
		Skills: []Skill{
			{ID: "skill-2", Name: "Skill Two"},
			{ID: "skill-3", Name: "Skill Three"},
		},
	}, nil)

	statuses := gw.Status()
	if len(statuses) != 2 {
		t.Fatalf("Expected 2 agents, got %d", len(statuses))
	}

	// Check that both agents are present
	names := make(map[string]bool)
	for _, s := range statuses {
		names[s.Name] = true
	}
	if !names["agent-1"] {
		t.Error("agent-1 not found in status")
	}
	if !names["agent-2"] {
		t.Error("agent-2 not found in status")
	}
}

func TestGateway_UnregisterLocalAgent(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	gw.RegisterLocalAgent("test-agent", AgentCard{
		Name:   "test-agent",
		Skills: []Skill{{ID: "skill-1", Name: "Skill One"}},
	}, nil)

	// Verify registered
	if len(gw.Status()) != 1 {
		t.Fatal("Agent not registered")
	}

	// Unregister
	gw.UnregisterLocalAgent("test-agent")

	// Verify unregistered
	if len(gw.Status()) != 0 {
		t.Error("Agent not unregistered")
	}
}

func TestGateway_Handler(t *testing.T) {
	gw := NewGateway("http://localhost:8080")
	handler := gw.Handler()
	if handler == nil {
		t.Error("Handler() returned nil")
	}
}

func TestGateway_AggregatedSkills(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	gw.RegisterLocalAgent("agent-1", AgentCard{
		Name: "agent-1",
		Skills: []Skill{
			{ID: "skill-a", Name: "Skill A"},
		},
	}, nil)

	gw.RegisterLocalAgent("agent-2", AgentCard{
		Name: "agent-2",
		Skills: []Skill{
			{ID: "skill-b", Name: "Skill B"},
			{ID: "skill-c", Name: "Skill C"},
		},
	}, nil)

	skills := gw.AggregatedSkills()
	if len(skills) != 3 {
		t.Errorf("Expected 3 skills, got %d", len(skills))
	}

	// Check that skills are prefixed with agent name
	skillIDs := make(map[string]bool)
	for _, s := range skills {
		skillIDs[s.ID] = true
	}

	if !skillIDs["agent-1/skill-a"] {
		t.Error("agent-1/skill-a not found")
	}
	if !skillIDs["agent-2/skill-b"] {
		t.Error("agent-2/skill-b not found")
	}
	if !skillIDs["agent-2/skill-c"] {
		t.Error("agent-2/skill-c not found")
	}
}

func TestGateway_ListRemoteAgents_Empty(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	agents := gw.ListRemoteAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 remote agents, got %d", len(agents))
	}
}

func TestGateway_GetRemoteAgent_NotFound(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	agent := gw.GetRemoteAgent("nonexistent")
	if agent != nil {
		t.Error("Expected nil for nonexistent agent")
	}
}

func TestGateway_TaskCount(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	if gw.TaskCount() != 0 {
		t.Errorf("expected 0 tasks, got %d", gw.TaskCount())
	}

	// Create tasks via the handler
	gw.handler.createTask("ctx-1")
	gw.handler.createTask("ctx-2")

	if gw.TaskCount() != 2 {
		t.Errorf("expected 2 tasks, got %d", gw.TaskCount())
	}
}

func TestGateway_StartCleanup(t *testing.T) {
	gw := NewGateway("http://localhost:8080")

	ctx, cancel := context.WithCancel(context.Background())
	gw.StartCleanup(ctx)

	// Canceling should stop the goroutine (no panic or leak)
	cancel()
}
