package skillsync

import (
	"context"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// ErrNewerLockVersion signals projection state written by a newer
// gridctl. Aliased from the engine so callers' errors.Is checks keep
// working across the pkg/project extraction.
var ErrNewerLockVersion = project.ErrNewerLockVersion

// LockFile is the skill-kind view over the unified project lockfile:
// what gridctl last projected, keyed skill name → client slug exactly
// as the legacy skillsync.lock.yaml was. Ownership (CreatedByGridctl)
// is what lets sync refuse to clobber foreign paths and lets unsync
// remove only gridctl's own artifacts. The engine owns the on-disk
// schema, versioning, migration, and locking.
type LockFile struct {
	Version int
	// Projections maps skill name → client slug → entry.
	Projections map[string]map[string]*Entry
}

// Entry is one (skill, client) projection record.
type Entry struct {
	// Channel is "symlink" or "copy".
	Channel Channel
	// Target is the absolute path gridctl created (the symlink itself or
	// the copied directory).
	Target string
	// CreatedByGridctl marks the path as gridctl-owned. Always true for
	// recorded entries; adopt reads it to tell managed copies apart.
	CreatedByGridctl bool
	// TreeHash is the copied directory's tree hash at sync time (empty
	// for symlinks, whose content lives in the registry).
	TreeHash string
	// Pack tags the projection with the pack that applied it (empty =
	// not pack-managed).
	Pack     string
	SyncedAt time.Time
}

// newLockFile returns an empty view.
func newLockFile() *LockFile {
	return &LockFile{Version: project.LockVersion, Projections: map[string]map[string]*Entry{}}
}

// entry returns the record for (skill, client), or nil.
func (lf *LockFile) entry(skill, client string) *Entry {
	return lf.Projections[skill][client]
}

// set records an entry for (skill, client).
func (lf *LockFile) set(skill, client string, e *Entry) {
	if lf.Projections[skill] == nil {
		lf.Projections[skill] = map[string]*Entry{}
	}
	lf.Projections[skill][client] = e
}

// remove deletes the record for (skill, client), dropping the skill key
// when its last client entry goes.
func (lf *LockFile) remove(skill, client string) {
	delete(lf.Projections[skill], client)
	if len(lf.Projections[skill]) == 0 {
		delete(lf.Projections, skill)
	}
}

// viewFromEntries builds the skill view from engine entries.
func viewFromEntries(entries []*project.Entry) *LockFile {
	lf := newLockFile()
	for _, e := range entries {
		if e.Kind != project.KindSkill {
			continue
		}
		lf.set(e.Source, e.Client, &Entry{
			Channel:          Channel(e.Channel),
			Target:           e.Path,
			CreatedByGridctl: e.CreatedByGridctl,
			TreeHash:         e.TreeHash,
			Pack:             e.Pack,
			SyncedAt:         e.SyncedAt,
		})
	}
	return lf
}

// viewFromLock projects the engine lock's skill entries into the
// legacy-shaped view the ops code works on.
func viewFromLock(pl *project.Lock) *LockFile {
	return viewFromEntries(pl.Entries(project.KindSkill))
}

// saveView flushes the view back into the engine lock and persists it.
// Projections dropped from the view are removed as explicit
// engine-driven deletes; entries of other kinds, unknown file-level
// fields, and unknown per-entry fields (carried forward by Lock.Set)
// ride along untouched.
func saveView(pl *project.Lock, lf *LockFile) error {
	var entries []*project.Entry
	for skill, clients := range lf.Projections {
		for client, e := range clients {
			entries = append(entries, &project.Entry{
				Kind:             project.KindSkill,
				Client:           client,
				Source:           skill,
				Path:             e.Target,
				Channel:          string(e.Channel),
				CreatedByGridctl: e.CreatedByGridctl,
				TreeHash:         e.TreeHash,
				Pack:             e.Pack,
				SyncedAt:         e.SyncedAt,
			})
		}
	}
	if err := pl.ReplaceKind(project.KindSkill, entries); err != nil {
		return err
	}
	return pl.Save()
}

// readLockFile loads the skill view straight from the unified lockfile
// at path. A missing file is the normal nothing-projected state.
// Production reads go through loadView, which also merges the legacy
// lockfiles before migration; this direct form backs tests that inspect
// the file a manager just wrote.
func readLockFile(path string) (*LockFile, error) {
	ul, err := project.ReadLockFile(path)
	if err != nil {
		return nil, err
	}
	return viewFromEntries(ul.Projections), nil
}

// loadView returns a read-only skill view of the projection state.
func (m *Manager) loadView(ctx context.Context) (*LockFile, error) {
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return viewFromLock(l), nil
}
