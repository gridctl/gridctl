package hookpins

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
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	lockName    = "hookpins"
	lockTimeout = 5 * time.Second
	filePerm    = os.FileMode(0o600)
)

// Sentinel errors. Load's ErrCorrupt and ErrNewerVersion follow the
// skill pin contract: a corrupt file is reported and not rewritten;
// ErrNewerVersion must never be papered over.
var (
	ErrCorrupt           = errors.New("corrupt hook pin file")
	ErrNewerVersion      = errors.New("hook pin file written by a newer gridctl")
	ErrNotPinned         = errors.New("hook is not pinned")
	ErrNotFound          = errors.New("hook not found")
	ErrHashMismatch      = errors.New("hook content changed since the reviewed diff")
	ErrReasonRequired    = errors.New("approving a hook with unresolved findings requires a reason")
	ErrDigestUnavailable = errors.New("hook content could not be hashed")
)

// Store manages TOFU content pins for hooks. Writes hold a file lock and
// are atomic. The store is not stack-scoped.
type Store struct {
	path    string
	logPath string

	mu   sync.Mutex
	data *PinFile
	log  *pinapprovals.Log
}

// New creates a Store for ~/.gridctl/pins/hooks.json. The path is resolved
// on first use so construction stays error-free.
func New() *Store {
	return &Store{}
}

// NewWithPath creates a Store that keeps pins in dir/hooks.json.
// Intended for tests; it never resolves the real home.
func NewWithPath(dir string) *Store {
	return &Store{
		path:    filepath.Join(dir, "hooks.json"),
		logPath: filepath.Join(dir, "approvals", "hooks", "global.json"),
	}
}

func (s *Store) ensurePath() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensurePathLocked()
}

func (s *Store) ensurePathLocked() error {
	if s.path != "" {
		if s.logPath == "" {
			s.logPath = filepath.Join(filepath.Dir(s.path), "approvals", "hooks", "global.json")
		}
		return nil
	}
	p, err := state.HookPinsPath()
	if err != nil {
		return fmt.Errorf("hookpins: resolving pin file path: %w", err)
	}
	lp, err := state.HookPinApprovalsPath()
	if err != nil {
		return fmt.Errorf("hookpins: resolving approvals log path: %w", err)
	}
	s.path = p
	s.logPath = lp
	return nil
}

// Load reads the pin file. A missing file starts empty. A corrupt file
// resets to empty and wraps ErrCorrupt. A newer version resets to empty
// and wraps ErrNewerVersion with the skill-pin wording shape.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensurePathLocked(); err != nil {
		return err
	}
	pinErr := s.loadPinFileLocked()
	logErr := s.loadApprovalsLocked(context.Background())
	if logErr != nil {
		return logErr
	}
	if pinErr != nil {
		return pinErr
	}
	s.projectApprovalsLocked()
	return nil
}

func (s *Store) loadPinFileLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = s.emptyFile()
			return nil
		}
		return fmt.Errorf("hookpins: reading pin file: %w", err)
	}
	var pf PinFile
	if err := json.Unmarshal(data, &pf); err != nil {
		s.data = s.emptyFile()
		return fmt.Errorf("hookpins: %w at %s: %v", ErrCorrupt, s.path, err)
	}
	switch pf.Version {
	case "", FileVersion:
	default:
		s.data = s.emptyFile()
		return fmt.Errorf("hookpins: %w: %s has version %q; upgrade gridctl to use it", ErrNewerVersion, s.path, pf.Version)
	}
	if pf.Hooks == nil {
		pf.Hooks = map[string]*HookPin{}
	}
	s.data = &pf
	return nil
}

func (s *Store) loadApprovalsLocked(ctx context.Context) error {
	if s.log == nil {
		s.log = pinapprovals.Open(s.logPath, "", pinapprovals.KindHook)
	}
	err := s.log.Load(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, pinapprovals.ErrCorrupt) {
		slog.Warn("hookpins: approvals log unreadable; continuing with empty history", "path", s.logPath, "error", err)
		return nil
	}
	if errors.Is(err, pinapprovals.ErrNewerVersion) {
		return fmt.Errorf("hookpins: %w: %v", ErrNewerVersion, err)
	}
	return fmt.Errorf("hookpins: loading approvals log: %w", err)
}

func (s *Store) projectApprovalsLocked() {
	if s.data == nil || s.log == nil {
		return
	}
	for name, pin := range s.data.Hooks {
		if pin == nil {
			continue
		}
		pin.Approvals = pinapprovals.CloneEntries(s.log.History(name))
		pin.CompositeHash = composite(pin.DefinitionHash, pin.Files)
	}
}

func (s *Store) emptyFile() *PinFile {
	return &PinFile{Version: FileVersion, Hooks: map[string]*HookPin{}}
}

// Get returns a copy of one pin. The bool is false when the name is not
// pinned. Load must have succeeded.
func (s *Store) Get(name string) (*HookPin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, false
	}
	pin := s.data.Hooks[name]
	if pin == nil {
		return nil, false
	}
	return clonePin(pin), true
}

// Pins returns a copy of every pin, keyed by name.
func (s *Store) Pins() map[string]*HookPin {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return map[string]*HookPin{}
	}
	out := make(map[string]*HookPin, len(s.data.Hooks))
	for name, pin := range s.data.Hooks {
		out[name] = clonePin(pin)
	}
	return out
}

// Pin records the bytes currently installed in req.Dir. An accepted import
// calls this, including when the bytes changed. It does not append an
// approval entry.
func (s *Store) Pin(ctx context.Context, req PinRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.Name == "" || req.Dir == "" {
		return fmt.Errorf("hookpins: pin requires a name and directory")
	}
	defHash, document, files, err := digestTree(req.Dir)
	if err != nil {
		return err
	}
	return s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.ensurePathLocked(); err != nil {
			return err
		}
		if err := s.loadPinFileLocked(); err != nil {
			return err
		}
		if err := s.loadApprovalsLocked(ctx); err != nil {
			return err
		}
		now := time.Now().UTC()
		pinnedAt := now
		if existing := s.data.Hooks[req.Name]; existing != nil && !existing.PinnedAt.IsZero() {
			pinnedAt = existing.PinnedAt
		}
		source := req.Source
		if source == "" {
			if req.Origin != nil && req.Origin.Repo != "" {
				source = SourceGit
			} else {
				source = SourceLocal
			}
		}
		pin := &HookPin{
			DefinitionHash: defHash,
			Files:          files,
			Document:       document,
			Source:         source,
			Origin:         req.Origin,
			PinnedAt:       pinnedAt,
			LastVerifiedAt: now,
			Status:         StatusPinned,
			Findings:       append([]pins.Finding(nil), req.Findings...),
		}
		pin.Approvals = pinapprovals.CloneEntries(s.log.History(req.Name))
		pin.CompositeHash = composite(defHash, files)
		s.data.Hooks[req.Name] = pin
		return s.saveLocked()
	})
}

// Sync recomputes digests for pinned hooks and marks drift. It does not
// replace pinned bytes. A missing hook directory is reported and kept.
func (s *Store) Sync(ctx context.Context, registryDir string) (*SyncResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &SyncResult{}
	err := s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.ensurePathLocked(); err != nil {
			return err
		}
		if err := s.loadPinFileLocked(); err != nil {
			return err
		}
		if err := s.loadApprovalsLocked(ctx); err != nil {
			return err
		}
		s.projectApprovalsLocked()
		now := time.Now().UTC()
		dirty := false
		names := make([]string, 0, len(s.data.Hooks))
		for name := range s.data.Hooks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			pin := s.data.Hooks[name]
			dir := filepath.Join(registryDir, "hooks", name)
			if _, statErr := os.Stat(dir); statErr != nil {
				result.Missing = append(result.Missing, name)
				continue
			}
			defHash, _, files, derr := digestTree(dir)
			if derr != nil {
				if pin.Status != StatusDrift {
					pin.Status = StatusDrift
					dirty = true
				}
				result.Drifted = append(result.Drifted, name)
				continue
			}
			status := StatusPinned
			if defHash != pin.DefinitionHash || !equalDigests(pin.Files, files) {
				status = StatusDrift
				result.Drifted = append(result.Drifted, name)
			}
			pin.LastVerifiedAt = now
			if pin.Status != status {
				pin.Status = status
				dirty = true
			}
		}
		if !dirty {
			return nil
		}
		return s.saveLocked()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Verify builds the diff for one hook without writing. CompositeHash is the
// fingerprint of the current bytes, the value approve --expect binds to.
func (s *Store) Verify(name, registryDir string) (*VerifyResult, error) {
	dir := filepath.Join(registryDir, "hooks", name)
	if _, err := os.Stat(filepath.Join(dir, "hook.json")); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("hookpins: hook %q: %w", name, ErrNotFound)
		}
		return nil, err
	}
	defHash, document, files, err := digestTree(dir)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, fmt.Errorf("hookpins: store is not loaded")
	}
	pin := s.data.Hooks[name]
	if pin == nil {
		return nil, fmt.Errorf("hookpins: hook %q: %w", name, ErrNotPinned)
	}
	result := &VerifyResult{Name: name, Status: StatusPinned, CompositeHash: composite(defHash, files)}
	added, removed, modified := skillpinsDiff(pin.Files, files)
	if defHash != pin.DefinitionHash || len(added)+len(removed)+len(modified) > 0 {
		result.Status = StatusDrift
		result.Diff = &HookDiff{
			Name:          name,
			OldHash:       pin.DefinitionHash,
			NewHash:       defHash,
			OldDocument:   pin.Document,
			NewDocument:   document,
			AddedFiles:    emptyIfNil(added),
			RemovedFiles:  emptyIfNil(removed),
			ModifiedFiles: emptyIfNil(modified),
			Findings:      append([]pins.Finding(nil), pin.Findings...),
		}
	}
	return result, nil
}

// Diff is Verify's diff. A clean pin returns a diff with equal hashes and
// status pinned so callers can still print the composite hash.
func (s *Store) Diff(name, registryDir string) (*VerifyResult, error) {
	return s.Verify(name, registryDir)
}

// CurrentCompositeHash is the approval fingerprint of the hook's current bytes.
func (s *Store) CurrentCompositeHash(name, registryDir string) (string, error) {
	dir := filepath.Join(registryDir, "hooks", name)
	defHash, _, files, err := digestTree(dir)
	if err != nil {
		return "", err
	}
	return composite(defHash, files), nil
}

// Approve re-pins the hook's current bytes and appends an approval entry.
// The log is appended inside the file lock before the pin file is rewritten.
func (s *Store) Approve(ctx context.Context, name, registryDir string, req ApproveRequest) error {
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
	dir := filepath.Join(registryDir, "hooks", name)
	defHash, document, files, err := digestTree(dir)
	if err != nil {
		return err
	}
	comp := composite(defHash, files)
	if req.ExpectedHash != "" && req.ExpectedHash != comp {
		return fmt.Errorf("hookpins: hook %q: %w", name, ErrHashMismatch)
	}
	err = s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.ensurePathLocked(); err != nil {
			return err
		}
		if err := s.loadPinFileLocked(); err != nil {
			return err
		}
		pin := s.data.Hooks[name]
		if pin == nil {
			return fmt.Errorf("hookpins: hook %q: %w", name, ErrNotPinned)
		}
		findings := req.Findings
		if len(findings) == 0 {
			findings = pin.Findings
		}
		if len(findings) > 0 && req.Reason == "" {
			return fmt.Errorf("hookpins: hook %q has %d unresolved finding(s): %w", name, len(findings), ErrReasonRequired)
		}
		if err := s.loadApprovalsLocked(ctx); err != nil {
			return err
		}
		by, source := approvedBy, req.ApprovedBySource
		if by == "" {
			source = ""
		}
		entry := pinapprovals.Entry{
			ApprovedAt:       time.Now().UTC(),
			ApprovedBy:       by,
			ApprovedBySource: source,
			ApprovedHash:     comp,
			Bound:            req.ExpectedHash != "",
			Reason:           req.Reason,
			Evidence:         evidence,
		}
		if err := s.log.Append(ctx, name, entry); err != nil {
			return err
		}
		now := time.Now().UTC()
		pin.DefinitionHash = defHash
		pin.Files = files
		pin.Document = document
		pin.ApprovedReason = req.Reason
		pin.LastVerifiedAt = now
		pin.Status = StatusPinned
		if len(req.Findings) > 0 {
			pin.Findings = append([]pins.Finding(nil), req.Findings...)
		}
		pin.Approvals = pinapprovals.CloneEntries(s.log.History(name))
		pin.CompositeHash = comp
		return s.saveLocked()
	})
	if err != nil {
		return err
	}
	slog.Info("hookpins: recorded approval", "hook", name, "bound", req.ExpectedHash != "", "has_reason", req.Reason != "")
	return nil
}

// Reset deletes the pin record. Approval history is left in the log.
func (s *Store) Reset(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.ensurePathLocked(); err != nil {
			return err
		}
		if err := s.loadPinFileLocked(); err != nil {
			return err
		}
		if _, ok := s.data.Hooks[name]; !ok {
			return fmt.Errorf("hookpins: hook %q: %w", name, ErrNotPinned)
		}
		delete(s.data.Hooks, name)
		return s.saveLocked()
	})
}

func (s *Store) withFileLock(fn func() error) error {
	if err := s.ensurePath(); err != nil {
		return err
	}
	return state.WithLock(lockName, lockTimeout, fn)
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("hookpins: creating pins directory: %w", err)
	}
	s.data.Version = FileVersion
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("hookpins: marshaling pin file: %w", err)
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("hookpins: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("hookpins: renaming temp file: %w", err)
	}
	return nil
}

func clonePin(in *HookPin) *HookPin {
	if in == nil {
		return nil
	}
	out := *in
	out.Files = append([]skillpins.FileDigest(nil), in.Files...)
	out.Findings = append([]pins.Finding(nil), in.Findings...)
	out.Approvals = pinapprovals.CloneEntries(in.Approvals)
	if in.Origin != nil {
		cp := *in.Origin
		out.Origin = &cp
	}
	return &out
}

func equalDigests(a, b []skillpins.FileDigest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func skillpinsDiff(pinned, current []skillpins.FileDigest) (added, removed, modified []string) {
	old := map[string]string{}
	for _, f := range pinned {
		old[f.Path] = f.Digest
	}
	seen := map[string]bool{}
	for _, f := range current {
		seen[f.Path] = true
		prev, ok := old[f.Path]
		switch {
		case !ok:
			added = append(added, f.Path)
		case prev != f.Digest:
			modified = append(modified, f.Path)
		}
	}
	for _, f := range pinned {
		if !seen[f.Path] {
			removed = append(removed, f.Path)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(modified)
	return added, removed, modified
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
