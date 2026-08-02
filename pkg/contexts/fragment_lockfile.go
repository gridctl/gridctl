package contexts

import (
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// FragmentLockFile is the context-fragment view over the unified project
// lockfile: one record per (client, fragment) projected file. It is a
// separate kind from the per-client context entries on purpose — contexts
// flushes those with ReplaceKind, and an older gridctl that cannot
// represent fragments must never drop or clobber their records.
type FragmentLockFile struct {
	// Projections maps fragment name -> client slug -> record.
	Projections map[string]map[string]*FragmentEntry
}

// FragmentEntry is one projected fragment file's ownership record.
type FragmentEntry struct {
	// Target is the absolute path gridctl wrote.
	Target string
	// InstalledHash is the hash of the bytes exactly as written (drift).
	InstalledHash string
	// CanonicalHash is the source fragment's hash at sync time (staleness).
	CanonicalHash string
	// Pack tags the projection with the pack that applied it.
	Pack     string
	SyncedAt time.Time
}

// fragmentViewFromLock projects the engine lock's context-fragment entries
// into the nested view the fragment ops work on.
func fragmentViewFromLock(l *project.Lock) *FragmentLockFile {
	lf := &FragmentLockFile{Projections: map[string]map[string]*FragmentEntry{}}
	for _, e := range l.Entries(project.KindContextFragment) {
		byClient := lf.Projections[e.Source]
		if byClient == nil {
			byClient = map[string]*FragmentEntry{}
			lf.Projections[e.Source] = byClient
		}
		byClient[e.Client] = &FragmentEntry{
			Target:        e.Path,
			InstalledHash: e.InstalledHash,
			CanonicalHash: e.CanonicalHash,
			Pack:          e.Pack,
			SyncedAt:      e.SyncedAt,
		}
	}
	return lf
}

// entry returns one (fragment, client) record, or nil.
func (lf *FragmentLockFile) entry(fragment, client string) *FragmentEntry {
	return lf.Projections[fragment][client]
}

// set records one (fragment, client) projection.
func (lf *FragmentLockFile) set(fragment, client string, e *FragmentEntry) {
	byClient := lf.Projections[fragment]
	if byClient == nil {
		byClient = map[string]*FragmentEntry{}
		lf.Projections[fragment] = byClient
	}
	byClient[client] = e
}

// remove drops one (fragment, client) projection.
func (lf *FragmentLockFile) remove(fragment, client string) {
	byClient := lf.Projections[fragment]
	if byClient == nil {
		return
	}
	delete(byClient, client)
	if len(byClient) == 0 {
		delete(lf.Projections, fragment)
	}
}

// applyFragmentView replaces KindContextFragment entries without saving.
func applyFragmentView(l *project.Lock, lf *FragmentLockFile) error {
	var entries []*project.Entry
	for fragment, byClient := range lf.Projections {
		for slug, e := range byClient {
			entries = append(entries, &project.Entry{
				Kind:             project.KindContextFragment,
				Client:           slug,
				Source:           fragment,
				Path:             e.Target,
				InstalledHash:    e.InstalledHash,
				CanonicalHash:    e.CanonicalHash,
				CreatedByGridctl: true,
				Pack:             e.Pack,
				SyncedAt:         e.SyncedAt,
			})
		}
	}
	return l.ReplaceKind(project.KindContextFragment, entries)
}

// saveFragmentView flushes the fragment view back into the engine lock and
// persists it. Only context-fragment entries are replaced; every other
// kind (including the per-client context entries) rides along untouched.
func saveFragmentView(l *project.Lock, lf *FragmentLockFile) error {
	if err := applyFragmentView(l, lf); err != nil {
		return err
	}
	return l.Save()
}
