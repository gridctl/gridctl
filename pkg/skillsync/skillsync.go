// Package skillsync projects active registry skills into native client
// skill directories (Claude Code's ~/.claude/skills, the vendor-neutral
// ~/.agents/skills interop dir, Antigravity's ~/.gemini/config/skills) so
// gridctl-managed skills are usable in clients that never fetch MCP
// prompts and auto-trigger in clients that read skills from disk. It is
// the directory-projection sibling of pkg/contexts: a per-client target
// table, a machine-global lockfile with ownership tracking, and
// sync/status/unsync operations. Every operation is a pure file
// operation; no running gateway is required. The MCP prompt channel is
// untouched: projection and prompts are complementary per-client
// delivery channels.
package skillsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gridctl/gridctl/pkg/registry"
)

// Sentinel errors callers branch on.
var (
	ErrUnknownClient = errors.New("unknown client")
	ErrNotAvailable  = errors.New("client not initialized on this machine")
	ErrNotProjected  = errors.New("skill is not projected")
)

const lockFileName = "skillsync.lock.yaml"

// SkillSource is the slice of the registry store projection reads. The
// concrete *registry.Store satisfies it; tests can substitute a fake.
type SkillSource interface {
	// GetSkill returns a skill by name (a copy).
	GetSkill(name string) (*registry.AgentSkill, error)
	// ActiveSkills returns skills with state "active" (copies).
	ActiveSkills() []*registry.AgentSkill
	// Dir returns the registry base directory (skills live under
	// Dir()/skills).
	Dir() string
}

// Manager owns the projection lockfile under <home>/.gridctl and every
// write into client skill directories. All target paths resolve against
// home, so tests point it at a temp dir. Mutating operations serialize
// on mu in-process and on a flock file across processes (the CLI and the
// daemon reconcile can race).
type Manager struct {
	home  string
	store SkillSource
	mu    sync.Mutex
}

// NewManager builds a Manager rooted at the user's home directory.
func NewManager(store SkillSource) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return NewManagerWithHome(home, store), nil
}

// NewManagerWithHome builds a Manager rooted at an explicit home
// directory. Tests use this to stay isolated from $HOME.
func NewManagerWithHome(home string, store SkillSource) *Manager {
	return &Manager{home: home, store: store}
}

// LockPath returns the projection lockfile path
// (<home>/.gridctl/skillsync.lock.yaml, a sibling of the registry).
func (m *Manager) LockPath() string {
	return filepath.Join(m.home, ".gridctl", lockFileName)
}

// HasProjections reports whether any skill is currently projected. The
// daemon reconcile uses it as a cheap no-op guard.
func (m *Manager) HasProjections() (bool, error) {
	lf, err := readLockFile(m.LockPath())
	if err != nil {
		return false, err
	}
	return len(lf.Projections) > 0, nil
}

// skillSourceDir returns the registry directory holding one skill.
func (m *Manager) skillSourceDir(sk *registry.AgentSkill) string {
	dir := sk.Name
	if sk.Dir != "" {
		dir = sk.Dir
	}
	return filepath.Join(m.store.Dir(), "skills", dir)
}
