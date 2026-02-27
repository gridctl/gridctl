package registry

import (
	"strings"
	"testing"
)

func TestBuildWorkflowDAG_NoDeps(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b"},
		{ID: "c", Tool: "server__tool-c"},
	}

	levels, err := BuildWorkflowDAG(steps)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG() error = %v", err)
	}

	if len(levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(levels))
	}
	if len(levels[0]) != 3 {
		t.Errorf("expected 3 steps in level 0, got %d", len(levels[0]))
	}
}

func TestBuildWorkflowDAG_LinearChain(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"a"}},
		{ID: "c", Tool: "server__tool-c", DependsOn: StringOrSlice{"b"}},
	}

	levels, err := BuildWorkflowDAG(steps)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG() error = %v", err)
	}

	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(levels))
	}
	if levels[0][0].ID != "a" {
		t.Errorf("level 0 should be 'a', got '%s'", levels[0][0].ID)
	}
	if levels[1][0].ID != "b" {
		t.Errorf("level 1 should be 'b', got '%s'", levels[1][0].ID)
	}
	if levels[2][0].ID != "c" {
		t.Errorf("level 2 should be 'c', got '%s'", levels[2][0].ID)
	}
}

func TestBuildWorkflowDAG_FanOut(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"a"}},
		{ID: "c", Tool: "server__tool-c", DependsOn: StringOrSlice{"a"}},
	}

	levels, err := BuildWorkflowDAG(steps)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG() error = %v", err)
	}

	if len(levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0].ID != "a" {
		t.Errorf("level 0 should contain only 'a'")
	}
	if len(levels[1]) != 2 {
		t.Errorf("level 1 should contain 2 steps (b and c), got %d", len(levels[1]))
	}
}

func TestBuildWorkflowDAG_FanIn(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b"},
		{ID: "c", Tool: "server__tool-c", DependsOn: StringOrSlice{"a", "b"}},
	}

	levels, err := BuildWorkflowDAG(steps)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG() error = %v", err)
	}

	if len(levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(levels))
	}
	if len(levels[0]) != 2 {
		t.Errorf("level 0 should contain 2 steps (a and b), got %d", len(levels[0]))
	}
	if len(levels[1]) != 1 || levels[1][0].ID != "c" {
		t.Errorf("level 1 should contain only 'c'")
	}
}

func TestBuildWorkflowDAG_Diamond(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"a"}},
		{ID: "c", Tool: "server__tool-c", DependsOn: StringOrSlice{"a"}},
		{ID: "d", Tool: "server__tool-d", DependsOn: StringOrSlice{"b", "c"}},
	}

	levels, err := BuildWorkflowDAG(steps)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG() error = %v", err)
	}

	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(levels))
	}
	if len(levels[0]) != 1 || levels[0][0].ID != "a" {
		t.Errorf("level 0 should contain only 'a'")
	}
	if len(levels[1]) != 2 {
		t.Errorf("level 1 should contain 2 steps (b and c), got %d", len(levels[1]))
	}
	if len(levels[2]) != 1 || levels[2][0].ID != "d" {
		t.Errorf("level 2 should contain only 'd'")
	}
}

func TestBuildWorkflowDAG_CycleDetection(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a", DependsOn: StringOrSlice{"b"}},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"a"}},
	}

	_, err := BuildWorkflowDAG(steps)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle error message, got: %v", err)
	}
}

func TestBuildWorkflowDAG_ThreeNodeCycle(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a", DependsOn: StringOrSlice{"c"}},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"a"}},
		{ID: "c", Tool: "server__tool-c", DependsOn: StringOrSlice{"b"}},
	}

	_, err := BuildWorkflowDAG(steps)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle error message, got: %v", err)
	}
}

func TestBuildWorkflowDAG_UnknownDep(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "a", Tool: "server__tool-a"},
		{ID: "b", Tool: "server__tool-b", DependsOn: StringOrSlice{"nonexistent"}},
	}

	_, err := BuildWorkflowDAG(steps)
	if err == nil {
		t.Fatal("expected error for unknown dependency, got nil")
	}
	if !strings.Contains(err.Error(), "unknown step 'nonexistent'") {
		t.Errorf("expected unknown step error, got: %v", err)
	}
}

func TestBuildWorkflowDAG_UnknownDepWithSuggestion(t *testing.T) {
	steps := []WorkflowStep{
		{ID: "validate-config", Tool: "server__validate"},
		{ID: "apply-config", Tool: "server__apply", DependsOn: StringOrSlice{"validate-confg"}},
	}

	_, err := BuildWorkflowDAG(steps)
	if err == nil {
		t.Fatal("expected error for unknown dependency, got nil")
	}
	if !strings.Contains(err.Error(), "did you mean 'validate-config'") {
		t.Errorf("expected suggestion in error, got: %v", err)
	}
}

func TestBuildWorkflowDAG_Empty(t *testing.T) {
	levels, err := BuildWorkflowDAG(nil)
	if err != nil {
		t.Fatalf("BuildWorkflowDAG(nil) error = %v", err)
	}
	if levels != nil {
		t.Errorf("expected nil levels, got %v", levels)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"validate-confg", "validate-config", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := levenshtein(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
