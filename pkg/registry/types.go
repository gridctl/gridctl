package registry

import "fmt"

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

	// --- Parsed from file content (not in frontmatter YAML) ---
	Body string `yaml:"-" json:"body"` // Markdown content after frontmatter

	// --- Computed fields (not serialized to YAML) ---
	FileCount int    `yaml:"-" json:"fileCount"` // Number of supporting files (scripts/, references/, assets/)
	Dir       string `yaml:"-" json:"-"`          // Relative path from skills/ root (e.g., "git-workflow/branch-fork")
}

// Validate checks the skill against the agentskills.io specification.
func (s *AgentSkill) Validate() error {
	return ValidateSkill(s)
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
