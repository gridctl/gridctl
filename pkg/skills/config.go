package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// SkillSource defines a remote skill source in skills.yaml.
type SkillSource struct {
	Name           string      `yaml:"name" json:"name"`
	Repo           string      `yaml:"repo" json:"repo"`
	Ref            string      `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path           string      `yaml:"path,omitempty" json:"path,omitempty"`
	AutoUpdate     *bool       `yaml:"auto_update,omitempty" json:"autoUpdate,omitempty"`
	UpdateInterval string      `yaml:"update_interval,omitempty" json:"updateInterval,omitempty"`
	Auth           *SourceAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// SourceAuth is the declarative auth block on a skills.yaml source.
// Raw tokens must NOT appear here — use CredentialRef (e.g.
// "${vault:GIT_TOKEN}") which is resolved against the live vault at
// clone/fetch time.
type SourceAuth struct {
	Method        string `yaml:"method,omitempty" json:"method,omitempty"`
	CredentialRef string `yaml:"credential_ref,omitempty" json:"credentialRef,omitempty"`
	SSHUser       string `yaml:"ssh_user,omitempty" json:"sshUser,omitempty"`
	SSHKeyPath    string `yaml:"ssh_key_path,omitempty" json:"sshKeyPath,omitempty"`
}

// ToAuthConfig converts the declarative block into a runtime AuthConfig.
// CredentialRef is copied through unchanged; callers are responsible for
// resolving it to a raw Token before invoking the importer.
func (a *SourceAuth) ToAuthConfig() AuthConfig {
	if a == nil {
		return AuthConfig{}
	}
	return AuthConfig{
		Method:        a.Method,
		CredentialRef: a.CredentialRef,
		SSHUser:       a.SSHUser,
		SSHKeyPath:    a.SSHKeyPath,
	}
}

// SkillsConfig represents the skills.yaml file.
type SkillsConfig struct {
	Defaults SkillDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Sources  []SkillSource `yaml:"sources" json:"sources"`
}

// SkillDefaults defines global defaults for skill sources.
type SkillDefaults struct {
	AutoUpdate     bool   `yaml:"auto_update" json:"autoUpdate"`
	UpdateInterval string `yaml:"update_interval" json:"updateInterval"`
}

// DefaultSkillsConfig returns a config with sensible defaults.
func DefaultSkillsConfig() *SkillsConfig {
	return &SkillsConfig{
		Defaults: SkillDefaults{
			AutoUpdate:     true,
			UpdateInterval: "24h",
		},
	}
}

// EffectiveAutoUpdate returns the auto_update setting for a source,
// falling back to the global default.
func (c *SkillsConfig) EffectiveAutoUpdate(src *SkillSource) bool {
	if src.AutoUpdate != nil {
		return *src.AutoUpdate
	}
	return c.Defaults.AutoUpdate
}

// EffectiveUpdateInterval returns the update_interval for a source,
// falling back to the global default.
func (c *SkillsConfig) EffectiveUpdateInterval(src *SkillSource) time.Duration {
	interval := src.UpdateInterval
	if interval == "" {
		interval = c.Defaults.UpdateInterval
	}
	if interval == "" {
		interval = "24h"
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

// LoadSkillsConfig reads and parses a skills.yaml file.
func LoadSkillsConfig(path string) (*SkillsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading skills config: %w", err)
	}

	cfg := DefaultSkillsConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing skills config: %w", err)
	}

	for i, src := range cfg.Sources {
		if src.Repo == "" {
			return nil, fmt.Errorf("source %d: repo is required", i)
		}
		if src.Name == "" {
			cfg.Sources[i].Name = repoToName(src.Repo)
		}
	}

	return cfg, nil
}

// SkillsConfigPath returns the default path to skills.yaml.
func SkillsConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gridctl", "skills.yaml")
}

// IsSemVerConstraint returns true if the ref looks like a semver constraint.
func IsSemVerConstraint(ref string) bool {
	if ref == "" {
		return false
	}
	_, err := semver.NewConstraint(ref)
	if err != nil {
		return false
	}
	// Exact versions (e.g., "1.2.3") are also valid constraints,
	// but we only want constraint operators like ^, ~, >=, etc.
	first := ref[0]
	return first == '^' || first == '~' || first == '>' || first == '<' || first == '!' || first == '='
}

// ResolveSemVerConstraint finds the best matching tag for a constraint.
func ResolveSemVerConstraint(constraintStr string, tags []string) (string, error) {
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return "", fmt.Errorf("parsing constraint %q: %w", constraintStr, err)
	}

	var bestVersion *semver.Version
	var bestTag string

	for _, tag := range tags {
		v, err := semver.NewVersion(tag)
		if err != nil {
			continue
		}
		if constraint.Check(v) {
			if bestVersion == nil || v.GreaterThan(bestVersion) {
				bestVersion = v
				bestTag = tag
			}
		}
	}

	if bestVersion == nil {
		return "", fmt.Errorf("no tag matches constraint %q", constraintStr)
	}

	return bestTag, nil
}

// repoToName extracts a short name from a repo URL.
func repoToName(repo string) string {
	base := filepath.Base(repo)
	// Remove .git suffix
	if ext := filepath.Ext(base); ext == ".git" {
		base = base[:len(base)-len(ext)]
	}
	return base
}
