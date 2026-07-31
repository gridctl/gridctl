// Package agentsync projects imported agent definitions into native
// client agent directories (Claude Code's ~/.claude/agents) so agents
// distributed through git repos work in clients that read subagent
// definitions from disk. It is the agent-kind tenant of the pkg/project
// engine: single-file targets with the dedicated-file ownership model
// from pkg/contexts (content hash, backup before replacing anything
// unmanaged, adopt), not pkg/skillsync's directory trees. The render is
// identity: the canonical AGENT.md bytes are copied verbatim.
package agentsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gridctl/gridctl/pkg/project"
)

// Sentinel errors callers branch on.
var (
	ErrUnknownClient = errors.New("unknown client")
	ErrNotAvailable  = errors.New("client not initialized on this machine")
	ErrNotProjected  = errors.New("agent is not projected")
)

// ErrNewerLockVersion signals projection state written by a newer
// gridctl. Aliased from the engine so callers' errors.Is checks work.
var ErrNewerLockVersion = project.ErrNewerLockVersion

// Target describes one client's native agents directory. Each agent
// becomes a single file AgentsPath/<name>.md, always copied (no symlink
// channel: a symlinked file would expose registry sidecar paths to
// client tooling and cannot express the adopt flow).
type Target struct {
	Slug string
	Name string
	// AgentsPath is a ~-template expanded against the Manager's home.
	AgentsPath string
	// DetectDirs mark the client as initialized on this machine. The
	// agents directory itself is created on first sync (Claude Code does
	// not create it by default), but only inside a detected client tree.
	DetectDirs []string
	// Experimental marks the thin-slice tier; surfaced in status output.
	Experimental bool
}

// Targets returns the supported projection targets in display order.
// Claude Code is the only render target in the thin slice; Cursor and
// VS Code Copilot read ~/.claude/agents natively, so this one path
// serves them too.
func Targets() []Target {
	return []Target{
		{
			Slug:         "claude-code",
			Name:         "Claude Code",
			AgentsPath:   "~/.claude/agents",
			DetectDirs:   []string{"~/.claude"},
			Experimental: true,
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
