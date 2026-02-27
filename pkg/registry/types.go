package registry

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ItemState represents the lifecycle state of a skill.
// Note: state is a gridctl extension, not part of the agentskills.io spec.
type ItemState string

const (
	StateDraft    ItemState = "draft"
	StateActive   ItemState = "active"
	StateDisabled ItemState = "disabled"
)

// AgentSkill represents an Agent Skills standard SKILL.md file.
// See https://agentskills.io/specification for the full spec.
type AgentSkill struct {
	// --- Frontmatter fields (from YAML between --- delimiters) ---
	Name          string            `yaml:"name" json:"name"`
	Description   string            `yaml:"description" json:"description"`
	License       string            `yaml:"license,omitempty" json:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	AllowedTools  string            `yaml:"allowed-tools,omitempty" json:"allowedTools,omitempty"`

	// --- Gridctl extensions (not in agentskills.io spec) ---
	State ItemState `yaml:"state,omitempty" json:"state"`

	// Workflow definition (gridctl extension).
	// When present, the skill becomes executable and is exposed as an MCP tool.
	Inputs   map[string]SkillInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Workflow []WorkflowStep        `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	Output   *WorkflowOutput       `yaml:"output,omitempty" json:"output,omitempty"`

	// --- Parsed from file content (not in frontmatter YAML) ---
	Body string `yaml:"-" json:"body"` // Markdown content after frontmatter

	// --- Computed fields (not serialized to YAML) ---
	FileCount int    `yaml:"-" json:"fileCount"` // Number of supporting files (scripts/, references/, assets/)
	Dir       string `yaml:"-" json:"-"`          // Relative path from skills/ root (e.g., "git-workflow/branch-fork")
}

// IsExecutable returns true if the skill has a workflow definition.
func (s *AgentSkill) IsExecutable() bool {
	return len(s.Workflow) > 0
}

// Validate checks the skill against the agentskills.io specification.
func (s *AgentSkill) Validate() error {
	return ValidateSkill(s)
}

// SkillInput defines a parameter for an executable skill.
type SkillInput struct {
	Type        string   `yaml:"type" json:"type"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}

// WorkflowStep defines a single tool invocation in a workflow.
type WorkflowStep struct {
	ID        string         `yaml:"id" json:"id"`
	Tool      string         `yaml:"tool" json:"tool"`
	Args      map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
	DependsOn StringOrSlice  `yaml:"depends_on,omitempty" json:"dependsOn,omitempty"`
	Condition string         `yaml:"condition,omitempty" json:"condition,omitempty"`
	OnError   string         `yaml:"on_error,omitempty" json:"onError,omitempty"`
	Timeout   string         `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry     *RetryPolicy   `yaml:"retry,omitempty" json:"retry,omitempty"`
}

// RetryPolicy defines retry behavior for a workflow step.
type RetryPolicy struct {
	MaxAttempts int    `yaml:"max_attempts" json:"maxAttempts"`
	Backoff     string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
}

// WorkflowOutput controls how step results are assembled.
type WorkflowOutput struct {
	Format   string   `yaml:"format,omitempty" json:"format,omitempty"`     // merged|last|custom
	Include  []string `yaml:"include,omitempty" json:"include,omitempty"`
	Template string   `yaml:"template,omitempty" json:"template,omitempty"`
}

// StringOrSlice allows YAML fields to be either a single string or a list.
// "depends_on: step-a" and "depends_on: [step-a, step-b]" both work.
type StringOrSlice []string

// UnmarshalYAML implements custom YAML unmarshaling for StringOrSlice.
func (s *StringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = StringOrSlice{value.Value}
		return nil
	}
	var slice []string
	if err := value.Decode(&slice); err != nil {
		return err
	}
	*s = slice
	return nil
}

// SkillFile represents a file within a skill directory.
type SkillFile struct {
	Path  string `json:"path"`  // Relative path within the skill dir (e.g., "scripts/lint.sh")
	Size  int64  `json:"size"`  // File size in bytes
	IsDir bool   `json:"isDir"` // True for directories
}

// RegistryStatus contains summary statistics.
type RegistryStatus struct {
	TotalSkills  int `json:"totalSkills"`
	ActiveSkills int `json:"activeSkills"`
}

// validateState checks that the state is valid, defaulting to draft if empty.
func validateState(s *ItemState) error {
	switch *s {
	case "":
		*s = StateDraft
	case StateDraft, StateActive, StateDisabled:
		// valid
	default:
		return fmt.Errorf("state %q must be one of: draft, active, disabled", *s)
	}
	return nil
}
