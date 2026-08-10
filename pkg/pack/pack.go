// Package pack defines the gridctl pack manifest: a versioned selector
// over a repo's skills, agents, context rule fragments, and gateway
// wiring, so one git import configures a whole team setup. This package
// is deliberately thin — types, parsing, and validation only. There is
// no pack engine: the CLI expands a manifest into calls against the
// existing kind managers (skillsync, agentsync, contexts, wiring),
// which own every write.
//
// Field names align with the Claude Code plugin.json family where the
// semantics match (name, version, description, author, skills, agents),
// so a pack maps onto that ecosystem instead of fighting it. The word
// "bundle" is avoided: the MCP ecosystem uses it for .mcpb, a
// single-server archive format.
package pack

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFileName is the fixed manifest location at a repo root.
const ManifestFileName = "gridctl-pack.yaml"

// APIVersion is the current manifest schema, emitted by everything
// gridctl writes and documented as the value authors should use.
const APIVersion = "gridctl.dev/v1"

// LegacyAPIVersion is the pre-1.0 alpha schema. It is structurally
// identical to APIVersion and stays accepted indefinitely so packs
// authored before the graduation keep importing (Article IX).
const LegacyAPIVersion = "gridctl.dev/v1alpha1"

// acceptedAPIVersions lists every manifest schema this gridctl parses,
// current first for error text.
var acceptedAPIVersions = []string{APIVersion, LegacyAPIVersion}

// Kind is the manifest's required kind value.
const Kind = "Pack"

// namePattern matches valid pack names: the same charset rule agents
// use (lowercase letters, digits, hyphens), so pack names are safe as
// lockfile keys and path components everywhere.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Author identifies a pack's maintainer (plugin.json-aligned subset).
type Author struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// Manifest is a parsed gridctl-pack.yaml.
type Manifest struct {
	APIVersion  string `yaml:"apiVersion" json:"apiVersion"`
	Kind        string `yaml:"kind" json:"kind"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Author      Author `yaml:"author,omitempty" json:"author,omitempty"`
	// Skills and Agents select resources by name from the same repo's
	// discovery. Empty means every discovered resource of that kind.
	Skills []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	Agents []string `yaml:"agents,omitempty" json:"agents,omitempty"`
	// Wiring asks apply to ensure the gateway entry is present in the
	// selected clients (empty Clients = all detected).
	Wiring  bool     `yaml:"wiring,omitempty" json:"wiring,omitempty"`
	Clients []string `yaml:"clients,omitempty" json:"clients,omitempty"`
	// Rules selects context rule fragments from the pack repo's
	// rules/*.md (or fragments/*.md) discovery. Empty means none
	// (rules are opt-in, never import-all).
	Rules []string `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// Parse decodes and validates a manifest.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing pack manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseFile reads and parses a manifest from path. A missing file
// reports os.IsNotExist-compatible errors so callers can distinguish
// "no manifest" from "broken manifest".
func ParseFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed file name under a caller-controlled root
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Validate checks the manifest's schema envelope and name.
func (m *Manifest) Validate() error {
	if !slices.Contains(acceptedAPIVersions, m.APIVersion) {
		return fmt.Errorf("unsupported apiVersion %q (this gridctl supports %s)",
			m.APIVersion, strings.Join(acceptedAPIVersions, ", "))
	}
	if m.Kind != Kind {
		return fmt.Errorf("unsupported kind %q (expected %s)", m.Kind, Kind)
	}
	if m.Name == "" {
		return fmt.Errorf("pack name is required")
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("pack name %q must be lowercase letters, digits, and hyphens", m.Name)
	}
	return m.validateRuleNames()
}

// Warnings reports advisory conditions a valid manifest still carries.
// Currently none; the reserved-rules warning was removed when the
// rules fragment library activated.
func (m *Manifest) Warnings() []string {
	return nil
}

// validateRuleNames rejects rule selections that could never become safe
// fragment filenames, mirroring the skill/agent name discipline.
func (m *Manifest) validateRuleNames() error {
	for _, r := range m.Rules {
		if !namePattern.MatchString(r) {
			return fmt.Errorf("invalid rule name %q: must be lowercase alphanumerics and hyphens", r)
		}
	}
	return nil
}
