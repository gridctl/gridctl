// Package agentsync projects imported agent definitions into native
// client agent directories so agents distributed through git repos work
// in clients that read subagent definitions from disk. It is the
// agent-kind tenant of the pkg/project engine: single-file targets with
// the dedicated-file ownership model from pkg/contexts (content hash,
// backup before replacing anything unmanaged, adopt).
//
// Two render classes exist. The claude-code target is identity: the
// canonical AGENT.md bytes are copied verbatim. (Cursor reads
// ~/.claude/agents too, so it needs no target of its own — see Targets
// for how that was verified.) The
// opencode, copilot, and gemini targets render the canonical definition
// into each client's dialect; those renders are lossy (unmappable
// frontmatter keys are dropped and reported), deterministic, and
// one-way — adopt is refused on rendered targets.
package agentsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// Sentinel errors callers branch on.
var (
	ErrUnknownClient = errors.New("unknown client")
	ErrUnknownAgent  = errors.New("unknown agent")
	ErrNotAvailable  = errors.New("client not initialized on this machine")
	ErrNotProjected  = errors.New("agent is not projected")
)

// ErrNewerLockVersion signals projection state written by a newer
// gridctl. Aliased from the engine so callers' errors.Is checks work.
var ErrNewerLockVersion = project.ErrNewerLockVersion

// Rendered is one render's output: the client-native bytes plus the
// canonical frontmatter keys the dialect could not express. Dropped is
// surfaced in status detail and dry-run output so lossy conversions are
// never silent.
type Rendered struct {
	Bytes   []byte
	Dropped []string
}

// RenderFunc converts a parsed canonical agent definition into one
// client's native dialect. Renders must be pure and deterministic:
// the same definition always yields the same bytes, or drift detection
// manufactures false positives on every sync.
type RenderFunc func(def *skills.AgentDefinition) (Rendered, error)

// Target describes one client's native agents directory. Each agent
// becomes a single file AgentsPath/<file name>, always copied (no
// symlink channel: a symlinked file would expose registry sidecar paths
// to client tooling and cannot express the adopt flow).
type Target struct {
	Slug string
	Name string
	// AgentsPath is a ~-template expanded against the Manager's home.
	AgentsPath string
	// DetectDirs mark the client as initialized on this machine. The
	// agents directory itself is created on first sync, but only inside
	// a detected client tree.
	DetectDirs []string
	// Experimental marks the target's tier; surfaced in status output.
	Experimental bool
	// Render converts the canonical definition into the client dialect.
	// Nil means identity: the canonical bytes are copied verbatim.
	Render RenderFunc
	// FileName maps an agent name to the target's file name. Nil means
	// "<name>.md" (Copilot requires "<name>.agent.md").
	FileName func(name string) string
}

// fileName resolves the destination file name for one agent.
func (t Target) fileName(name string) string {
	if t.FileName != nil {
		return t.FileName(name)
	}
	return name + ".md"
}

// renderKind labels the target's render class for status output.
func (t Target) renderKind() string {
	if t.Render == nil {
		return "identity"
	}
	return "lossy"
}

// Targets returns the supported projection targets in display order.
// claude-code is the identity target, and Cursor rides along on it: no
// Cursor target exists because Cursor reads ~/.claude/agents directly.
// Verified against the shipped Cursor bundle rather than its docs — a
// path predicate matching ".claude/agents/" feeds Cursor's subagent
// descriptors and gates their deletable flag off, so Cursor lists the
// agents and treats the files as read-only. Client formats churn, so
// re-check that predicate before treating the free ride as permanent.
// The rendered
// targets convert into each client's own dialect. VS Code Copilot does NOT read
// ~/.claude/agents — its global agents live under ~/.copilot/agents,
// which is why it needs a render target.
func Targets() []Target {
	return []Target{
		{
			Slug:         "claude-code",
			Name:         "Claude Code",
			AgentsPath:   "~/.claude/agents",
			DetectDirs:   []string{"~/.claude"},
			Experimental: true,
		},
		{
			Slug:         "opencode",
			Name:         "OpenCode",
			AgentsPath:   "~/.config/opencode/agents",
			DetectDirs:   []string{"~/.config/opencode"},
			Experimental: true,
			Render:       renderOpenCode,
		},
		{
			Slug:         "copilot",
			Name:         "GitHub Copilot",
			AgentsPath:   "~/.copilot/agents",
			DetectDirs:   []string{"~/.copilot"},
			Experimental: true,
			Render:       renderCopilot,
			FileName:     func(name string) string { return name + ".agent.md" },
		},
		{
			Slug:         "gemini",
			Name:         "Gemini CLI",
			AgentsPath:   "~/.gemini/agents",
			DetectDirs:   []string{"~/.gemini"},
			Experimental: true,
			Render:       renderGemini,
		},
	}
}

// FindTarget returns the projection target for slug.
func FindTarget(slug string) (Target, bool) {
	for _, t := range Targets() {
		if t.Slug == slug {
			return t, true
		}
	}
	return Target{}, false
}

// SupportedSlugs lists the target slugs, derived from the table so
// error messages never go stale.
func SupportedSlugs() []string {
	targets := Targets()
	slugs := make([]string, len(targets))
	for i, t := range targets {
		slugs[i] = t.Slug
	}
	return slugs
}

// expandHome resolves a ~-template against home (mirrors pkg/skillsync).
func expandHome(home, template string) string {
	if len(template) > 0 && template[0] == '~' {
		return filepath.Join(home, template[1:])
	}
	return template
}

// agentsDir resolves the target's agents directory against home.
func (t Target) agentsDir(home string) string {
	return expandHome(home, t.AgentsPath)
}

// available reports whether agents may be projected to this target:
// when any detect dir exists (the client is initialized on this
// machine). The agents subdirectory itself is created on sync.
func (t Target) available(home string) bool {
	for _, d := range t.DetectDirs {
		if info, err := os.Stat(expandHome(home, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Manager owns agent projections and every write into client agent
// directories. All target paths resolve against home; the canonical
// agent store lives under registryDir/agents. Mutating operations
// serialize on mu in-process and on the engine's cross-process lock.
type Manager struct {
	home        string
	registryDir string
	store       *project.Store
	mu          sync.Mutex

	// modelPolicy is the optional compiled `model_preferences.agents`
	// scope, installed by the controller (daemon) or a --stack-carrying
	// CLI invocation. nil means no stack context: pass-through for new
	// projections, preserve for projections a policy previously rewrote.
	modelPolicy *registry.ModelPolicy
}

// NewManager builds a Manager rooted at the user's home directory. It
// is for end-of-the-line CLI call sites only: any caller in pkg/ or
// internal/ that tests can reach must use NewManagerWithHome so an
// injected home keeps the suite away from real client agent
// directories.
func NewManager(registryDir string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return NewManagerWithHome(home, registryDir), nil
}

// NewManagerWithHome builds a Manager rooted at an explicit home
// directory. Tests use this to stay isolated from $HOME.
func NewManagerWithHome(home, registryDir string) *Manager {
	return &Manager{home: home, registryDir: registryDir, store: project.NewStore(home)}
}

// LockPath returns the unified projection lockfile path.
func (m *Manager) LockPath() string {
	return m.store.Path()
}

// HasProjections reports whether any agent is currently projected.
func (m *Manager) HasProjections(ctx context.Context) (bool, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return false, err
	}
	return len(lf.Projections) > 0, nil
}
