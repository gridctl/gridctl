package modelsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/project"
)

// Complexity tiers, in LiteLLM's fixed vocabulary and render order.
const (
	TierSimple    = "SIMPLE"
	TierMedium    = "MEDIUM"
	TierComplex   = "COMPLEX"
	TierReasoning = "REASONING"
)

// tierOrder is the deterministic emission order for tier maps.
var tierOrder = []string{TierSimple, TierMedium, TierComplex, TierReasoning}

// PolicyKind is the required kind: field value in the policy document.
const PolicyKind = "models"

// Policy is the typed model routing policy. Unknown top-level keys land
// in Extra so a newer document survives an older binary's validation
// pass (and so dangerous keys can be linted by name).
type Policy struct {
	Name        string `yaml:"name"`
	Kind        string `yaml:"kind"`
	Description string `yaml:"description"`

	Router Router `yaml:"router"`
	// Backends are references to model_name values that already exist in
	// the user's own LiteLLM model_list. The fragment never re-emits
	// them: a duplicate model_name across included files is silently
	// load-balanced by LiteLLM.
	Backends []string           `yaml:"backends"`
	Tiers    Tiers              `yaml:"tiers"`
	Weights  map[string]float64 `yaml:"weights"`
	// Passthrough is opaque YAML merged last into
	// complexity_router_config, for auto-router keys gridctl does not
	// model. Typed keys win: it may not set tiers, and dimension_weights
	// in it is ignored when weights is set.
	Passthrough map[string]any `yaml:"passthrough"`

	Clients Clients `yaml:"clients"`
	Targets Targets `yaml:"targets"`

	// Extra holds unknown top-level keys, preserved for lint and
	// forward compatibility. Never rendered.
	Extra map[string]any `yaml:"-"`

	raw []byte
}

// Router names what clients call and where unclassified requests land.
type Router struct {
	EntryModel  string `yaml:"entry_model"`
	DefaultTier string `yaml:"default_tier"`
}

// Tiers maps each complexity tier to a backend model_name. Scalar
// values only in v1; pool lists belong in passthrough until typed.
type Tiers struct {
	Simple    string `yaml:"SIMPLE"`
	Medium    string `yaml:"MEDIUM"`
	Complex   string `yaml:"COMPLEX"`
	Reasoning string `yaml:"REASONING"`
}

// byName returns the backend assigned to tier, or "".
func (t Tiers) byName(tier string) string {
	switch tier {
	case TierSimple:
		return t.Simple
	case TierMedium:
		return t.Medium
	case TierComplex:
		return t.Complex
	case TierReasoning:
		return t.Reasoning
	}
	return ""
}

// Clients holds the client projection targets.
type Clients struct {
	OpenCode *OpenCodeClient `yaml:"opencode"`
}

// OpenCodeClient wires the OpenCode provider stanza.
type OpenCodeClient struct {
	ProviderID string `yaml:"provider_id"`
	BaseURL    string `yaml:"base_url"`
	// APIKeyEnv names the environment variable holding the LiteLLM key.
	// Only the env reference is ever rendered, never a literal.
	APIKeyEnv string `yaml:"api_key_env"`
	// Schema pins the OpenCode config generation: v1 (provider/npm/
	// options), v2 (providers/package/settings/env), or detect (default:
	// choose by which key the target file already has, else v1).
	Schema string `yaml:"schema"`
	// ConfigPath overrides the default opencode.json location.
	ConfigPath string `yaml:"config_path"`
}

// Targets holds the proxy projection targets.
type Targets struct {
	LiteLLM *LiteLLMTarget `yaml:"litellm"`
}

// LiteLLMTarget names the human-owned parent config and where the
// rendered fragment lives (default: next to the parent).
type LiteLLMTarget struct {
	ConfigPath   string `yaml:"config_path"`
	FragmentPath string `yaml:"fragment_path"`
}

// policyKnownKeys is the authoritative top-level key set; anything else
// lands in Extra.
var policyKnownKeys = map[string]bool{
	"name": true, "kind": true, "description": true,
	"router": true, "backends": true, "tiers": true, "weights": true,
	"passthrough": true, "clients": true, "targets": true,
}

// ParsePolicy decodes a policy document. Unknown top-level keys are
// collected into Extra rather than rejected; validation decides which
// of them are dangerous.
func ParsePolicy(data []byte) (*Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing models policy: %w", err)
	}
	var loose map[string]any
	if err := yaml.Unmarshal(data, &loose); err != nil {
		return nil, fmt.Errorf("parsing models policy: %w", err)
	}
	for k, v := range loose {
		if !policyKnownKeys[k] {
			if p.Extra == nil {
				p.Extra = map[string]any{}
			}
			p.Extra[k] = v
		}
	}
	p.raw = data
	return &p, nil
}

// Hash fingerprints the policy document bytes with the engine's scheme.
func (p *Policy) Hash() string {
	sum := sha256.Sum256(p.raw)
	return project.HashScheme + hex.EncodeToString(sum[:])
}

// HasPolicy reports whether the canonical policy document exists.
func (m *Manager) HasPolicy() bool {
	info, err := os.Stat(m.PolicyPath())
	return err == nil && !info.IsDir()
}

// LoadPolicy reads and parses the canonical policy document.
func (m *Manager) LoadPolicy() (*Policy, error) {
	data, err := os.ReadFile(m.PolicyPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoPolicy
		}
		return nil, fmt.Errorf("reading models policy: %w", err)
	}
	return ParsePolicy(data)
}

// SavePolicy writes the canonical policy document with a backup of the
// previous revision.
func (m *Manager) SavePolicy(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.Dir(), 0755); err != nil {
		return fmt.Errorf("creating models directory: %w", err)
	}
	if _, err := createBackup(m.PolicyPath()); err != nil {
		return err
	}
	return project.AtomicWriteFile(m.PolicyPath(), data)
}

// initWith writes content as the policy document, refusing to
// overwrite an existing one unless forced.
func (m *Manager) initWith(content string, force bool) error {
	if m.HasPolicy() && !force {
		return fmt.Errorf("%w: %s", ErrPolicyExists, m.PolicyPath())
	}
	if _, err := ParsePolicy([]byte(content)); err != nil {
		return err
	}
	return m.SavePolicy([]byte(content))
}
