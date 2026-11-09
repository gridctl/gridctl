package proposals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	lockName    = "proposals"
	lockTimeout = 5 * time.Second
	filePerm    = os.FileMode(0o600)
	dirPerm     = os.FileMode(0o700)
	idAttempts  = 8
)

var (
	idPattern       = regexp.MustCompile(`^p-[0-9a-f]{12}$`)
	baseHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Store holds proposals, one JSON file each. It keeps only its directory
// and scan settings; every public method reloads under state.WithLock.
type Store struct {
	mu  sync.Mutex
	dir string

	scanSet     bool
	scanEnabled bool
	scanIgnore  []string

	// newID is replaced by tests to force an id collision.
	newID func() (string, error)
}

// New creates a Store for ~/.gridctl/proposals. The directory is resolved
// on first use. Scan settings default to ctxpins.DeployedScanConfig so a
// deployed stack's scan_ignore applies.
func New() *Store {
	return &Store{newID: randomID}
}

// NewWithPath creates a Store rooted at dir with the scanner on and no
// ignores. Intended for tests; it never resolves the real home.
func NewWithPath(dir string) *Store {
	return &Store{dir: dir, scanSet: true, scanEnabled: true, newID: randomID}
}

// SetScanConfig configures the advisory scanner used for findings.
func (s *Store) SetScanConfig(enabled bool, ignore []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanSet = true
	s.scanEnabled = enabled
	s.scanIgnore = append([]string(nil), ignore...)
}

func (s *Store) scanSettings() (bool, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.scanSet {
		s.scanEnabled, s.scanIgnore = ctxpins.DeployedScanConfig()
		s.scanSet = true
	}
	return s.scanEnabled, append([]string(nil), s.scanIgnore...)
}

func (s *Store) ensureDir() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir != "" {
		return s.dir, nil
	}
	dir, err := state.ProposalsDir()
	if err != nil {
		return "", fmt.Errorf("proposals: resolving proposals directory: %w", err)
	}
	s.dir = dir
	return dir, nil
}

// Dir returns the proposals directory, resolving it if needed.
func (s *Store) Dir() (string, error) {
	return s.ensureDir()
}

func (s *Store) filePath(dir, id string) string {
	return filepath.Join(dir, id+".json")
}

// Create validates a create document, computes findings, path, and
// provenance against the current target, and stores the proposal. Every
// refusal happens before any file is written. A diff that does not apply
// to a current base is refused with ErrApplyFailed; a stale base is stored
// with base_current false.
func (s *Store) Create(ctx context.Context, req CreateRequest, targets Resolver) (*Proposal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := validateCreate(req)
	if err != nil {
		return nil, err
	}
	patches, err := ParseUnified(req.Diff)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return nil, fmt.Errorf("proposals: %w: no target resolver", ErrStoreUnavailable)
	}
	target, err := targets.Resolve(ctx, p.Kind, p.Target)
	if err != nil {
		return nil, err
	}
	paths := patchPaths(patches)
	current, err := target.ReadFiles(ctx, paths)
	if err != nil {
		return nil, err
	}
	hash, err := target.CurrentHash(ctx)
	if err != nil {
		return nil, err
	}
	p.BaseCurrent = hash == p.BaseHash
	enabled, ignore := s.scanSettings()

	applied, applyErr := applyAll(current, patches)
	if applyErr == nil && p.BaseCurrent && target.Kind() != KindHook {
		if _, err := target.PostApplyHash(ctx, applied); err != nil {
			applyErr = err
		}
	}
	switch {
	case applyErr != nil && p.BaseCurrent:
		return nil, applyErr
	case p.Kind == KindHook:
		p.Findings = scanText(p.Target, req.Diff, enabled, ignore)
	case applyErr != nil:
		p.Findings = scanAdded(p.Target, patches, enabled, ignore)
	default:
		findings, err := target.Scan(ctx, applied, enabled, ignore)
		if err != nil {
			return nil, err
		}
		p.Findings = findings
	}
	if p.Findings == nil {
		p.Findings = []pins.Finding{}
	}
	prov, err := target.Provenance(ctx)
	if err != nil {
		return nil, err
	}
	p.Provenance = prov
	p.Path = decidePath(p.Kind, prov)
	p.Files = paths

	err = s.withLock(ctx, func(dir string) error {
		for attempt := 0; attempt < idAttempts; attempt++ {
			id, err := s.newID()
			if err != nil {
				return fmt.Errorf("proposals: generating id: %w", err)
			}
			if _, err := os.Lstat(s.filePath(dir, id)); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("proposals: checking id: %w", err)
			}
			p.ID = id
			return s.save(dir, p)
		}
		return errors.New("proposals: could not allocate a unique id")
	})
	if err != nil {
		return nil, err
	}
	slog.Info("proposals: staged proposal",
		"id", p.ID,
		"kind", p.Kind,
		"target", p.Target,
		"path", p.Path,
		"files", len(p.Files),
		"findings", len(p.Findings),
		"evidence_count", len(p.Evidence),
		"base_current", p.BaseCurrent,
	)
	return p, nil
}

// validateCreate checks every field that needs no target. It returns the
// proposal skeleton with validated and normalized values.
func validateCreate(req CreateRequest) (*Proposal, error) {
	switch req.Kind {
	case KindSkill, KindContext, KindHook:
	case KindAgent:
		return nil, fmt.Errorf("proposals: %w: kind agent is reserved; agent pins do not exist yet", ErrKindUnsupported)
	default:
		return nil, fmt.Errorf("proposals: %w: kind must be skill, context, or hook", ErrInvalidInput)
	}
	if strings.TrimSpace(req.Target) == "" {
		return nil, fmt.Errorf("proposals: %w: target is required", ErrInvalidInput)
	}
	if req.BaseHash == "" || !baseHashPattern.MatchString(req.BaseHash) {
		what := "is required"
		if req.BaseHash != "" {
			what = "is not a composite hash"
		}
		return nil, fmt.Errorf("proposals: %w: base_hash %s; run 'gridctl proposals base %s %s' or '%s' and copy composite_hash",
			ErrInvalidInput, what, req.Kind, req.Target, pinsDiffCommand(req.Kind, req.Target))
	}
	if req.Diff == "" {
		return nil, fmt.Errorf("proposals: %w: diff is required", ErrInvalidInput)
	}
	if len(req.Diff) > MaxDiffBytes {
		return nil, fmt.Errorf("proposals: %w: diff is larger than %d bytes", ErrInvalidInput, MaxDiffBytes)
	}
	switch req.Classification {
	case ClassKnowledge, ClassGuarantee:
	default:
		return nil, fmt.Errorf("proposals: %w: classification must be knowledge or guarantee", ErrInvalidInput)
	}
	if req.Prevalence.Sessions < 0 || req.Prevalence.Runs < 0 {
		return nil, fmt.Errorf("proposals: %w: prevalence counts must be non-negative", ErrInvalidInput)
	}
	if len(req.Source) > MaxSourceBytes {
		return nil, fmt.Errorf("proposals: %w: source is longer than %d bytes", ErrInvalidInput, MaxSourceBytes)
	}
	if err := pinapprovals.ValidateReason(req.Source); err != nil {
		return nil, fmt.Errorf("proposals: source: %w", err)
	}
	evidence, err := pinapprovals.ValidateReferences(req.Evidence)
	if err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	if err := checkEvidenceCount(len(evidence)); err != nil {
		return nil, err
	}
	if err := pinapprovals.ValidateReason(req.Reason); err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	if err := pinapprovals.ValidateApprover(req.CreatedBy); err != nil {
		return nil, fmt.Errorf("proposals: created_by: %w", err)
	}
	by, bySource := pinapprovals.ResolveApprover(strings.TrimSpace(req.Principal), strings.TrimSpace(req.CreatedBy))
	if err := pinapprovals.ValidateApprover(by); err != nil {
		return nil, fmt.Errorf("proposals: created_by: %w", err)
	}
	if evidence == nil {
		evidence = []string{}
	}
	return &Proposal{
		Version:         FileVersion,
		Kind:            req.Kind,
		Target:          req.Target,
		BaseHash:        req.BaseHash,
		Diff:            req.Diff,
		Classification:  req.Classification,
		Prevalence:      req.Prevalence,
		Evidence:        evidence,
		Reason:          req.Reason,
		Source:          req.Source,
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
		CreatedBy:       strings.TrimSpace(by),
		CreatedBySource: bySource,
		Status:          StatusPending,
	}, nil
}

// pinsDiffCommand names the kind's pins diff command that prints
// composite_hash.
func pinsDiffCommand(kind, name string) string {
	switch kind {
	case KindContext:
		return fmt.Sprintf("gridctl ctx pins diff %s --format json", name)
	case KindHook:
		return fmt.Sprintf("gridctl skill pins diff %s --kind hook --format json", name)
	default:
		return fmt.Sprintf("gridctl skill pins diff %s --format json", name)
	}
}

// decidePath is the single place the apply/record decision is made.
// Hooks are always record-only; any provenance other than local records.
func decidePath(kind string, prov Provenance) string {
	if kind == KindHook || prov.Kind != ProvenanceLocal {
		return PathRecord
	}
	return PathApply
}

// applyAll applies every patch in memory. current holds existing files;
// a missing key is an absent file.
func applyAll(current map[string][]byte, patches []FilePatch) (map[string][]byte, error) {
	applied := make(map[string][]byte, len(patches))
	for _, p := range patches {
		data, ok := current[p.Path]
		var in []byte
		if ok {
			in = data
			if in == nil {
				in = []byte{}
			}
		}
		out, err := ApplyStrict(in, p)
		if err != nil {
			return nil, err
		}
		applied[p.Path] = out
	}
	return applied, nil
}

func scanText(name, text string, enabled bool, ignore []string) []pins.Finding {
	if !enabled {
		return nil
	}
	return pins.FilterFindings(pins.ScanSkill(name, "", text), ignore)
}

func scanAdded(name string, patches []FilePatch, enabled bool, ignore []string) []pins.Finding {
	var out []pins.Finding
	for _, p := range patches {
		out = append(out, scanText(name, addedText(p), enabled, ignore)...)
	}
	return out
}

// List reads up to MaxListFiles proposal files, applies f, and sorts
// pending first, then newest first. A file that fails to load is reported
// in Skipped by category and does not stop the listing.
func (s *Store) List(ctx context.Context, f Filter) (ListResult, error) {
	var res ListResult
	switch f.Status {
	case "", "all", StatusPending, StatusApproved, StatusRejected:
	default:
		return res, fmt.Errorf("proposals: %w: status must be pending, approved, rejected, or all", ErrInvalidInput)
	}
	err := s.withLock(ctx, func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("proposals: reading proposals directory: %w", err)
		}
		read := 0
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") {
				continue
			}
			if read >= MaxListFiles {
				res.Truncated = true
				break
			}
			read++
			id := strings.TrimSuffix(name, ".json")
			path := filepath.Join(dir, name)
			p, err := s.load(dir, id)
			if err != nil {
				res.Skipped = append(res.Skipped, Skipped{Path: path, Category: loadCategory(err)})
				continue
			}
			if f.Status != "" && f.Status != "all" && p.Status != f.Status {
				continue
			}
			if f.Kind != "" && p.Kind != f.Kind {
				continue
			}
			res.Proposals = append(res.Proposals, p)
		}
		return nil
	})
	if err != nil {
		return ListResult{}, err
	}
	sortProposals(res.Proposals)
	return res, nil
}

// sortProposals orders pending first, then newest first, then by id.
func sortProposals(ps []*Proposal) {
	sort.SliceStable(ps, func(i, j int) bool {
		pi, pj := ps[i].Status == StatusPending, ps[j].Status == StatusPending
		if pi != pj {
			return pi
		}
		if !ps[i].CreatedAt.Equal(ps[j].CreatedAt) {
			return ps[i].CreatedAt.After(ps[j].CreatedAt)
		}
		return ps[i].ID < ps[j].ID
	})
}

// loadCategory names a load failure without the path or contents.
func loadCategory(err error) string {
	switch {
	case errors.Is(err, ErrCorrupt):
		return "corrupt"
	case errors.Is(err, ErrNewerVersion):
		return "newer_version"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	default:
		return "unreadable"
	}
}

// Get loads one proposal. An unknown or malformed id wraps ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (*Proposal, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("proposals: %w: %q", ErrNotFound, id)
	}
	var p *Proposal
	err := s.withLock(ctx, func(dir string) error {
		var err error
		p, err = s.load(dir, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Annotate refreshes the computed fields of a loaded proposal without
// writing: base_current against the target's current hash, and pin_status.
// A target or pin store that cannot be read leaves base_current as stored
// and sets pin_status to unknown.
func Annotate(ctx context.Context, p *Proposal, targets Resolver) {
	if p == nil {
		return
	}
	p.PinStatus = PinStatusUnknown
	if targets == nil {
		return
	}
	target, err := targets.Resolve(ctx, p.Kind, p.Target)
	if err != nil {
		return
	}
	if hash, err := target.CurrentHash(ctx); err == nil {
		p.BaseCurrent = hash == p.BaseHash
	}
	if status, err := target.PinStatus(ctx); err == nil {
		p.PinStatus = status
	}
}

// Reject records a rejection. The reason is required and validated as an
// approval reason. Nothing is written to the target or any approvals log.
func (s *Store) Reject(ctx context.Context, id string, req RejectRequest) (*Proposal, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("proposals: %w: %q", ErrNotFound, id)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("proposals: %w: reject requires a reason", ErrInvalidInput)
	}
	if err := pinapprovals.ValidateReason(req.Reason); err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	by := strings.TrimSpace(req.ApprovedBy)
	if err := pinapprovals.ValidateApprover(by); err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	var out *Proposal
	err := s.withLock(ctx, func(dir string) error {
		p, err := s.load(dir, id)
		if err != nil {
			return err
		}
		if err := requirePending(p); err != nil {
			return err
		}
		source := req.ApprovedBySource
		if by == "" {
			source = ""
		}
		p.Status = StatusRejected
		p.Verdict = &Verdict{
			At:       time.Now().UTC().Truncate(time.Second),
			By:       by,
			BySource: source,
			Reason:   req.Reason,
			Applied:  false,
		}
		if err := s.save(dir, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	slog.Info("proposals: rejected proposal", "id", id, "kind", out.Kind, "target", out.Target)
	return out, nil
}

func requirePending(p *Proposal) error {
	if p.Status == StatusPending {
		return nil
	}
	detail := p.Status
	if p.Verdict != nil {
		by := p.Verdict.By
		if by == "" {
			by = "unknown"
		}
		detail = fmt.Sprintf("%s (%s by %s)", p.Status, p.Verdict.At.UTC().Format("2006-01-02"), by)
	}
	return fmt.Errorf("proposals: proposal %s is already %s: %w", p.ID, detail, ErrNotPending)
}

// withLock runs fn under the proposals file lock with the resolved
// directory.
func (s *Store) withLock(ctx context.Context, fn func(dir string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := s.ensureDir()
	if err != nil {
		return err
	}
	return state.WithLock(lockName, lockTimeout, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(dir)
	})
}

// load reads one proposal file. A missing file wraps ErrNotFound, a parse
// failure wraps ErrCorrupt, and a version other than empty or FileVersion
// wraps ErrNewerVersion. Such a file is never rewritten.
func (s *Store) load(dir, id string) (*Proposal, error) {
	path := s.filePath(dir, id)
	data, err := os.ReadFile(path) // #nosec G304 -- path is <proposals dir>/<validated id>.json
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("proposals: %w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("proposals: reading %s: %w", path, err)
	}
	var head struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("proposals: %w at %s: %v", ErrCorrupt, path, err)
	}
	switch head.Version {
	case "", FileVersion:
	default:
		return nil, fmt.Errorf("proposals: %w: %s has version %q; upgrade gridctl to use it", ErrNewerVersion, path, head.Version)
	}
	var p Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("proposals: %w at %s: %v", ErrCorrupt, path, err)
	}
	if p.ID != id {
		return nil, fmt.Errorf("proposals: %w at %s: id %q does not match the file name", ErrCorrupt, path, p.ID)
	}
	p.PinStatus = ""
	if p.Evidence == nil {
		p.Evidence = []string{}
	}
	if p.Findings == nil {
		p.Findings = []pins.Finding{}
	}
	if p.Files == nil {
		p.Files = []string{}
	}
	return &p, nil
}

// save writes one proposal atomically (temp file then rename) with mode
// 0600. The computed pin_status is never stored.
func (s *Store) save(dir string, p *Proposal) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("proposals: creating proposals directory: %w", err)
	}
	cp := *p
	cp.Version = FileVersion
	cp.PinStatus = ""
	data, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return fmt.Errorf("proposals: marshaling proposal: %w", err)
	}
	data = append(data, '\n')
	path := s.filePath(dir, p.ID)
	tmp, err := os.CreateTemp(dir, "."+p.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("proposals: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		if rerr := os.Remove(tmpName); rerr != nil && !os.IsNotExist(rerr) {
			slog.Warn("proposals: failed to remove temp file", "path", tmpName, "error", rerr)
		}
	}
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("proposals: setting temp file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("proposals: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("proposals: closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("proposals: renaming temp file: %w", err)
	}
	p.Version = FileVersion
	return nil
}

func randomID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "p-" + hex.EncodeToString(b[:]), nil
}
