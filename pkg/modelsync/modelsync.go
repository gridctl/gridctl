// Package modelsync manages the model routing policy (one canonical
// document at <home>/.gridctl/models/policy.yaml) and projects it into
// LiteLLM proxy configuration and client provider config. It is the
// models-kind tenant of the pkg/project engine.
//
// Three ownership mechanisms, deliberately different per target:
//
//   - The rendered LiteLLM router fragment is a wholly gridctl-owned
//     file (wholesale ownership, contexts-style installed/canonical
//     hashes). The fragment carries only the auto-router model entry:
//     backends stay in the user's own model_list, referenced by name,
//     because LiteLLM's include directive extends model_list across
//     files and a re-emitted backend would silently load-balance
//     against the original.
//   - The include: line referencing the fragment from the human-owned
//     LiteLLM config is a single-line text edit (the contexts import-
//     shim pattern adapted to YAML): the parent file is never parsed
//     and re-marshalled, so its comments and formatting survive
//     byte-for-byte outside the one managed line.
//   - The provider subtree inside the human-owned opencode.json is a
//     key-level ownership write (the wiring pattern: canonical value
//     hashes with history, foreign and drifted refusals), applied as an
//     RFC 6902 patch through hujson so every byte outside the owned
//     subtree survives, comments included.
//
// gridctl stays off the inference data path: every operation here is a
// pure file operation, no running gateway or LiteLLM is required, and
// if gridctl is down inference is unaffected. LiteLLM only reads its
// config at startup, so sync latches a restart-pending state
// (Entry.AckedHash) that only an explicit ack clears.
package modelsync

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/state"
)

// Sentinel errors callers branch on.
var (
	ErrNoPolicy     = errors.New("no models policy; run 'gridctl models init' first")
	ErrPolicyExists = errors.New("models policy already exists (use --force to overwrite)")
	ErrNotSynced    = errors.New("nothing synced for this target")
	// ErrNewerLockVersion is aliased from the engine so callers'
	// errors.Is checks work without importing pkg/project.
	ErrNewerLockVersion = project.ErrNewerLockVersion
)

// policyFileName is the canonical policy document name under Dir().
const policyFileName = "policy.yaml"

// defaultFragmentName is the rendered LiteLLM fragment's file name when
// the policy does not name one; it sits next to the parent config so
// the include reference stays a bare relative path.
const defaultFragmentName = "gridctl-models.yaml"

// Projection sources (Entry.Source) and clients (Entry.Client).
const (
	srcFragment = "litellm-fragment"
	srcInclude  = "litellm-include"
	srcOpenCode = "opencode"

	clientLiteLLM  = "litellm"
	clientOpenCode = "opencode"
)

// Manager owns the models policy store and every projection decision.
// All paths resolve against home, so tests point it at a temp dir.
// Mutating operations serialize on mu in-process and on the engine's
// cross-process lock.
type Manager struct {
	home  string
	store *project.Store
	mu    sync.Mutex
}

// NewManager builds a Manager rooted at the user's home directory. CLI
// call sites only; anything tests can reach uses NewManagerWithHome.
func NewManager() (*Manager, error) {
	home, err := state.Home()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return NewManagerWithHome(home), nil
}

// NewManagerWithHome builds a Manager rooted at an explicit home
// directory. Tests use this to stay isolated from $HOME.
func NewManagerWithHome(home string) *Manager {
	return &Manager{home: home, store: project.NewStore(home)}
}

// Dir returns the canonical models store directory.
func (m *Manager) Dir() string {
	return filepath.Join(m.home, ".gridctl", "models")
}

// PolicyPath returns the canonical policy document path.
func (m *Manager) PolicyPath() string {
	return filepath.Join(m.Dir(), policyFileName)
}

// LockPath returns the unified projection lockfile path.
func (m *Manager) LockPath() string {
	return m.store.Path()
}

// expandPath resolves a policy-declared path against the manager's
// home. "~/x" is home-relative; absolute paths pass through cleaned;
// anything else is refused so a cwd-dependent path can never leak into
// the lockfile.
func (m *Manager) expandPath(p string) (string, error) {
	switch {
	case p == "":
		return "", errors.New("empty path")
	case p == "~":
		return m.home, nil
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(m.home, p[2:]), nil
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	}
	return "", fmt.Errorf("path %q must be absolute or ~/-relative", p)
}

// FragmentPath resolves where the rendered LiteLLM fragment lives:
// the policy's explicit fragment_path, else next to the parent config,
// else under the models store (render-only, nothing to include it from).
func (m *Manager) FragmentPath(p *Policy) (string, error) {
	if p.Targets.LiteLLM != nil && p.Targets.LiteLLM.FragmentPath != "" {
		return m.expandPath(p.Targets.LiteLLM.FragmentPath)
	}
	if p.Targets.LiteLLM != nil && p.Targets.LiteLLM.ConfigPath != "" {
		parent, err := m.expandPath(p.Targets.LiteLLM.ConfigPath)
		if err != nil {
			return "", err
		}
		return filepath.Join(filepath.Dir(parent), defaultFragmentName), nil
	}
	return filepath.Join(m.Dir(), defaultFragmentName), nil
}

// opencodeConfigPath resolves the OpenCode config location: the
// policy's override, else the standard per-home location. Home-relative
// on every platform so GRIDCTL_HOME isolation covers it.
func (m *Manager) opencodeConfigPath(oc *OpenCodeClient) (string, error) {
	if oc.ConfigPath != "" {
		return m.expandPath(oc.ConfigPath)
	}
	return filepath.Join(m.home, ".config", "opencode", "opencode.json"), nil
}
