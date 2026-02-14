package registry

import (
	"testing"
)

func TestPrompt_Validate(t *testing.T) {
	tests := []struct {
		name    string
		prompt  Prompt
		wantErr bool
	}{
		{
			name: "valid prompt",
			prompt: Prompt{
				Name:    "my-prompt",
				Content: "Hello {{name}}",
				State:   StateActive,
			},
		},
		{
			name: "valid prompt with arguments",
			prompt: Prompt{
				Name:    "greet_user",
				Content: "Hello {{name}}, welcome to {{place}}",
				Arguments: []Argument{
					{Name: "name", Description: "User name", Required: true},
					{Name: "place", Description: "Location", Default: "the system"},
				},
				Tags:  []string{"greeting"},
				State: StateDraft,
			},
		},
		{
			name: "defaults to draft state",
			prompt: Prompt{
				Name:    "test",
				Content: "content",
			},
		},
		{
			name:    "empty name",
			prompt:  Prompt{Content: "content"},
			wantErr: true,
		},
		{
			name: "invalid name characters",
			prompt: Prompt{
				Name:    "my prompt!",
				Content: "content",
			},
			wantErr: true,
		},
		{
			name: "name with path traversal",
			prompt: Prompt{
				Name:    "../etc/passwd",
				Content: "content",
			},
			wantErr: true,
		},
		{
			name: "empty content",
			prompt: Prompt{
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid state",
			prompt: Prompt{
				Name:    "test",
				Content: "content",
				State:   "bogus",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.prompt.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrompt_Validate_DefaultsState(t *testing.T) {
	p := Prompt{Name: "test", Content: "content"}
	if err := p.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.State != StateDraft {
		t.Errorf("expected state %q, got %q", StateDraft, p.State)
	}
}

func TestSkill_Validate(t *testing.T) {
	tests := []struct {
		name    string
		skill   Skill
		wantErr bool
	}{
		{
			name: "valid skill",
			skill: Skill{
				Name:  "deploy-app",
				Steps: []Step{{Tool: "docker__build"}, {Tool: "docker__push"}},
				State: StateActive,
			},
		},
		{
			name: "valid skill with input and timeout",
			skill: Skill{
				Name:        "run-tests",
				Description: "Run test suite",
				Steps:       []Step{{Tool: "exec__run", Arguments: map[string]string{"cmd": "go test"}}},
				Input:       []Argument{{Name: "verbose", Description: "Verbose output", Required: false}},
				Timeout:     "30s",
				Tags:        []string{"testing", "ci"},
				State:       StateDraft,
			},
		},
		{
			name:    "empty name",
			skill:   Skill{Steps: []Step{{Tool: "test"}}},
			wantErr: true,
		},
		{
			name: "invalid name characters",
			skill: Skill{
				Name:  "my skill!",
				Steps: []Step{{Tool: "test"}},
			},
			wantErr: true,
		},
		{
			name: "no steps",
			skill: Skill{
				Name:  "empty-skill",
				Steps: []Step{},
			},
			wantErr: true,
		},
		{
			name: "nil steps",
			skill: Skill{
				Name: "nil-skill",
			},
			wantErr: true,
		},
		{
			name: "step with empty tool",
			skill: Skill{
				Name:  "bad-step",
				Steps: []Step{{Tool: "good"}, {Tool: ""}},
			},
			wantErr: true,
		},
		{
			name: "invalid state",
			skill: Skill{
				Name:  "test",
				Steps: []Step{{Tool: "test"}},
				State: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.skill.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
