package ctxpins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	lockName    = "ctxpins"
	lockTimeout = 5 * time.Second
	filePerm    = os.FileMode(0o600)
	dirPerm     = os.FileMode(0o755)
)

// Sentinel errors. Load's ErrCorrupt and ErrNewerVersion follow the
// pkg/skillpins contract: a corrupt file is reported and not rewritten;
// ErrNewerVersion must never be papered over. The rest are decision errors
// so the CLI and API can map them without string matching.
var (
	ErrCorrupt           = errors.New("corrupt context pin file")
	ErrNewerVersion      = errors.New("context pin file written by a newer gridctl")
	ErrNotPinned         = errors.New("fragment is not pinned")
	ErrNoDocument        = errors.New("fragment does not exist")
	ErrHashMismatch      = errors.New("fragment content changed since the reviewed diff")
	ErrReasonRequired    = errors.New("approving a fragment with unresolved findings requires a reason")
	ErrDigestUnavailable = errors.New("fragment content could not be hashed")
)

// Store manages TOFU content pins for the global context. It holds only
// its path and scan config. Every public method reloads the pin file under
// state.WithLock, so two Store values on the same path never disagree.
type Store struct {
	path    string
	logPath string

	mu          sync.Mutex
	scanEnabled bool
	scanIgnore  []string
}

// New creates a Store for ~/.gridctl/pins/context/global.json. The path is
// resolved on first use so construction stays error-free; resolution fails
// only when no home directory is available.
func New() *Store {
	return &Store{scanEnabled: true}
}

// NewWithPath creates a Store that keeps pins in dir/global.json.
// Intended for tests; it never resolves the real home.
func NewWithPath(dir string) *Store {
	return &Store{
		path:        filepath.Join(dir, "global.json"),
		logPath:     filepath.Join(dir, "approvals", "context", "global.json"),
		scanEnabled: true,
	}
}

// SetScanConfig configures the advisory poisoning scanner. enabled toggles
// it; ignore suppresses findings by code. Scanning never affects the hash
// or status. Nil Scan on a deployed stack means on; callers pass that
// resolved bool.
func (s *Store) SetScanConfig(enabled bool, ignore []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanEnabled = enabled
	s.scanIgnore = append([]string(nil), ignore...)
}

func (s *Store) scanSettings() (bool, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanEnabled, append([]string(nil), s.scanIgnore...)
}

func (s *Store) ensurePath() error {
	// The file lock does not order these fields for the race detector or
	// the Go memory model. Two first calls on a New() store would otherwise
	// race on path and logPath.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if s.logPath == "" {
			s.logPath = filepath.Join(filepath.Dir(s.path), "approvals", "context", "global.json")
		}
		return nil
	}
	p, err := state.ContextPinsPath()
	if err != nil {
		return fmt.Errorf("ctxpins: resolving pin file path: %w", err)
	}
	lp, err := state.ContextPinApprovalsPath()
	if err != nil {
		return fmt.Errorf("ctxpins: resolving approvals log path: %w", err)
	}
	s.path = p
	s.logPath = lp
	return nil
}

// Get returns a deep copy of one pin. The bool is false when the name is
// not pinned. A corrupt or newer pin file is returned as an error and is
// not rewritten.
func (s *Store) Get(ctx context.Context, name string) (*FragmentPin, bool, error) {
	var pin *FragmentPin
	var ok bool
	err := s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		projectApprovals(pf, log)
		got, found := pf.Fragments[name]
		if !found || got == nil {
			return false, nil
		}
		pin = copyFragmentPin(got)
		ok = true
		return false, nil
	})
	if err != nil {
		return nil, false, err
	}
	return pin, ok, nil
}

// GetAll returns a deep copy of every pin, keyed by fragment name. The map
// is empty when the file is absent. A corrupt or newer file is an error.
func (s *Store) GetAll(ctx context.Context) (map[string]*FragmentPin, error) {
	out := map[string]*FragmentPin{}
	err := s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		projectApprovals(pf, log)
		for name, pin := range pf.Fragments {
			if pin == nil {
				continue
			}
			out[name] = copyFragmentPin(pin)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Sync pins unseen documents, verifies the rest, flips status on change,
// and lists missing pinned names without pruning. It saves once, and only
// when something changed. A document with ReadErr set is drift when pinned
// and skipped when unpinned.
func (s *Store) Sync(ctx context.Context, src Source) (*SyncResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("ctxpins: nil document source")
	}
	docs, err := src.ListDocuments(ctx)
	if err != nil {
		return nil, err
	}
	enabled, ignore := s.scanSettings()
	result := &SyncResult{}
	err = s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		projectApprovals(pf, log)
		now := time.Now().UTC()
		dirty := false
		seen := make(map[string]bool, len(docs))
		for _, d := range docs {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if d.Name == "" {
				continue
			}
			seen[d.Name] = true
			pin := pf.Fragments[d.Name]
			if d.ReadErr != nil {
				if pin == nil {
					slog.Warn("ctxpins: reading unpinned fragment failed, not pinning", "fragment", d.Name, "error", d.ReadErr)
					continue
				}
				result.Drifted = append(result.Drifted, d.Name)
				pin.LastVerifiedAt = now
				if pin.Status != StatusDrift {
					slog.Warn("ctxpins: reading pinned fragment failed; marking pin drift",
						"fragment", d.Name, "error", d.ReadErr,
						"hint", "review with 'gridctl ctx pins diff' or the Pins workspace")
					pin.Status = StatusDrift
					dirty = true
				}
				continue
			}
			hash := Hash(d.Raw)
			if pin == nil {
				pf.Fragments[d.Name] = newPin(d, hash, now, "", enabled, ignore, log)
				result.Pinned = append(result.Pinned, d.Name)
				dirty = true
				continue
			}
			status := StatusPinned
			if hash != pin.Hash {
				status = StatusDrift
				result.Drifted = append(result.Drifted, d.Name)
			}
			pin.LastVerifiedAt = now
			if pin.Status != status {
				if status == StatusDrift {
					slog.Warn("ctxpins: pin drift detected", "fragment", d.Name,
						"hint", "review with 'gridctl ctx pins diff' or the Pins workspace")
				}
				pin.Status = status
				dirty = true
			}
		}
		for name := range pf.Fragments {
			if !seen[name] {
				result.Missing = append(result.Missing, name)
			}
		}
		return dirty, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(result.Pinned)
	sort.Strings(result.Drifted)
	sort.Strings(result.Missing)
	return result, nil
}

// Verify compares one fragment with its pin and does not write.
// ErrDigestUnavailable is returned when the document cannot be read, before
// any not-found result. ErrNoDocument means the name does not exist.
// ErrNotPinned means the name has no record.
func (s *Store) Verify(ctx context.Context, name string, src Source) (*VerifyResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := documentOrError(ctx, src, name)
	if err != nil {
		return nil, err
	}
	enabled, ignore := s.scanSettings()
	var result *VerifyResult
	err = s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		projectApprovals(pf, log)
		pin := pf.Fragments[name]
		if pin == nil {
			return false, fmt.Errorf("ctxpins: fragment %q: %w", name, ErrNotPinned)
		}
		hash := Hash(doc.Raw)
		result = &VerifyResult{
			Name:          name,
			Status:        StatusPinned,
			CompositeHash: CompositeHash(hash),
		}
		if hash != pin.Hash {
			findings := scanFindings(doc, enabled, ignore)
			if findings == nil {
				findings = []pins.Finding{}
			}
			result.Status = StatusDrift
			result.Diff = &Diff{
				Name:        name,
				OldHash:     pin.Hash,
				NewHash:     hash,
				OldDocument: pin.Document,
				NewDocument: string(doc.Raw),
				Findings:    findings,
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Approve re-pins the current content and appends an approval entry. A
// present fragment with no record is pinned by this approval, matching
// skill pins: the call is a review of current content, not a check that a
// record already exists. Absent fragments return ErrNoDocument from the
// source. The log is written inside the file lock before the pin file. A
// log error leaves the pin file unchanged. Reset does not touch the log.
func (s *Store) Approve(ctx context.Context, name string, src Source, req ApproveRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := pinapprovals.ValidateReason(req.Reason); err != nil {
		return err
	}
	evidence, err := pinapprovals.ValidateReferences(req.Evidence)
	if err != nil {
		return err
	}
	approvedBy := strings.TrimSpace(req.ApprovedBy)
	if err := pinapprovals.ValidateApprover(approvedBy); err != nil {
		return err
	}
	doc, err := documentOrError(ctx, src, name)
	if err != nil {
		return err
	}
	enabled, ignore := s.scanSettings()
	hash := Hash(doc.Raw)
	composite := CompositeHash(hash)
	if req.ExpectedHash != "" && req.ExpectedHash != composite {
		return fmt.Errorf("ctxpins: fragment %q: %w", name, ErrHashMismatch)
	}
	findings := scanFindings(doc, enabled, ignore)
	if len(findings) > 0 && req.Reason == "" {
		return fmt.Errorf("ctxpins: fragment %q has %d advisory finding(s): %w", name, len(findings), ErrReasonRequired)
	}
	err = s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		if log == nil {
			return false, errors.New("ctxpins: approvals log is not loaded")
		}
		by, source := approvedBy, req.ApprovedBySource
		if by == "" {
			source = ""
		}
		entry := pinapprovals.Entry{
			ApprovedAt:       time.Now().UTC(),
			ApprovedBy:       by,
			ApprovedBySource: source,
			ApprovedHash:     composite,
			Bound:            req.ExpectedHash != "",
			Reason:           req.Reason,
			Evidence:         evidence,
		}
		if err := log.Append(ctx, name, entry); err != nil {
			return false, err
		}
		now := time.Now().UTC()
		pin := newPin(doc, hash, now, req.Reason, enabled, ignore, log)
		if existing := pf.Fragments[name]; existing != nil && !existing.PinnedAt.IsZero() {
			pin.PinnedAt = existing.PinnedAt
		}
		pf.Fragments[name] = pin
		return true, nil
	})
	if err != nil {
		return err
	}
	source := req.ApprovedBySource
	if approvedBy == "" {
		source = ""
	}
	slog.Info("ctxpins: recorded approval",
		"fragment", name,
		"bound", req.ExpectedHash != "",
		"approver_source", source,
		"evidence_count", len(evidence),
		"has_reason", req.Reason != "",
	)
	return nil
}

// Reset deletes the pin record. The approvals log is left untouched, so a
// later re-pin still shows the prior entries.
func (s *Store) Reset(ctx context.Context, name string) error {
	return s.withLock(ctx, func(pf *PinFile, _ *pinapprovals.Log) (bool, error) {
		if _, ok := pf.Fragments[name]; !ok {
			return false, fmt.Errorf("ctxpins: fragment %q: %w", name, ErrNotPinned)
		}
		delete(pf.Fragments, name)
		return true, nil
	})
}

// Rename moves a record from one fragment name to another when from exists
// and to does not. Any other condition is a no-op that returns nil. Used by
// the fragments-mode migration, which preserves the digest because the
// written bytes normalize to the same hash.
func (s *Store) Rename(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	return s.withLock(ctx, func(pf *PinFile, log *pinapprovals.Log) (bool, error) {
		pin := pf.Fragments[from]
		if pin == nil {
			return false, nil
		}
		if _, exists := pf.Fragments[to]; exists {
			return false, nil
		}
		if err := moveApprovalHistory(ctx, log, from, to); err != nil {
			return false, err
		}
		moved := copyFragmentPin(pin)
		if log != nil {
			moved.Approvals = log.History(to)
		}
		pf.Fragments[to] = moved
		delete(pf.Fragments, from)
		return true, nil
	})
}

// moveApprovalHistory copies the from-subject log onto to, oldest first so
// the newest entry stays first. The old subject is left in place: Reset is
// the only operation that is specified to leave the log untouched, and
// there is no delete API. A later load rehydrates the new name from the log.
func moveApprovalHistory(ctx context.Context, log *pinapprovals.Log, from, to string) error {
	if log == nil {
		return nil
	}
	hist := log.History(from)
	for i := len(hist) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := log.Append(ctx, to, hist[i]); err != nil {
			return err
		}
	}
	return nil
}

func documentOrError(ctx context.Context, src Source, name string) (Document, error) {
	if src == nil {
		return Document{}, errors.New("ctxpins: nil document source")
	}
	doc, err := src.GetDocument(ctx, name)
	if err != nil {
		return Document{}, err
	}
	if doc.ReadErr != nil {
		return Document{}, fmt.Errorf("ctxpins: fragment %q: %w: %v", name, ErrDigestUnavailable, doc.ReadErr)
	}
	return doc, nil
}

func newPin(d Document, hash string, now time.Time, reason string, scanEnabled bool, ignore []string, log *pinapprovals.Log) *FragmentPin {
	source := SourceLocal
	if d.Pack != "" {
		source = SourcePack
	}
	pin := &FragmentPin{
		Hash:           hash,
		Document:       string(d.Raw),
		Source:         source,
		Pack:           d.Pack,
		ApprovedReason: reason,
		PinnedAt:       now,
		LastVerifiedAt: now,
		Status:         StatusPinned,
		Findings:       scanFindings(d, scanEnabled, ignore),
	}
	if log != nil {
		pin.Approvals = log.History(d.Name)
	}
	return pin
}

func scanFindings(d Document, enabled bool, ignore []string) []pins.Finding {
	if !enabled {
		return nil
	}
	return pins.FilterFindings(pins.ScanSkill(d.Name, d.Description, d.Body), ignore)
}

func projectApprovals(pf *PinFile, log *pinapprovals.Log) {
	if pf == nil || log == nil {
		return
	}
	for name, pin := range pf.Fragments {
		if pin == nil {
			continue
		}
		pin.Approvals = log.History(name)
	}
}

func copyFragmentPin(pin *FragmentPin) *FragmentPin {
	if pin == nil {
		return nil
	}
	out := *pin
	out.Findings = append([]pins.Finding(nil), pin.Findings...)
	out.Approvals = pinapprovals.CloneEntries(pin.Approvals)
	return &out
}

func (s *Store) withLock(ctx context.Context, fn func(*PinFile, *pinapprovals.Log) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return state.WithLock(lockName, lockTimeout, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.ensurePath(); err != nil {
			return err
		}
		pf, err := s.load()
		if err != nil {
			return err
		}
		log, err := s.loadLog(ctx)
		if err != nil {
			return err
		}
		dirty, err := fn(pf, log)
		if err != nil || !dirty {
			return err
		}
		return s.save(pf)
	})
}

func (s *Store) load() (*PinFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyPinFile(), nil
		}
		return nil, fmt.Errorf("ctxpins: reading pin file: %w", err)
	}
	var pf PinFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return emptyPinFile(), fmt.Errorf("ctxpins: %w at %s: %v", ErrCorrupt, s.path, err)
	}
	switch pf.Version {
	case "", FileVersion:
	default:
		return emptyPinFile(), fmt.Errorf("ctxpins: %w: %s has version %q; upgrade gridctl to use it", ErrNewerVersion, s.path, pf.Version)
	}
	if pf.Fragments == nil {
		pf.Fragments = map[string]*FragmentPin{}
	}
	return &pf, nil
}

func (s *Store) loadLog(ctx context.Context) (*pinapprovals.Log, error) {
	log := pinapprovals.Open(s.logPath, "", pinapprovals.KindContext)
	err := log.Load(ctx)
	if err == nil {
		return log, nil
	}
	if errors.Is(err, pinapprovals.ErrCorrupt) {
		slog.Warn("ctxpins: approvals log unreadable; continuing with empty history", "path", s.logPath, "error", err)
		return log, nil
	}
	if errors.Is(err, pinapprovals.ErrNewerVersion) {
		return nil, fmt.Errorf("ctxpins: %w: %v", ErrNewerVersion, err)
	}
	return nil, fmt.Errorf("ctxpins: loading approvals log: %w", err)
}

func (s *Store) save(pf *PinFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), dirPerm); err != nil {
		return fmt.Errorf("ctxpins: creating pins directory: %w", err)
	}
	pf.Version = FileVersion
	if pf.Fragments == nil {
		pf.Fragments = map[string]*FragmentPin{}
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("ctxpins: marshaling pin file: %w", err)
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("ctxpins: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			slog.Warn("ctxpins: failed to remove temp file after rename error", "path", tmp, "error", removeErr)
		}
		return fmt.Errorf("ctxpins: renaming temp file: %w", err)
	}
	return nil
}

func emptyPinFile() *PinFile {
	return &PinFile{
		Version:   FileVersion,
		Fragments: map[string]*FragmentPin{},
	}
}
