// Package hooksync projects imported hooks into client hook
// configuration. It is the hook tenant of pkg/project: scripts are copied
// into a gridctl-owned directory per (hook, client), and the client's
// configuration is written with recorded ownership. gridctl never
// executes a hook.
//
// Two channels exist. A merge-key target (Claude Code) owns matcher
// groups inside a shared settings file; the write is an RFC 6902 patch on
// the parsed hujson tree, so bytes outside the patched elements, including
// comments, stay as they were found. A dedicated-file target (OpenCode)
// owns one generated file per hook. The ownership decision table follows
// pkg/wiring and pkg/agentsync, with the identical-value adopt branch
// omitted: an unrecorded group or file is never claimed without --force.
package hooksync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Channels name how a target owns its region. A merge-key target owns
// matcher groups inside a shared settings file, never the file itself. A
// dedicated-file target owns one generated file per hook.
const (
	ChannelMergeKey      = "merge-key"
	ChannelDedicatedFile = "dedicated-file"
)

// Render classes. Identity groups are the definition's groups with
// absolute script paths. A lossy render is a dialect translation that
// names every dropped group and handler key.
const (
	RenderIdentity = "identity"
	RenderLossy    = "lossy"
)

// nameToken is replaced with the hook name in a dedicated-file Path.
const nameToken = "<name>"

// Target describes one client's hook destination.
type Target struct {
	Slug string
	Name string
	// Path is a ~-template expanded against the Manager's home. A
	// merge-key target names its shared settings file. A dedicated-file
	// target carries <name>, replaced with the hook name.
	Path string
	// DetectDirs mark the client as initialized on this machine.
	DetectDirs []string
	// Channel is ChannelMergeKey or ChannelDedicatedFile. Every behavior
	// branch reads it from this table, never from a lockfile entry.
	Channel string
	// RenderClass is RenderIdentity or RenderLossy, reported in status.
	RenderClass string
	// Render is the dialect renderer. Nil is the Claude Code identity render.
	Render RenderFunc
}

// UnsupportedClient is a client with a hook mechanism gridctl does not
// write. Sync reports it; it never creates the native file.
type UnsupportedClient struct {
	Slug     string
	Name     string
	Location string
	// CoveredBy names the target slug whose projection this client reads.
	CoveredBy string
	// The client is detected when any DetectDirs entry is a directory or
	// any DetectFiles entry is a regular file.
	DetectDirs  []string
	DetectFiles []string
}

// Targets returns the projection targets in display order.
func Targets() []Target {
	return []Target{
		{
			Slug:        "claude-code",
			Name:        "Claude Code",
			Path:        "~/.claude/settings.json",
			DetectDirs:  []string{"~/.claude"},
			Channel:     ChannelMergeKey,
			RenderClass: RenderIdentity,
		},
		{
			Slug:        "opencode",
			Name:        "OpenCode",
			Path:        "~/.config/opencode/plugins/gridctl-hook-" + nameToken + ".ts",
			DetectDirs:  []string{"~/.config/opencode"},
			Channel:     ChannelDedicatedFile,
			RenderClass: RenderLossy,
			Render:      renderOpenCode,
		},
	}
}

// UnsupportedClients returns clients that have a hook mechanism and no
// target, in display order. Location is the native path named in
// skipped-unsupported rows and the stderr note. A covered client reads
// the CoveredBy target's projection.
func UnsupportedClients() []UnsupportedClient {
	return []UnsupportedClient{
		{Slug: "gemini", Name: "Gemini CLI", Location: "~/.gemini/settings.json hooks key", DetectFiles: []string{"~/.gemini/settings.json"}},
		{Slug: "cursor", Name: "Cursor", Location: "reads ~/.claude/settings.json hooks (Cursor Settings → Agents → Third-Party Imports, on by default)", CoveredBy: "claude-code", DetectDirs: []string{"~/.cursor"}},
		{Slug: "copilot", Name: "GitHub Copilot", Location: "~/.copilot/hooks/*.json", DetectDirs: []string{"~/.copilot"}},
		{Slug: "windsurf", Name: "Windsurf (Devin Local)", Location: "~/.config/devin/config.json hooks key; reads ~/.claude/settings.json hooks when read_config_from.claude is on (default)", CoveredBy: "claude-code", DetectDirs: []string{"~/.codeium/windsurf", "~/.config/devin", "~/.devin"}},
		{Slug: "antigravity", Name: "Antigravity", Location: "~/.gemini/config/hooks.json", DetectDirs: []string{"~/.gemini/config", "~/.gemini/antigravity"}},
	}
}

// Detail is the skipped-unsupported row detail: the native location, and
// for a covered client the target whose projection it reads.
func (c UnsupportedClient) Detail() string {
	if c.CoveredBy == "" {
		return c.Location
	}
	return "covered by the " + c.CoveredBy + " projection; " + c.Location
}

// CoveredNote is the stderr note for a detected client. A covered client
// names the projection it reads; any other names its native location.
func (c UnsupportedClient) CoveredNote() string {
	if c.CoveredBy != "" {
		path := c.CoveredBy
		if t, ok := FindTarget(c.CoveredBy); ok {
			path = t.Path
		}
		return fmt.Sprintf("note: %s reads the %s projection (%s); no separate target", c.Slug, c.CoveredBy, path)
	}
	return fmt.Sprintf("note: %s (%s) has a hook mechanism but is not a projection target in this release", c.Slug, c.Location)
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

// PathFor returns the target's destination for one hook: the shared
// settings file for a merge-key target, the hook's own file otherwise.
func (t Target) PathFor(home, hookName string) string {
	return expandHome(home, strings.ReplaceAll(t.Path, nameToken, hookName))
}

func (t Target) available(home string) bool {
	return anyDir(home, t.DetectDirs)
}

func (c UnsupportedClient) detected(home string) bool {
	if anyDir(home, c.DetectDirs) {
		return true
	}
	for _, f := range c.DetectFiles {
		if info, err := os.Stat(expandHome(home, f)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func anyDir(home string, dirs []string) bool {
	for _, d := range dirs {
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

// DetectedUnsupported returns unsupported clients detected under the
// manager home. The CLI prints one stderr note per row
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

// scriptDirFor is the gridctl-owned copy of one hook's scripts for one
// client. Claude Code keeps ~/.gridctl/hooks/<name>/; every other target
// owns ~/.gridctl/hooks/<name>@<client>/. "@" is outside the hook name
// charset, so no hook's copy collides with another hook's per-client copy.
// Each (hook, client) record owns the directory it names, so syncing one
// client never changes the bytes another client runs.
func scriptDirFor(home, name, client string) string {
	dir := name
	if client != "claude-code" {
		dir = name + "@" + client
	}
	return filepath.Join(home, ".gridctl", "hooks", dir)
}
