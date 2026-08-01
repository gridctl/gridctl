// Package project is the generic projection engine behind pkg/skillsync
// and pkg/contexts: "project canonical content into per-client locations
// with lockfile-tracked ownership." The engine owns the unified lockfile
// (schema, two-tier versioning, migration from the two legacy lockfiles,
// cross-process locking) and the shared vocabulary (states, dry-run
// actions, hash-scheme prefix, atomic writes, backup pruning).
//
// A kind yields a set of (source, target) projection keys: the contexts
// adapter records exactly one source ("global") fanned to N clients; the
// skills adapter records one source per projected skill. Everything the
// two kinds do differently on purpose stays in the kind packages:
// target tables, channel/strategy resolution, hashing (tree vs content),
// backup placement, status enumeration mode, rendering, and remediation
// text. The engine deliberately does not force a uniform Target or a
// shared sync loop; the frozen CLI contracts are per-kind and the
// characterization tests in cmd/gridctl arbitrate.
package project

import (
	"errors"
	"sort"
	"time"
)

// Kind identifies a projection tenant.
type Kind string

const (
	// KindSkill projects registry skill directories into client skill
	// dirs (pkg/skillsync).
	KindSkill Kind = "skill"
	// KindContext projects the canonical global context file into client
	// context locations (pkg/contexts).
	KindContext Kind = "context"
	// KindAgent projects imported agent definitions into client agent
	// directories as single files (pkg/agentsync).
	KindAgent Kind = "agent"
	// KindWiring records ownership of gateway entries merged into client
	// MCP configs (pkg/wiring): key-level ownership inside files gridctl
	// does not otherwise own, per Article XVI.
	KindWiring Kind = "wiring"
)

// Projection states shared by every kind. Kinds may extend the
// vocabulary (contexts adds "unsupported" and "never-synced").
const (
	StateInSync        = "in-sync"
	StateStale         = "stale"
	StateDrifted       = "drifted"
	StateTargetMissing = "target-missing"
)

// Actions shared by every kind's sync results. Kind-specific actions
// (linked, copied, created, ...) stay in the kind packages.
const (
	ActionUpdated            = "updated"
	ActionUnchanged          = "unchanged"
	ActionError              = "error"
	ActionSkippedDrift       = "skipped-drift"
	ActionSkippedUnavailable = "skipped-unavailable"
	ActionWouldUpdate        = "would-update"
)

// HashScheme prefixes every stored hash so a future scheme change never
// presents as false drift (the pkg/pins lesson).
const HashScheme = "sha256:"

// ErrNewerLockVersion signals a lockfile written by a newer gridctl.
// Callers must never paper over it: acting on state a newer version
// wrote risks silent data loss.
var ErrNewerLockVersion = errors.New("project lockfile was written by a newer gridctl version")

// ErrPathConflict signals two projections claiming the same destination
// path. The unified lockfile exists to enforce the opposite invariant:
// one destination has exactly one owner.
var ErrPathConflict = errors.New("destination path is already owned by another projection")

// Entry is one recorded (kind, source, client) projection. The primary
// key is (client, path): one destination path has exactly one owner.
// The kind-specific attribute fields form a union across the two kinds;
// the engine stores them but never interprets them, and each kind's
// adapter reads only its own. Extra preserves fields this binary does
// not understand, so a revision bump by a newer gridctl survives a
// rewrite by this one (Article XVII).
type Entry struct {
	Kind   Kind   `yaml:"kind"`
	Client string `yaml:"client"`
	// Source names what is projected: the skill name for KindSkill, the
	// scope ("global") for KindContext.
	Source string `yaml:"source"`
	// Path is the absolute destination path gridctl wrote or created.
	Path string `yaml:"path"`

	// KindSkill attributes.
	Channel          string `yaml:"channel,omitempty"`
	CreatedByGridctl bool   `yaml:"created_by_gridctl,omitempty"`
	TreeHash         string `yaml:"tree_hash,omitempty"`

	// KindContext attributes.
	Strategy      string `yaml:"strategy,omitempty"`
	InstalledHash string `yaml:"installed_hash,omitempty"`
	CanonicalHash string `yaml:"canonical_hash,omitempty"`
	CreatedFile   bool   `yaml:"created_file,omitempty"`

	// KindWiring attributes. Path is the composite "<config path>#<entry
	// name>" (one config file legitimately holds several owned entries, and
	// the one-owner invariant keys on the full Path); ConfigPath is the real
	// file path so no consumer parses the composite. Hashes is the short
	// history of canonical value hashes gridctl wrote, newest last, so a
	// shape change by a newer gridctl never reads as user drift.
	ConfigPath string   `yaml:"config_path,omitempty"`
	Group      string   `yaml:"group,omitempty"`
	ClientID   string   `yaml:"client_id,omitempty"`
	Hashes     []string `yaml:"hashes,omitempty"`

	SyncedAt time.Time `yaml:"synced_at"`

	Extra map[string]any `yaml:",inline"`
}

// key identifies one projection for lookups.
type key struct {
	kind   Kind
	client string
	source string
}

// sortEntries orders entries deterministically (kind, source, client)
// so the lockfile is stable across rewrites.
func sortEntries(entries []*Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Client < b.Client
	})
}
