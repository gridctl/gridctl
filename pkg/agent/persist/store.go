package persist

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// runFilePerm is the mode JSONL run files are created with. 0600
// matches the vault/state convention — operator data is private to
// the user, not world-readable.
const runFilePerm = 0o600

// runDirPerm matches runFilePerm at the directory level; new
// directories on the path inherit the same restrictive permissions.
const runDirPerm = 0o700

// fileExt is the on-disk extension for run ledgers.
const fileExt = ".jsonl"

// runIDRandomBytes is the entropy length for the random suffix on a
// run ID. 8 bytes (16 hex chars) gives ~10^19 possibilities — large
// enough that two runs starting in the same wall-clock millisecond
// won't collide under realistic operator workloads.
const runIDRandomBytes = 8

// Store is the file-backed run-state ledger. One Store instance
// serves the whole process; concurrent OpenWriter calls for the same
// run ID return the same underlying writer (single-writer per run).
type Store struct {
	dir string

	mu      sync.Mutex
	writers map[string]*Recorder
}

// NewStore constructs a Store rooted at the given directory. The
// directory is created on first write; passing an empty string falls
// back to the default ~/.gridctl/runs/ location via DefaultRunsDir.
func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultRunsDir()
	}
	return &Store{
		dir:     dir,
		writers: make(map[string]*Recorder),
	}
}

// Dir returns the configured runs directory.
func (s *Store) Dir() string {
	return s.dir
}

// PathFor returns the on-disk path for a given run ID.
func (s *Store) PathFor(runID string) string {
	return filepath.Join(s.dir, runID+fileExt)
}

// OpenWriter returns a Recorder for the given run ID, creating the
// file (and directory) on first call. Subsequent calls for the same
// run ID return the cached Recorder so a run is guaranteed to have
// exactly one writer; callers must Close when done with the run.
func (s *Store) OpenWriter(runID string) (*Recorder, error) {
	if runID == "" {
		return nil, errors.New("persist: run id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.writers[runID]; ok {
		return rec, nil
	}
	if err := os.MkdirAll(s.dir, runDirPerm); err != nil {
		return nil, fmt.Errorf("persist: creating runs dir: %w", err)
	}
	path := s.PathFor(runID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, runFilePerm)
	if err != nil {
		return nil, fmt.Errorf("persist: opening run file: %w", err)
	}
	rec := &Recorder{
		runID: runID,
		path:  path,
		file:  f,
		store: s,
	}
	// Re-establish the sequence high-water mark on reopen so a
	// resumed run continues numbering its events without gaps.
	if seq, err := lastSequence(path); err == nil {
		rec.seq.Store(seq)
	}
	s.writers[runID] = rec
	return rec, nil
}

// Read returns all events recorded for a run, in file order.
// Trailing partial lines (e.g. from a crash mid-write) are silently
// skipped — the on-disk shape is append-only, so a partial line can
// only ever be the last record.
func (s *Store) Read(runID string) ([]Event, error) {
	path := s.PathFor(runID)
	f, err := os.Open(path) // #nosec G304 -- path is the store's own derivation
	if err != nil {
		return nil, fmt.Errorf("persist: opening run file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var events []Event
	for scanner.Scan() {
		line := bytes(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Trailing partial lines manifest here; we ignore
			// them deliberately. A non-trailing parse failure is
			// recoverable in the sense that the rest of the
			// ledger is still readable.
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("persist: scanning run file: %w", err)
	}
	return events, nil
}

// Stream invokes fn for each event in the run, in file order. Stream
// stops early if fn returns an error or if ctx is cancelled. Useful
// for SSE event streaming and for resume's replay loop, both of
// which want to avoid materialising the full ledger.
func (s *Store) Stream(ctx context.Context, runID string, fn func(Event) error) error {
	path := s.PathFor(runID)
	f, err := os.Open(path) // #nosec G304 -- path is the store's own derivation
	if err != nil {
		return fmt.Errorf("persist: opening run file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// List enumerates every run on disk, newest first by mtime. Each
// summary is derived from the head of the file (RunStarted) and the
// tail (last RunCompleted, if any) — neither is required to be
// present on a partially-written ledger.
func (s *Store) List(limit int) ([]RunSummary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("persist: reading runs dir: %w", err)
	}
	type tagged struct {
		name    string
		modTime time.Time
	}
	var files []tagged
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, fileExt) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, tagged{name: name, modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	summaries := make([]RunSummary, 0, len(files))
	for _, t := range files {
		runID := strings.TrimSuffix(t.name, fileExt)
		summary, err := s.Summary(runID)
		if err != nil {
			continue
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// Summary derives a run header from its ledger. Returns os.ErrNotExist
// when the run file is absent.
func (s *Store) Summary(runID string) (RunSummary, error) {
	path := s.PathFor(runID)
	info, err := os.Stat(path)
	if err != nil {
		return RunSummary{}, err
	}
	events, err := s.Read(runID)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{
		RunID:     runID,
		Path:      path,
		EventCount: len(events),
		ModTime:    info.ModTime(),
		Status:     "running",
	}
	for _, ev := range events {
		switch ev.Type {
		case EventRunStarted:
			var p RunStartedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				summary.Skill = p.Skill
				summary.Flavor = p.Flavor
				summary.ParentRunID = p.ParentRunID
				summary.TraceID = p.TraceID
				summary.StartedAt = ev.Time
			}
		case EventRunCompleted:
			var p RunCompletedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				summary.Status = p.Status
				summary.Error = p.Error
				summary.CompletedAt = ev.Time
			}
		case EventApprovalRequest:
			var p ApprovalRequestPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				summary.PendingApproval = p.ApprovalID
				summary.Status = "awaiting_approval"
			}
		case EventApprovalResponse:
			summary.PendingApproval = ""
			if summary.Status == "awaiting_approval" {
				summary.Status = "running"
			}
		}
	}
	return summary, nil
}

// CloseAll flushes and closes every active recorder. Returns the
// first close error, if any; the rest are skipped.
func (s *Store) CloseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for id, rec := range s.writers {
		if err := rec.closeLocked(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.writers, id)
	}
	return firstErr
}

// Recorder is an append-only writer for a single run's JSONL ledger.
// All write paths funnel through Record, which serialises encoding
// and write so two goroutines emitting events on the same run don't
// interleave bytes mid-line.
type Recorder struct {
	runID string
	path  string

	mu     sync.Mutex
	file   *os.File
	store  *Store
	closed bool

	seq atomic.Uint64
}

// RunID returns the run identifier this Recorder writes to.
func (r *Recorder) RunID() string { return r.runID }

// Path returns the on-disk path the Recorder writes to.
func (r *Recorder) Path() string { return r.path }

// NextSeq advances and returns the next sequence number. Exposed so
// callers that compose their own Event values keep the sequence
// monotonic; Record handles this automatically when the caller passes
// a payload through MarshalEvent.
func (r *Recorder) NextSeq() uint64 {
	return r.seq.Add(1)
}

// Record appends a typed payload as an Event. The sequence number is
// allocated from the recorder's monotonic counter; Time is set to
// time.Now().UTC().
func (r *Recorder) Record(eventType EventType, payload any) (Event, error) {
	seq := r.seq.Add(1)
	ev, err := MarshalEvent(r.runID, seq, eventType, payload)
	if err != nil {
		return Event{}, err
	}
	if err := r.write(ev); err != nil {
		return Event{}, err
	}
	return ev, nil
}

// write serialises an Event onto the underlying file. JSON encoding
// is done into a local buffer first so a partial encode failure can
// abort cleanly without polluting the file.
func (r *Recorder) write(ev Event) error {
	encoded, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("persist: encoding event: %w", err)
	}
	encoded = append(encoded, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("persist: recorder is closed")
	}
	if _, err := r.file.Write(encoded); err != nil {
		return fmt.Errorf("persist: writing event: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file. Calling Close more
// than once is safe; subsequent calls are no-ops.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		r.store.mu.Lock()
		delete(r.store.writers, r.runID)
		r.store.mu.Unlock()
	}
	return r.closeLocked()
}

func (r *Recorder) closeLocked() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

// RunSummary is the header view of a run, computed from the JSONL
// ledger by Store.List and Store.Summary. The shape is intentionally
// flat for `gridctl runs list --format json` consumers.
type RunSummary struct {
	RunID           string    `json:"run_id"`
	Path            string    `json:"path,omitempty"`
	Skill           string    `json:"skill,omitempty"`
	Flavor          string    `json:"flavor,omitempty"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	ModTime         time.Time `json:"mtime,omitempty"`
	EventCount      int       `json:"event_count"`
	ParentRunID     string    `json:"parent_run_id,omitempty"`
	TraceID         string    `json:"trace_id,omitempty"`
	PendingApproval string    `json:"pending_approval,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// NewRunID produces a fresh run identifier in the shape
// "run_<rfc3339-compact>_<8-byte-hex>". The compact RFC 3339 prefix
// keeps `runs list` lexicographically sortable by start time without
// reading the file body.
func NewRunID() string {
	now := time.Now().UTC().Format("20060102T150405.000")
	now = strings.ReplaceAll(now, ".", "")
	buf := make([]byte, runIDRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand should not fail in practice; fall back to a
		// fixed suffix so callers still get a usable string instead
		// of an empty one.
		return fmt.Sprintf("run_%s_%016x", now, time.Now().UnixNano())
	}
	return fmt.Sprintf("run_%s_%s", now, hex.EncodeToString(buf))
}

// DefaultRunsDir resolves the default runs directory under the user's
// home. Mirrors pkg/state.BaseDir(); duplicated here to keep
// pkg/agent/persist independent of pkg/state (the agent runtime must
// remain importable from packages that pkg/state itself depends on).
func DefaultRunsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gridctl", "runs")
}

// lastSequence reads the last well-formed line of a run file and
// returns its Seq. Used on Recorder open to re-establish the
// monotonic counter after a crash or reopen.
func lastSequence(path string) (uint64, error) {
	f, err := os.Open(path) // #nosec G304 -- caller-derived path
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // read-only

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var last uint64
	for scanner.Scan() {
		line := bytes(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		last = ev.Seq
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	return last, nil
}

// bytes copies a scanner-owned slice so callers can hold onto it
// past the next Scan call without the slice being reused.
func bytes(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
