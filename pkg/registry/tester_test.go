package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// --- ParseCriterion ---

func TestParseCriterion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNil   bool
		wantGiven string
		wantWhen  string
		wantThen  string
	}{
		{
			name:      "standard uppercase",
			input:     "GIVEN a valid input WHEN the skill is called THEN contains result",
			wantGiven: "a valid input",
			wantWhen:  "the skill is called",
			wantThen:  "contains result",
		},
		{
			name:      "case insensitive",
			input:     "given a user WHEN tool runs then is not empty",
			wantGiven: "a user",
			wantWhen:  "tool runs",
			wantThen:  "is not empty",
		},
		{
			name:    "no match",
			input:   "just a plain string",
			wantNil: true,
		},
		{
			name:    "missing THEN",
			input:   "GIVEN something WHEN something",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCriterion(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil, got nil")
			}
			if got.Given != tt.wantGiven {
				t.Errorf("Given: got %q, want %q", got.Given, tt.wantGiven)
			}
			if got.When != tt.wantWhen {
				t.Errorf("When: got %q, want %q", got.When, tt.wantWhen)
			}
			if got.Then != tt.wantThen {
				t.Errorf("Then: got %q, want %q", got.Then, tt.wantThen)
			}
		})
	}
}

// --- resolveToolName ---

func TestResolveToolName(t *testing.T) {
	tests := []struct {
		when      string
		skillName string
		want      string
	}{
		{"the skill is called", "my-skill", "my-skill"},
		{"my-skill is called", "other-skill", "my-skill"},
		{"github__list_repos is called", "other", "github__list_repos"},
		{"use github__search_code to find it", "other", "github__search_code"},
		{"something unrelated happens", "fallback-skill", "fallback-skill"},
	}

	for _, tt := range tests {
		t.Run(tt.when, func(t *testing.T) {
			got := resolveToolName(tt.when, tt.skillName)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- evaluateAssertion ---

func TestEvaluateAssertion(t *testing.T) {
	tests := []struct {
		name    string
		actual  string
		then    string
		want    bool
		wantErr bool
	}{
		{"contains match", "hello world", "contains world", true, false},
		{"contains no match", "hello world", "contains foo", false, false},
		{"bare text match", "hello world", "world", true, false},
		{"is empty true", "  ", "is empty", true, false},
		{"is empty false", "data", "is empty", false, false},
		{"is not empty true", "data", "is not empty", true, false},
		{"is not empty false", "", "is not empty", false, false},
		{"matches valid regex", "hello123", "matches [0-9]+", true, false},
		{"matches no match", "hello", "matches [0-9]+", false, false},
		{"matches invalid regex", "hello", "matches [invalid", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluateAssertion(tt.actual, tt.then)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: got %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// --- RunAcceptanceCriteria ---

func newTestExecutor(t *testing.T, caller *mockToolCaller) *Executor {
	t.Helper()
	return NewExecutor(caller, nil)
}

func TestRunAcceptanceCriteria_AllPass(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["my-skill"] = &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("result contains expected_value here")},
	}

	e := newTestExecutor(t, caller)
	skill := &AgentSkill{
		Name: "my-skill",
		AcceptanceCriteria: []string{
			"GIVEN valid input WHEN the skill is called THEN contains expected_value",
		},
	}

	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed != 1 || result.Failed != 0 {
		t.Errorf("got passed=%d failed=%d, want passed=1 failed=0", result.Passed, result.Failed)
	}
	if !result.Results[0].Passed {
		t.Error("expected first criterion to pass")
	}
}

func TestRunAcceptanceCriteria_Fails(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["my-skill"] = &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("no match here")},
	}

	e := newTestExecutor(t, caller)
	skill := &AgentSkill{
		Name: "my-skill",
		AcceptanceCriteria: []string{
			"GIVEN valid input WHEN the skill is called THEN contains expected_value",
		},
	}

	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Failed != 1 {
		t.Errorf("got failed=%d, want 1", result.Failed)
	}
	if result.Results[0].Actual == "" {
		t.Error("expected Actual to be set on failure")
	}
}

func TestRunAcceptanceCriteria_Skipped(t *testing.T) {
	e := newTestExecutor(t, newMockToolCaller())
	skill := &AgentSkill{
		Name:               "my-skill",
		AcceptanceCriteria: []string{"not a valid criterion"},
	}

	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Skipped != 1 || result.Passed != 0 || result.Failed != 0 {
		t.Errorf("got skipped=%d passed=%d failed=%d, want skipped=1", result.Skipped, result.Passed, result.Failed)
	}
	if !result.Results[0].Skipped {
		t.Error("expected criterion to be skipped")
	}
}

func TestRunAcceptanceCriteria_ToolError(t *testing.T) {
	caller := newMockToolCaller()
	caller.errors["my-skill"] = errors.New("tool unavailable")

	e := newTestExecutor(t, caller)
	skill := &AgentSkill{
		Name: "my-skill",
		AcceptanceCriteria: []string{
			"GIVEN valid input WHEN the skill is called THEN contains result",
		},
	}

	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Failed != 1 {
		t.Errorf("got failed=%d, want 1", result.Failed)
	}
}

func TestRunAcceptanceCriteria_ToolReturnsError(t *testing.T) {
	caller := newMockToolCaller()
	caller.results["my-skill"] = &mcp.ToolCallResult{
		IsError: true,
		Content: []mcp.Content{mcp.NewTextContent("tool error message")},
	}

	e := newTestExecutor(t, caller)
	skill := &AgentSkill{
		Name: "my-skill",
		AcceptanceCriteria: []string{
			"GIVEN valid input WHEN the skill is called THEN contains result",
		},
	}

	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Failed != 1 {
		t.Errorf("expected failure on tool error, got failed=%d", result.Failed)
	}
}

func TestRunAcceptanceCriteria_DefaultTimeout(t *testing.T) {
	e := newTestExecutor(t, newMockToolCaller())
	skill := &AgentSkill{
		Name: "my-skill",
		AcceptanceCriteria: []string{
			"GIVEN valid input WHEN the skill is called THEN is not empty",
		},
	}
	// Pass 0 to use the default timeout path
	result, err := e.RunAcceptanceCriteria(context.Background(), skill, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed != 1 {
		t.Errorf("expected 1 pass, got %d", result.Passed)
	}
}

// --- Store test result persistence ---

func TestStoreTestResults(t *testing.T) {
	store := newTestStore(t)

	// nil when no result stored
	if got := store.GetTestResult("missing"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}

	result := &SkillTestResult{
		Skill:  "my-skill",
		Passed: 2,
		Failed: 1,
		Results: []CriterionResult{
			{Criterion: "GIVEN x WHEN y THEN z", Passed: true},
		},
	}
	store.SaveTestResult("my-skill", result)

	got := store.GetTestResult("my-skill")
	if got == nil {
		t.Fatal("expected result, got nil")
	}
	if got.Passed != 2 || got.Failed != 1 {
		t.Errorf("got passed=%d failed=%d, want 2/1", got.Passed, got.Failed)
	}

	// Verify copy semantics — mutating returned value doesn't affect stored value
	got.Passed = 99
	if store.GetTestResult("my-skill").Passed != 2 {
		t.Error("expected store to return independent copies")
	}
}
