package contexts

import (
	"context"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// ErrNewerLockVersion signals projection state written by a newer
// gridctl. Aliased from the engine so callers' errors.Is checks keep
// working across the pkg/project extraction.
var ErrNewerLockVersion = project.ErrNewerLockVersion

// LockFile is the context-kind view over the unified project lockfile:
// the per-client sync records, keyed by client slug exactly as the
// legacy context.lock.yaml was. The engine owns the on-disk schema,
// versioning, migration, and locking; this view exists so the ops code
// keeps its shape.
type LockFile struct {
	Version int
	// Scope is "global" today, recorded as the engine entry's source.
	Scope   string
	Clients map[string]*ClientEntry
}

// ClientEntry is one client's sync record. Drift is judged against
// InstalledHash; staleness against CanonicalHash.
type ClientEntry struct {
	Strategy string
	// Target is the absolute path gridctl wrote to.
	Target string
	// InstalledHash is the managed-region hash exactly as written.
	InstalledHash string
	// CanonicalHash is the canonical file's hash at sync time.
	CanonicalHash string
	// CreatedFile records whether gridctl created the target file itself
	// (unsync then removes the whole file, not just the managed region).
	CreatedFile bool
	SyncedAt    time.Time
}

// viewFromLock projects the engine lock's context entries into the
// legacy-shaped view the ops code works on.
func viewFromLock(l *project.Lock) *LockFile {
	lf := &LockFile{Version: project.LockVersion, Scope: "global", Clients: map[string]*ClientEntry{}}
	for _, e := range l.Entries(project.KindContext) {
		if e.Source != "" {
			lf.Scope = e.Source
		}
		lf.Clients[e.Client] = &ClientEntry{
			Strategy:      e.Strategy,
			Target:        e.Path,
			InstalledHash: e.InstalledHash,
			CanonicalHash: e.CanonicalHash,
			CreatedFile:   e.CreatedFile,
			SyncedAt:      e.SyncedAt,
		}
	}
	return lf
}

// saveView flushes the view back into the engine lock and persists it.
// Clients dropped from the view are removed as explicit engine-driven
// deletes; entries of other kinds, unknown file-level fields, and
// unknown per-entry fields (carried forward by Lock.Set) ride along
// untouched.
func saveView(l *project.Lock, lf *LockFile) error {
	var entries []*project.Entry
	for slug, e := range lf.Clients {
		entries = append(entries, &project.Entry{
			Kind:          project.KindContext,
			Client:        slug,
			Source:        lf.Scope,
			Path:          e.Target,
			Strategy:      e.Strategy,
			InstalledHash: e.InstalledHash,
			CanonicalHash: e.CanonicalHash,
			CreatedFile:   e.CreatedFile,
			SyncedAt:      e.SyncedAt,
		})
	}
	if err := l.ReplaceKind(project.KindContext, entries); err != nil {
		return err
	}
	return l.Save()
}

// loadView returns a read-only context view of the projection state.
func (m *Manager) loadView(ctx context.Context) (*LockFile, error) {
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return viewFromLock(l), nil
}
