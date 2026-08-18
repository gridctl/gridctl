// Package wiring records ownership of the gateway entries gridctl
// merges into client MCP configs (the `gridctl link` surface). It is the
// wiring-kind tenant of the pkg/project engine, with key-level ownership
// inside files gridctl does not otherwise own: every link records the
// (client, config path, entry name) it wrote plus a canonical hash of
// the written value, so unlink, drift, adopt, and doctor are decided
// from recorded state, never inferred from entry shape (Article XVI).
// All client-format knowledge stays in pkg/provisioner; this package
// only decides ownership and delegates the byte-level work.
package wiring

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/gowebpki/jcs"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/state"
)

// DefaultServerName is the config entry key gridctl writes when no
// group-specific name applies.
const DefaultServerName = "gridctl"

// Sentinel errors callers branch on.
var (
	// ErrForeign marks an entry at gridctl's name that gridctl never
	// recorded. It is never deleted, and only overwritten with --force.
	ErrForeign = errors.New("entry was not recorded by gridctl")
	// ErrDrifted marks a recorded entry whose current value matches no
	// recorded hash: it was edited after gridctl wrote it.
	ErrDrifted = errors.New("entry was edited since gridctl wrote it")
	// ErrNotRecorded marks an unlink of something neither present nor
	// recorded.
	ErrNotRecorded = errors.New("nothing recorded for this client entry")
	// ErrNothingToAdopt marks an adopt of an entry that does not exist.
	ErrNothingToAdopt = errors.New("nothing to adopt")
	// ErrCannotPlan marks a provisioner that cannot report its planned
	// entry value; ownership hashing is impossible without it.
	ErrCannotPlan = errors.New("provisioner cannot plan its entry value")
	// ErrUnknownClient marks a slug the provisioner registry does not know.
	ErrUnknownClient = errors.New("unknown client")
	// ErrNotDetected marks a known client with no config detected on this
	// system.
	ErrNotDetected = errors.New("client is not detected on this system")
)

// maxHashHistory bounds the per-entry list of canonical hashes gridctl
// has written. History exists so a newer gridctl changing its own
// written shape never presents the old shape as user drift (the ucf
// lesson); five generations is far more than two release cycles need.
const maxHashHistory = 5

// Manager owns wiring records and every ownership decision around
// client config entries. Mutating operations serialize on mu in-process
// and on the engine's cross-process lock.
type Manager struct {
	home     string
	registry *provisioner.Registry
	store    *project.Store
	mu       sync.Mutex
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
// directory, with the full client registry.
func NewManagerWithHome(home string) *Manager {
	return NewManagerWith(home, provisioner.NewRegistry())
}

// NewManagerWith builds a Manager with an explicit registry. Tests use
// it to drive ownership decisions with fake clients.
func NewManagerWith(home string, registry *provisioner.Registry) *Manager {
	return &Manager{home: home, registry: registry, store: project.NewStore(home)}
}

// LockPath returns the unified projection lockfile path.
func (m *Manager) LockPath() string {
	return m.store.Path()
}

// Registry exposes the client registry the manager decides over.
func (m *Manager) Registry() *provisioner.Registry {
	return m.registry
}

// ValueHash canonicalizes an entry value per RFC 8785 and hashes it
// with the engine's scheme. Values decoded from TOML or YAML configs
// hash identically to their JSON form, so a client rewriting its file
// in a different style never reads as drift. Only the hash is ever
// stored: entry values can carry secrets in env blocks.
func ValueHash(value map[string]any) (string, error) {
	data, err := json.Marshal(normalizeValue(value))
	if err != nil {
		return "", fmt.Errorf("encoding entry value: %w", err)
	}
	canonical, err := jcs.Transform(data)
	if err != nil {
		return "", fmt.Errorf("canonicalizing entry value: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return project.HashScheme + hex.EncodeToString(sum[:]), nil
}

// normalizeValue converts decoder-specific map shapes (yaml.v2-style
// map[any]any keys) into JSON-encodable ones. Non-string keys are
// stringified; the known client shapes never produce them.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalizeValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprintf("%v", k)] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeValue(item)
		}
		return out
	default:
		return v
	}
}

// hashRecorded reports whether hash is one gridctl wrote.
func hashRecorded(hashes []string, hash string) bool {
	for _, h := range hashes {
		if h == hash {
			return true
		}
	}
	return false
}

// appendHash appends hash to the history, deduplicating and keeping the
// newest maxHashHistory entries.
func appendHash(hashes []string, hash string) []string {
	kept := make([]string, 0, len(hashes)+1)
	for _, h := range hashes {
		if h != hash {
			kept = append(kept, h)
		}
	}
	kept = append(kept, hash)
	if len(kept) > maxHashHistory {
		kept = kept[len(kept)-maxHashHistory:]
	}
	return kept
}

// currentValue finds the named entry in the client's config via the
// provisioner's read-only enumeration. The bool reports presence; a
// missing or empty config is present=false with no error.
func currentValue(prov provisioner.ClientProvisioner, configPath, name string) (map[string]any, bool, error) {
	entries, err := prov.ListServers(configPath)
	if err != nil {
		return nil, false, err
	}
	for _, e := range entries {
		if e.Name == name {
			return e.Raw, true, nil
		}
	}
	return nil, false, nil
}

// plannedHash computes the canonical hash of what prov's Link would
// write for opts.
func plannedHash(prov provisioner.ClientProvisioner, opts provisioner.LinkOptions) (string, error) {
	planned := provisioner.PlannedEntry(prov, opts)
	if planned == nil {
		return "", fmt.Errorf("%w: %s", ErrCannotPlan, prov.Slug())
	}
	return ValueHash(planned)
}

// rebuildOptions reconstructs the LinkOptions a recorded entry was
// written with, against the current gateway port, so status can judge
// staleness without re-reading flags.
func rebuildOptions(e *Entry, name string, port int) provisioner.LinkOptions {
	base := provisioner.GatewayHTTPURL(port)
	if e.Group != "" {
		base = provisioner.GroupGatewayHTTPURL(port, e.Group)
	}
	return provisioner.LinkOptions{
		GatewayURL: provisioner.AppendClientParam(base, e.ClientID),
		Port:       port,
		ServerName: name,
		ClientID:   e.ClientID,
		Group:      e.Group,
	}
}

// loadView returns a read-only wiring view of the recorded state.
func (m *Manager) loadView(ctx context.Context) (*LockFile, error) {
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return viewFromLock(l), nil
}
