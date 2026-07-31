package project

import (
	"context"
	"fmt"
	"path/filepath"
)

const lockFileName = "project.lock.yaml"

// Store owns the unified projection lockfile under <home>/.gridctl and
// the cross-process lock guarding it. All paths resolve against home,
// so tests point it at a temp dir. Two Store instances over the same
// home (or two processes) serialize on the flock; in-process
// higher-level serialization stays with the kind managers' mutexes.
type Store struct {
	home string
}

// NewStore builds a Store rooted at an explicit home directory.
func NewStore(home string) *Store {
	return &Store{home: home}
}

// Path returns the unified lockfile path
// (<home>/.gridctl/project.lock.yaml). Its ".flock" sibling and the
// migration backups live next to it, outside the watched registry tree
// and outside every client-scanned directory.
func (s *Store) Path() string {
	return filepath.Join(s.home, ".gridctl", lockFileName)
}

// flockPath returns the sibling lock file taken by mutating operations.
func (s *Store) flockPath() string {
	return s.Path() + ".flock"
}

// Lock is an in-memory view of the lockfile, handed to Load and Mutate
// callers. Save is only valid inside Mutate.
type Lock struct {
	file     *LockFile
	store    *Store
	writable bool
	index    map[key]*Entry
}

// newLock builds the lookup index over a loaded file.
func newLock(lf *LockFile, store *Store, writable bool) *Lock {
	l := &Lock{file: lf, store: store, writable: writable, index: map[key]*Entry{}}
	for _, e := range lf.Projections {
		l.index[key{kind: e.Kind, client: e.Client, source: e.Source}] = e
	}
	return l
}

// Get returns the entry for (kind, client, source), or nil.
func (l *Lock) Get(kind Kind, client, source string) *Entry {
	return l.index[key{kind: kind, client: client, source: source}]
}

// Entries returns the entries of one kind in deterministic
// (source, client) order.
func (l *Lock) Entries(kind Kind) []*Entry {
	var out []*Entry
	for _, e := range l.file.Projections {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	sortEntries(out)
	return out
}

// Set records an entry, replacing any previous record for the same
// (kind, client, source) and enforcing the one-owner invariant: a path
// already owned by a different projection is refused, never stolen.
// When e carries no Extra, the previous record's unknown fields are
// carried forward, so re-recording an entry by an older binary never
// strips fields a newer revision wrote (Article XVII); pass an empty
// non-nil map to deliberately clear them.
func (l *Lock) Set(e *Entry) error {
	for _, other := range l.file.Projections {
		if other.Client == e.Client && other.Path == e.Path &&
			(other.Kind != e.Kind || other.Source != e.Source) {
			return fmt.Errorf("%w: %s is recorded for %s %q (client %s)",
				ErrPathConflict, e.Path, other.Kind, other.Source, other.Client)
		}
	}
	k := key{kind: e.Kind, client: e.Client, source: e.Source}
	if prev, ok := l.index[k]; ok {
		if e.Extra == nil {
			e.Extra = prev.Extra
		}
		for i, other := range l.file.Projections {
			if other == prev {
				l.file.Projections[i] = e
				break
			}
		}
	} else {
		l.file.Projections = append(l.file.Projections, e)
	}
	l.index[k] = e
	return nil
}

// ReplaceKind makes entries the complete recorded set for one kind:
// each entry is Set, and records of that kind absent from entries are
// removed as explicit engine-driven deletes. Entries of other kinds and
// unknown file-level fields ride along untouched. This is how a kind
// view flushes back into the lock.
func (l *Lock) ReplaceKind(kind Kind, entries []*Entry) error {
	seen := map[key]bool{}
	for _, e := range entries {
		if err := l.Set(e); err != nil {
			return err
		}
		seen[key{kind: e.Kind, client: e.Client, source: e.Source}] = true
	}
	for _, e := range l.Entries(kind) {
		if !seen[key{kind: e.Kind, client: e.Client, source: e.Source}] {
			l.Remove(e.Kind, e.Client, e.Source)
		}
	}
	return nil
}

// Remove deletes the record for (kind, client, source). Removal is an
// explicit engine-driven delete under the cross-process lock, never an
// inference from absence.
func (l *Lock) Remove(kind Kind, client, source string) {
	k := key{kind: kind, client: client, source: source}
	prev, ok := l.index[k]
	if !ok {
		return
	}
	delete(l.index, k)
	kept := l.file.Projections[:0]
	for _, e := range l.file.Projections {
		if e != prev {
			kept = append(kept, e)
		}
	}
	l.file.Projections = kept
}

// Save persists the lock atomically. Kind managers call it after every
// recorded mutation (skillsync's persistIfRecorded crash-safety
// property: a crash mid-pass must never leave artifacts on disk the
// lockfile does not own).
func (l *Lock) Save() error {
	if !l.writable {
		return fmt.Errorf("project lock is read-only outside Mutate")
	}
	return writeLockFile(l.store.Path(), l.file)
}

// Load returns a read-only view of the projection state without taking
// the cross-process lock (the lockfile is written atomically, so
// lock-free reads see a consistent file). Before migration, the two
// legacy lockfiles are merged in memory; nothing is written.
func (s *Store) Load(ctx context.Context) (*Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lf, err := s.loadFile()
	if err != nil {
		return nil, err
	}
	return newLock(lf, s, false), nil
}

// Mutate runs fn with a writable view while holding the cross-process
// lock. On the first mutating operation after an upgrade the legacy
// lockfiles migrate to the unified file (backups, then tombstones).
// Dry-run passes take no lock at all: they read the same merged view
// (the lockfile is written atomically, so lock-free reads are
// consistent), skip the on-disk migration, refuse Save, and therefore
// can neither write anything nor fail on lock contention.
func (s *Store) Mutate(ctx context.Context, dryRun bool, fn func(l *Lock) error) error {
	if dryRun {
		if err := ctx.Err(); err != nil {
			return err
		}
		lf, err := s.loadFile()
		if err != nil {
			return err
		}
		return fn(newLock(lf, s, false))
	}
	return s.withFlock(ctx, func() error {
		if err := s.migrateLocked(); err != nil {
			return err
		}
		lf, err := s.loadFile()
		if err != nil {
			return err
		}
		return fn(newLock(lf, s, true))
	})
}

// loadFile reads the unified lockfile, falling back to an in-memory
// merge of the legacy files when the unified file does not exist yet.
func (s *Store) loadFile() (*LockFile, error) {
	if fileExists(s.Path()) {
		return ReadLockFile(s.Path())
	}
	return s.mergeLegacy()
}
