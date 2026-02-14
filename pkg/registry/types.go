package registry

import (
	"fmt"
	"regexp"
)

// namePattern validates MCP-compatible identifiers.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ItemState represents the lifecycle state of a prompt or skill.
type ItemState string

const (
	StateDraft    ItemState = "draft"
	StateActive   ItemState = "active"
	StateDisabled ItemState = "disabled"
)

// Prompt represents a reusable prompt template.
type Prompt struct {
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Content     string     `yaml:"content" json:"content"`
	Arguments   []Argument `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	Tags        []string   `yaml:"tags,omitempty" json:"tags,omitempty"`
	State       ItemState  `yaml:"state" json:"state"`
}

// Argument represents a parameter in a prompt template.
type Argument struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
}

// Skill represents a composed tool workflow.
type Skill struct {
	Name        string     `yaml:"name" json:"name"`
	Description string     `yaml:"description" json:"description"`
	Steps       []Step     `yaml:"steps" json:"steps"`
	Input       []Argument `yaml:"input,omitempty" json:"input,omitempty"`
	Timeout     string     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Tags        []string   `yaml:"tags,omitempty" json:"tags,omitempty"`
	State       ItemState  `yaml:"state" json:"state"`
}

// Step represents a single step in a skill's tool chain.
type Step struct {
	Tool      string            `yaml:"tool" json:"tool"`
	Arguments map[string]string `yaml:"arguments,omitempty" json:"arguments,omitempty"`
}

// RegistryStatus contains summary statistics.
type RegistryStatus struct {
	TotalPrompts  int `json:"totalPrompts"`
	ActivePrompts int `json:"activePrompts"`
	TotalSkills   int `json:"totalSkills"`
	ActiveSkills  int `json:"activeSkills"`
}

// Validate checks a Prompt for correctness.
func (p *Prompt) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !namePattern.MatchString(p.Name) {
		return fmt.Errorf("name %q must match %s", p.Name, namePattern.String())
	}
	if p.Content == "" {
		return fmt.Errorf("content is required")
	}
	if err := validateState(&p.State); err != nil {
		return err
	}
	return nil
}

// Validate checks a Skill for correctness.
func (sk *Skill) Validate() error {
	if sk.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !namePattern.MatchString(sk.Name) {
		return fmt.Errorf("name %q must match %s", sk.Name, namePattern.String())
	}
	if len(sk.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}
	for i, step := range sk.Steps {
		if step.Tool == "" {
			return fmt.Errorf("step[%d]: tool is required", i)
		}
	}
	if err := validateState(&sk.State); err != nil {
		return err
	}
	return nil
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
