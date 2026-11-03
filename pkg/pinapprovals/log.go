package pinapprovals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Sentinel errors reported by Load. A corrupt file resets the in-memory log
// to empty and returns ErrCorrupt; a caller that prefers availability may
// continue. ErrNewerVersion must not be papered over: appending would
// overwrite a log this binary does not understand.
var (
	ErrCorrupt      = errors.New("corrupt approvals log")
	ErrNewerVersion = errors.New("approvals log written by a newer gridctl")
)

// logFile is the on-disk document. Entries are newest first.
type logFile struct {
	Version  string             `json:"version"`
	Stack    string             `json:"stack"`
	Kind     string             `json:"kind"`
	Subjects map[string][]Entry `json:"subjects"`
}

// Log is the approvals history for one stack and kind. Open does not touch
// the disk. Load and Append honor context cancellation before I/O.
//
// Append does not take a file lock. The owning pin store calls it inside
// its existing state.WithLock critical section so an append and a re-pin
// cannot interleave across processes. Two Append calls without that lock
// can overwrite each other.
type Log struct {
	path     string
	stack    string
	kind     string
	mu       sync.Mutex
	ready    bool
	subjects map[string][]Entry
}

// Open returns a log bound to path. It does not create or read the file.
func Open(path, stack, kind string) *Log {
	return &Log{
		path:     path,
		stack:    stack,
		kind:     kind,
		subjects: map[string][]Entry{},
	}
}

// Load reads the log. A missing file is an empty log. A corrupt file resets
// to empty and returns an error wrapping ErrCorrupt. A version other than
// "" or "1" resets to empty and returns an error wrapping ErrNewerVersion;
// the message names the path and the version. A newer-version log is not
// writable until a later Load succeeds.
func (l *Log) Load(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			l.mu.Lock()
			l.subjects = map[string][]Entry{}
			l.ready = true
			l.mu.Unlock()
			return nil
		}
		return fmt.Errorf("pinapprovals: reading %s: %w", l.path, err)
	}
	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		l.resetLocked(true)
		return fmt.Errorf("pinapprovals: %w at %s: %v", ErrCorrupt, l.path, err)
	}
	switch doc.Version {
	case "", FileVersion:
	default:
		l.resetLocked(false)
		return fmt.Errorf("pinapprovals: %w: %s has version %q; upgrade gridctl to use it", ErrNewerVersion, l.path, doc.Version)
	}
	if doc.Subjects == nil {
		doc.Subjects = map[string][]Entry{}
	}
	l.mu.Lock()
	l.subjects = doc.Subjects
	l.ready = true
	l.mu.Unlock()
	return nil
}

func (l *Log) resetLocked(ready bool) {
	l.mu.Lock()
	l.subjects = map[string][]Entry{}
	l.ready = ready
	l.mu.Unlock()
}

// Append prepends entry for subject, keeps at most MaxHistory entries, and
// writes the file atomically with mode 0600. The in-memory log changes only
// after the write succeeds. A log that has not been loaded, or that Load
// rejected as a newer version, is not written.
//
// Append does not take a file lock. Call it inside the owning pin store's
// state.WithLock critical section.
func (l *Log) Append(ctx context.Context, subject string, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	ready := l.ready
	l.mu.Unlock()
	if !ready {
		return fmt.Errorf("pinapprovals: approvals log %s is not loaded", l.path)
	}
	entry = cloneEntry(entry)
	if !entry.ApprovedAt.IsZero() {
		entry.ApprovedAt = entry.ApprovedAt.UTC()
	}
	if entry.ApprovedBy == "" {
		entry.ApprovedBySource = ""
	}

	l.mu.Lock()
	prev := l.subjects[subject]
	next := make([]Entry, 0, min(len(prev)+1, MaxHistory))
	next = append(next, entry)
	for _, existing := range prev {
		if len(next) == MaxHistory {
			break
		}
		next = append(next, cloneEntry(existing))
	}
	subjects := copySubjects(l.subjects)
	subjects[subject] = next
	l.mu.Unlock()

	if err := l.write(ctx, subjects); err != nil {
		return err
	}
	l.mu.Lock()
	l.subjects = subjects
	l.mu.Unlock()
	return nil
}

// History returns a copy of subject's entries, newest first. An unknown
// subject returns nil.
func (l *Log) History(subject string) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return CloneEntries(l.subjects[subject])
}

// Latest returns a copy of the newest entry. The bool is false when the
// subject has no entries.
func (l *Log) Latest(subject string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.subjects[subject]
	if len(entries) == 0 {
		return Entry{}, false
	}
	return cloneEntry(entries[0]), true
}

func copySubjects(in map[string][]Entry) map[string][]Entry {
	out := make(map[string][]Entry, len(in)+1)
	for name, entries := range in {
		out[name] = CloneEntries(entries)
	}
	return out
}

func (l *Log) write(ctx context.Context, subjects map[string][]Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if subjects == nil {
		subjects = map[string][]Entry{}
	}
	data, err := json.MarshalIndent(logFile{
		Version:  FileVersion,
		Stack:    l.stack,
		Kind:     l.kind,
		Subjects: subjects,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("pinapprovals: marshaling approvals log: %w", err)
	}
	data = append(data, '\n')
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("pinapprovals: creating approvals directory: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("pinapprovals: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			slog.Warn("pinapprovals: failed to remove temp file after rename error", "path", tmp, "error", removeErr)
		}
		return fmt.Errorf("pinapprovals: renaming temp file: %w", err)
	}
	return nil
}
