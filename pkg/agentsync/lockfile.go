package agentsync

import (
	"context"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// LockFile is the agent-kind view over the unified project lockfile,
// keyed agent name → client slug. The engine owns the on-disk schema,
// versioning, migration, and locking; this view exists so the ops code
// keeps the same shape as the other kinds.
type LockFile struct {
	// Projections maps agent name → client slug → entry.
	Projections map[string]map[string]*Entry
}

// Entry is one (agent, client) projection record. Drift is judged
// against InstalledHash; staleness against CanonicalHash. The channel is
// always copy.
type Entry struct {
	// Target is the absolute file path gridctl wrote.
	Target string
	// InstalledHash is the projected file's hash exactly as written.
	InstalledHash string
	// CanonicalHash is the canonical AGENT.md hash at sync time. The
	// render is identity, so the two coincide at write time; they diverge
	// when either side changes.
	CanonicalHash string
	// CreatedByGridctl marks the path as gridctl-owned. Always true for
	// recorded entries.
	CreatedByGridctl bool
	// ModelValue is the model preference a policy rewrite wrote into the
	// installed bytes; non-empty marks the projection as rewritten (the
	// preserve rule and adopt key on it), and the value lets adopt tell
	// the policy's write apart from a deliberate user edit. Empty for
	// pass-through projections.
	ModelValue string
	// Pack tags the projection with the pack that applied it (empty =
	// not pack-managed).
	Pack     string
	SyncedAt time.Time
}

// newLockFile returns an empty view.
func newLockFile() *LockFile {
	return &LockFile{Projections: map[string]map[string]*Entry{}}
}

// entry returns the record for (agent, client), or nil.
func (lf *LockFile) entry(agent, client string) *Entry {
	return lf.Projections[agent][client]
}

// set records an entry for (agent, client).
func (lf *LockFile) set(agent, client string, e *Entry) {
	if lf.Projections[agent] == nil {
		lf.Projections[agent] = map[string]*Entry{}
	}
	lf.Projections[agent][client] = e
}

// remove deletes the record for (agent, client), dropping the agent key
// when its last client entry goes.
func (lf *LockFile) remove(agent, client string) {
	delete(lf.Projections[agent], client)
	if len(lf.Projections[agent]) == 0 {
		delete(lf.Projections, agent)
	}
}

// viewFromLock projects the engine lock's agent entries into the view
// the ops code works on.
func viewFromLock(pl *project.Lock) *LockFile {
	lf := newLockFile()
	for _, e := range pl.Entries(project.KindAgent) {
		lf.set(e.Source, e.Client, &Entry{
			Target:           e.Path,
			InstalledHash:    e.InstalledHash,
			CanonicalHash:    e.CanonicalHash,
			CreatedByGridctl: e.CreatedByGridctl,
			ModelValue:       e.ModelValue,
			Pack:             e.Pack,
			SyncedAt:         e.SyncedAt,
		})
	}
	return lf
}

// saveView flushes the view back into the engine lock and persists it.
// Projections dropped from the view are removed as explicit
// engine-driven deletes; entries of other kinds, unknown file-level
// fields, and unknown per-entry fields (carried forward by Lock.Set)
// ride along untouched.
func saveView(pl *project.Lock, lf *LockFile) error {
	var entries []*project.Entry
	for agent, clients := range lf.Projections {
		for client, e := range clients {
			entries = append(entries, &project.Entry{
				Kind:             project.KindAgent,
				Client:           client,
				Source:           agent,
				Path:             e.Target,
				Channel:          ChannelCopy,
				CreatedByGridctl: e.CreatedByGridctl,
				ModelValue:       e.ModelValue,
				InstalledHash:    e.InstalledHash,
				CanonicalHash:    e.CanonicalHash,
				Pack:             e.Pack,
				SyncedAt:         e.SyncedAt,
			})
		}
	}
	if err := pl.ReplaceKind(project.KindAgent, entries); err != nil {
		return err
	}
	return pl.Save()
}

// loadView returns a read-only agent view of the projection state.
func (m *Manager) loadView(ctx context.Context) (*LockFile, error) {
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return viewFromLock(l), nil
}
