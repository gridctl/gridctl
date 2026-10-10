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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/skills"
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

// Source is one external git repository a pack selects skills and agents
// from. Field names and YAML tags mirror skills.SkillSource. A source is a
// repository the pack author chose, pinned by commit at import, not an index
// entry.
type Source struct {
	Repo string      `yaml:"repo" json:"repo"`
	Ref  string      `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path string      `yaml:"path,omitempty" json:"path,omitempty"`
	Auth *SourceAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// SourceAuth is the declarative auth block on a pack source. It mirrors
// skills.SourceAuth except ssh_key_path, which a manifest must not carry:
// a key path is a caller secret, not pack content. UnmarshalYAML captures
// that key so validation can refuse it by name instead of dropping it.
type SourceAuth struct {
	Method        string `yaml:"method,omitempty" json:"method,omitempty"`
	CredentialRef string `yaml:"credential_ref,omitempty" json:"credentialRef,omitempty"`
	SSHUser       string `yaml:"ssh_user,omitempty" json:"sshUser,omitempty"`
	sshKeyPath    string `yaml:"-" json:"-"`
}

// UnmarshalYAML decodes the auth block and remembers ssh_key_path so
// Validate can reject it explicitly.
func (a *SourceAuth) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Method        string `yaml:"method"`
		CredentialRef string `yaml:"credential_ref"`
		SSHUser       string `yaml:"ssh_user"`
		SSHKeyPath    string `yaml:"ssh_key_path"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	a.Method = raw.Method
	a.CredentialRef = raw.CredentialRef
	a.SSHUser = raw.SSHUser
	a.sshKeyPath = raw.SSHKeyPath
	return nil
}

// HasSSHKeyPath reports whether the manifest named ssh_key_path.
func (a *SourceAuth) HasSSHKeyPath() bool {
	return a != nil && a.sshKeyPath != ""
}

// Selection is one skill or agent name. A scalar is a name from the pack
// repository. A mapping names a resource in a declared source. JSON encoding
// is always the object form.
type Selection struct {
	Name   string `yaml:"name" json:"name"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

// UnmarshalYAML accepts a scalar name or a mapping with name and source.
// A mapping that omits either key is an error that names the line.
func (s *Selection) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var name string
		if err := value.Decode(&name); err != nil {
			return err
		}
		s.Name = name
		s.Source = ""
		return nil
	case yaml.MappingNode:
		var raw struct {
			Name   string `yaml:"name"`
			Source string `yaml:"source"`
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		if raw.Name == "" {
			return fmt.Errorf("selection mapping requires name (line %d)", value.Line)
		}
		if raw.Source == "" {
			return fmt.Errorf("selection mapping requires source (line %d)", value.Line)
		}
		s.Name = raw.Name
		s.Source = raw.Source
		return nil
	default:
		return fmt.Errorf("selection must be a name or a mapping (line %d)", value.Line)
	}
}

// Manifest is a parsed gridctl-pack.yaml.
type Manifest struct {
	APIVersion  string `yaml:"apiVersion" json:"apiVersion"`
	Kind        string `yaml:"kind" json:"kind"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Author      Author `yaml:"author,omitempty" json:"author,omitempty"`
	// Sources names external git repositories this pack selects from.
	// Empty means every selection comes from this repository.
	Sources map[string]Source `yaml:"sources,omitempty" json:"sources,omitempty"`
	// Skills and Agents select resources by name. A string entry (Source
	// empty) comes from this repository. A mapping names a declared source.
	// Empty means every resource of that kind discovered in this repository
	// only; external sources are never import-all.
	Skills []Selection `yaml:"skills,omitempty" json:"skills,omitempty"`
	Agents []Selection `yaml:"agents,omitempty" json:"agents,omitempty"`
	// Wiring asks apply to ensure the gateway entry is present in the
	// selected clients (empty Clients = all detected).
	Wiring  bool     `yaml:"wiring,omitempty" json:"wiring,omitempty"`
	Clients []string `yaml:"clients,omitempty" json:"clients,omitempty"`
	// Rules selects context rule fragments from the pack repo's
	// rules/*.md (or fragments/*.md) discovery. Empty means none
	// (rules are opt-in, never import-all).
	Rules []string `yaml:"rules,omitempty" json:"rules,omitempty"`
	// Variables documents value-free prerequisites. Installing a pack never
	// writes these declarations to a stack or variable store.
	Variables map[string]VariableDeclaration `yaml:"variables,omitempty" json:"variables,omitempty"`
	// Stack names a stack file inside this repository. Empty means the
	// pack carries no stack and apply does not start a gateway.
	Stack string `yaml:"stack,omitempty" json:"stack,omitempty"`
}

// VariableDeclaration documents a pack prerequisite.
type VariableDeclaration struct {
	Required    *bool  `yaml:"required,omitempty" json:"required,omitempty"`
	Secret      *bool  `yaml:"secret,omitempty" json:"secret,omitempty"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Docs        string `yaml:"docs,omitempty" json:"docs,omitempty"`
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
	for key, declaration := range m.Variables {
		if key == "" {
			return fmt.Errorf("variable declaration key cannot be empty")
		}
		switch declaration.Type {
		case "", "string", "json", "list", "number", "bool":
		default:
			return fmt.Errorf("variable %q has unsupported type %q", key, declaration.Type)
		}
	}
	if err := validateStackPath(m.Stack); err != nil {
		return err
	}
	if err := m.validateSources(); err != nil {
		return err
	}
	if err := m.validateSelections(m.Skills, "skill"); err != nil {
		return err
	}
	if err := m.validateSelections(m.Agents, "agent"); err != nil {
		return err
	}
	return m.validateRuleNames()
}

// SkillNames returns skill selections that come from the pack repository.
func (m *Manifest) SkillNames() []string {
	return localSelectionNames(m.Skills)
}

// AgentNames returns agent selections that come from the pack repository.
func (m *Manifest) AgentNames() []string {
	return localSelectionNames(m.Agents)
}

func localSelectionNames(in []Selection) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, sel := range in {
		if sel.Source == "" {
			out = append(out, sel.Name)
		}
	}
	return out
}

func (m *Manifest) validateSources() error {
	names := make([]string, 0, len(m.Sources))
	for name := range m.Sources {
		names = append(names, name)
	}
	slices.Sort(names)
	seen := map[string]string{}
	for _, name := range names {
		src := m.Sources[name]
		if !namePattern.MatchString(name) {
			return fmt.Errorf("source %q must be lowercase letters, digits, and hyphens", name)
		}
		switch gitpkg.DetectProtocol(src.Repo) {
		case gitpkg.ProtocolHTTPS, gitpkg.ProtocolSSH:
		default:
			return fmt.Errorf("source %q repo %q must be an https or ssh URL", name, src.Repo)
		}
		if other, ok := seen[src.Repo]; ok {
			return fmt.Errorf("source %q has the same repo as %q", name, other)
		}
		seen[src.Repo] = name
		if src.Path != "" {
			if err := skills.SafeRepoPath(src.Path); err != nil {
				return fmt.Errorf("source %q path: %w", name, err)
			}
		}
		if src.Auth == nil {
			continue
		}
		if src.Auth.HasSSHKeyPath() {
			return fmt.Errorf("source %q auth.ssh_key_path is not allowed in a pack manifest", name)
		}
		switch src.Auth.Method {
		case "ssh-key", "ssh-agent", "token":
		default:
			return fmt.Errorf("source %q auth.method %q must be ssh-key, ssh-agent, or token", name, src.Auth.Method)
		}
	}
	return nil
}

func (m *Manifest) validateSelections(selections []Selection, kind string) error {
	for _, sel := range selections {
		if sel.Source == "" {
			continue
		}
		if _, ok := m.Sources[sel.Source]; !ok {
			return fmt.Errorf("%s %q names undeclared source %q", kind, sel.Name, sel.Source)
		}
	}
	return nil
}

// validateStackPath accepts an empty path and otherwise requires a
// slash-separated relative path that stays inside the pack repository.
func validateStackPath(p string) error {
	if p == "" {
		return nil
	}
	if path.Clean(p) != p || path.IsAbs(p) || filepath.IsAbs(p) || strings.Contains(p, `\`) {
		return fmt.Errorf("stack path %q must be a relative path inside the pack repository", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("stack path %q must be a relative path inside the pack repository", p)
		}
	}
	return nil
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
