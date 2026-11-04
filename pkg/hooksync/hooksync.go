// Package hooksync projects imported hooks into Claude Code settings.
// It is the hook tenant of pkg/project: scripts are copied into a
// gridctl-owned directory, and matcher groups are patched into the
// hooks key of ~/.claude/settings.json with recorded, region-level
// ownership. gridctl never executes a hook.
//
// The ownership decision table follows pkg/wiring, with the
// identical-value adopt branch omitted. An unrecorded group is never
// claimed without --force. The settings write is an RFC 6902 patch on
// the parsed hujson tree, so bytes outside the patched elements,
// including comments, stay as they were found.
package hooksync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/state"
)

// Sentinel errors callers branch on.
var (
	ErrUnknownClient = errors.New("unknown client")
	ErrUnknownHook   = errors.New("unknown hook")
	ErrNotProjected  = errors.New("hook is not projected")
)

// ErrNewerLockVersion signals projection state written by a newer
// gridctl. Aliased from the engine so callers' errors.Is checks work.
var ErrNewerLockVersion = project.ErrNewerLockVersion

// ChannelMergeKey is the only hook channel: gridctl owns matcher groups
// inside a shared settings file, never the file itself.
const ChannelMergeKey = "merge-key"

// RenderIdentity is the only render class. The projected groups are the
// definition's groups with absolute script paths, not a dialect translation.
const RenderIdentity = "identity"

// Target describes one client's settings file. Later dialect targets are
// additive: this release has Claude Code only.
type Target struct {
	Slug string
	Name string
	// SettingsPath is a ~-template expanded against the Manager's home.
	SettingsPath string
	// DetectDirs mark the client as initialized on this machine.
	DetectDirs []string
}

// UnsupportedClient is a client with a hook mechanism this release does
// not write. Sync reports it; it never creates the native file.
type UnsupportedClient struct {
	Slug       string
	Name       string
	Location   string
	DetectDirs []string
}

// Targets returns the projection targets in display order.
func Targets() []Target {
	return []Target{
		{
			Slug:         "claude-code",
			Name:         "Claude Code",
			SettingsPath: "~/.claude/settings.json",
			DetectDirs:   []string{"~/.claude"},
		},
	}
}

// UnsupportedClients returns clients that have a hook mechanism and no
// target in this release, in display order. Location is the native path
// named in skipped-unsupported rows and the stderr note.
func UnsupportedClients() []UnsupportedClient {
	return []UnsupportedClient{
		{Slug: "gemini", Name: "Gemini CLI", Location: "~/.gemini/settings.json hooks key", DetectDirs: []string{"~/.gemini"}},
		{Slug: "cursor", Name: "Cursor", Location: "~/.cursor/hooks.json", DetectDirs: []string{"~/.cursor"}},
		{Slug: "copilot", Name: "GitHub Copilot", Location: "~/.copilot/hooks/*.json", DetectDirs: []string{"~/.copilot"}},
		{Slug: "windsurf", Name: "Windsurf", Location: "~/.codeium/windsurf/hooks.json", DetectDirs: []string{"~/.codeium/windsurf"}},
		{Slug: "antigravity", Name: "Antigravity", Location: "~/.gemini/config/hooks.json", DetectDirs: []string{"~/.gemini/config", "~/.gemini/antigravity"}},
		{Slug: "opencode", Name: "OpenCode", Location: "plugins only (~/.config/opencode/plugins/)", DetectDirs: []string{"~/.config/opencode"}},
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

// FindUnsupported returns a known non-target client.
func FindUnsupported(slug string) (UnsupportedClient, bool) {
	for _, c := range UnsupportedClients() {
		if c.Slug == slug {
			return c, true
		}
	}
	return UnsupportedClient{}, false
}

// KnownSlugs lists target and unsupported slugs, for the unknown-client error.
func KnownSlugs() []string {
	var slugs []string
	for _, t := range Targets() {
		slugs = append(slugs, t.Slug)
	}
	for _, c := range UnsupportedClients() {
		slugs = append(slugs, c.Slug)
	}
	return slugs
}

// expandHome resolves a ~-template against home. It does not expand
// environment variables or %USERPROFILE%.
func expandHome(home, template string) string {
	if len(template) > 0 && template[0] == '~' {
		return filepath.Join(home, template[1:])
	}
	return template
}

func (t Target) settingsPath(home string) string {
	return expandHome(home, t.SettingsPath)
}

func (t Target) available(home string) bool {
	for _, d := range t.DetectDirs {
		if info, err := os.Stat(expandHome(home, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (c UnsupportedClient) detected(home string) bool {
	for _, d := range c.DetectDirs {
		if info, err := os.Stat(expandHome(home, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Manager owns hook projections. All target paths resolve against home.
// Mutating operations serialize on mu and on the engine's cross-process lock.
type Manager struct {
	home        string
	registryDir string
	store       *project.Store
	pins        *hookpins.Store
	mu          sync.Mutex
}

// NewManager builds a Manager rooted at the user's home directory. It is
// for end-of-the-line CLI call sites only: any caller in pkg/ or
// internal/ that tests can reach must use NewManagerWithHome.
func NewManager(registryDir string) (*Manager, error) {
	home, err := state.Home()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return NewManagerWithHome(home, registryDir), nil
}

// NewManagerWithHome builds a Manager rooted at an explicit home.
// Tests use this to stay isolated from $HOME.
func NewManagerWithHome(home, registryDir string) *Manager {
	return &Manager{
		home:        home,
		registryDir: registryDir,
		store:       project.NewStore(home),
		pins:        hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins")),
	}
}

// LockPath returns the unified projection lockfile path.
func (m *Manager) LockPath() string {
	return m.store.Path()
}

// Home returns the manager home. Pack status uses it to join rows.
func (m *Manager) Home() string {
	return m.home
}

// HasProjections reports whether any hook is currently projected.
func (m *Manager) HasProjections(ctx context.Context) (bool, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return false, err
	}
	return len(lf.Records) > 0, nil
}

// DetectedUnsupported returns unsupported clients whose detect directory
// exists under the manager home. The CLI prints one stderr note per row
// when sync was not given --clients. Pack apply turns the same list into
// skipped-unsupported rows.
func (m *Manager) DetectedUnsupported() []UnsupportedClient {
	var out []UnsupportedClient
	for _, c := range UnsupportedClients() {
		if c.detected(m.home) {
			out = append(out, c)
		}
	}
	return out
}

// scriptDirFor is the gridctl-owned copy of one hook's scripts.
func scriptDirFor(home, name string) string {
	return filepath.Join(home, ".gridctl", "hooks", name)
}
