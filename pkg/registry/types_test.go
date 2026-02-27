package registry

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentSkill_Validate(t *testing.T) {
	tests := []struct {
		name    string
		skill   AgentSkill
		wantErr bool
	}{
		{
			name: "valid skill",
			skill: AgentSkill{
				Name:        "code-review",
				Description: "Review code for quality issues",
				State:       StateActive,
			},
		},
		{
			name: "valid skill with all fields",
			skill: AgentSkill{
				Name:          "deploy-app",
				Description:   "Deploy application to production",
				License:       "MIT",
				Compatibility: "Requires Docker",
				Metadata:      map[string]string{"author": "test", "version": "1.0"},
				AllowedTools:  "Bash(docker:*) Read",
				State:         StateActive,
				Body:          "# Deploy\n\nRun the deployment.",
			},
		},
		{
			name: "defaults to draft state",
			skill: AgentSkill{
				Name:        "test-skill",
				Description: "A test skill",
			},
		},
		{
			name: "empty name",
			skill: AgentSkill{
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "missing description",
			skill: AgentSkill{
				Name: "my-skill",
			},
			wantErr: true,
		},
		{
			name: "invalid name characters (uppercase)",
			skill: AgentSkill{
				Name:        "MySkill",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "invalid name characters (spaces)",
			skill: AgentSkill{
				Name:        "my skill",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "consecutive hyphens",
			skill: AgentSkill{
				Name:        "my--skill",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "leading hyphen",
			skill: AgentSkill{
				Name:        "-my-skill",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "trailing hyphen",
			skill: AgentSkill{
				Name:        "my-skill-",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "name too long",
			skill: AgentSkill{
				Name:        "a234567890123456789012345678901234567890123456789012345678901234x",
				Description: "A skill",
			},
			wantErr: true,
		},
		{
			name: "single character name",
			skill: AgentSkill{
				Name:        "a",
				Description: "A skill",
			},
		},
		{
			name: "invalid state",
			skill: AgentSkill{
				Name:        "test",
				Description: "A skill",
				State:       "bogus",
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

func TestRegistryStatus_JSON(t *testing.T) {
	status := RegistryStatus{
		TotalSkills:  5,
		ActiveSkills: 3,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded RegistryStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.TotalSkills != 5 {
		t.Errorf("TotalSkills = %d, want 5", decoded.TotalSkills)
	}
	if decoded.ActiveSkills != 3 {
		t.Errorf("ActiveSkills = %d, want 3", decoded.ActiveSkills)
	}

	// Verify JSON field names
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map error = %v", err)
	}
	if _, ok := raw["totalSkills"]; !ok {
		t.Error("expected JSON key 'totalSkills'")
	}
	if _, ok := raw["activeSkills"]; !ok {
		t.Error("expected JSON key 'activeSkills'")
	}
}

func TestValidateState(t *testing.T) {
	tests := []struct {
		name    string
		state   ItemState
		want    ItemState
		wantErr bool
	}{
		{name: "empty defaults to draft", state: "", want: StateDraft},
		{name: "draft is valid", state: StateDraft, want: StateDraft},
		{name: "active is valid", state: StateActive, want: StateActive},
		{name: "disabled is valid", state: StateDisabled, want: StateDisabled},
		{name: "invalid state", state: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.state
			err := validateState(&s)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateState() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && s != tt.want {
				t.Errorf("validateState() state = %q, want %q", s, tt.want)
			}
		})
	}
}

func TestStringOrSlice_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want StringOrSlice
	}{
		{
			name: "single string",
			yaml: `depends_on: step-a`,
			want: StringOrSlice{"step-a"},
		},
		{
			name: "string slice",
			yaml: `depends_on: [step-a, step-b]`,
			want: StringOrSlice{"step-a", "step-b"},
		},
		{
			name: "multiline slice",
			yaml: "depends_on:\n  - step-a\n  - step-b\n  - step-c",
			want: StringOrSlice{"step-a", "step-b", "step-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result struct {
				DependsOn StringOrSlice `yaml:"depends_on"`
			}
			if err := yaml.Unmarshal([]byte(tt.yaml), &result); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if len(result.DependsOn) != len(tt.want) {
				t.Fatalf("got %d items, want %d", len(result.DependsOn), len(tt.want))
			}
			for i, v := range result.DependsOn {
				if v != tt.want[i] {
					t.Errorf("item[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestStringOrSlice_EmptyUnmarshal(t *testing.T) {
	var result struct {
		DependsOn StringOrSlice `yaml:"depends_on"`
	}
	if err := yaml.Unmarshal([]byte("other: value"), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if result.DependsOn != nil {
		t.Errorf("expected nil, got %v", result.DependsOn)
	}
}

func TestAgentSkill_IsExecutable(t *testing.T) {
	tests := []struct {
		name string
		skill AgentSkill
		want  bool
	}{
		{
			name: "with workflow",
			skill: AgentSkill{
				Workflow: []WorkflowStep{{ID: "step-1", Tool: "server__tool"}},
			},
			want: true,
		},
		{
			name: "without workflow",
			skill: AgentSkill{},
			want: false,
		},
		{
			name: "empty workflow slice",
			skill: AgentSkill{
				Workflow: []WorkflowStep{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.skill.IsExecutable(); got != tt.want {
				t.Errorf("IsExecutable() = %v, want %v", got, tt.want)
			}
		})
	}
}
