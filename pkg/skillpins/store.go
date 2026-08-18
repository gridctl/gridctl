package skillpins

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	lockTimeout = 5 * time.Second
	fileVersion = "1"
	filePerm    = os.FileMode(0600)
)

// Sentinel errors. Load's ErrCorrupt/ErrNewerVersion follow the pkg/pins
// contract: a caller preferring availability (the daemon) may continue past
// ErrCorrupt with the empty store Load leaves behind; ErrNewerVersion must
// never be papered over. The rest are decision errors surfaced by Approve
// and the diff paths so the CLI and API can map them to exit codes and
// status codes without string matching.
var (
	ErrCorrupt        = errors.New("corrupt skill pin file")
	ErrNewerVersion   = errors.New("skill pin file written by a newer gridctl")
	ErrNotPinned      = errors.New("skill is not pinned")
	ErrHashMismatch   = errors.New("skill content changed since the reviewed diff")
	ErrReasonRequired = errors.New("approving a skill with unresolved findings requires a reason")
)

// Store manages TOFU content pins for a stack's skill registry.
// It is safe for concurrent use: in-memory access is guarded by a RWMutex,
// and disk writes are serialized across processes via state.WithLock. The
// pin file lives under ~/.gridctl/pins/, deliberately outside the watched
// registry tree, so pin writes can never re-trigger the registry watcher.
type Store struct {
	stackName string
	path      string
	mu        sync.RWMutex
	data      *PinFile

	// scanEnabled and scanIgnore mirror pkg/pins' advisory scanner knobs:
	// they never affect hashing or drift.
	scanEnabled bool
	scanIgnore  []string
}

// New creates a Store for the given stack name.
// The pin file lives at ~/.gridctl/pins/skills/{stackName}.json.
// Call Load() before performing verification or pinning operations.
func New(stackName string) *Store {
	ps := &Store{
		stackName:   stackName,
		scanEnabled: true,
	}
	ps.data = ps.emptyPinFile()
	return ps
}

// ensurePath resolves the on-disk location lazily so New stays error-free;
// resolution fails only when no home directory is available. Callers hold
// ps.mu.
func (ps *Store) ensurePath() error {
	if ps.path != "" {
		return nil
	}
	p, err := state.SkillPinsPath(ps.stackName)
	if err != nil {
		return fmt.Errorf("skillpins: resolving pin file path: %w", err)
	}
	ps.path = p
	return nil
}

// NewWithPath creates a Store that keeps pins in dir/{stackName}.skills.json.
// Intended for testing where the real state directory should not be used.
func NewWithPath(dir, stackName string) *Store {
	ps := &Store{
		stackName:   stackName,
		path:        filepath.Join(dir, stackName+".skills.json"),
		scanEnabled: true,
	}
	ps.data = ps.emptyPinFile()
	return ps
}

// SetScanConfig configures the advisory poisoning scanner: enabled toggles
// it, ignore suppresses findings by code. Call before the store starts
// pinning or verifying.
func (ps *Store) SetScanConfig(enabled bool, ignore []string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.scanEnabled = enabled
	ps.scanIgnore = append([]string(nil), ignore...)
}

// scanFindings runs the poisoning scan for one skill under the store's scan
// settings. Caller must hold ps.mu (read or write).
func (ps *Store) scanFindings(sk *registry.AgentSkill) []pins.Finding {
	if !ps.scanEnabled {
		return nil
	}
	return pins.FilterFindings(pins.ScanSkill(sk.Name, sk.Description, sk.Body), ps.scanIgnore)
}

// Load reads the pin file from disk into memory. A missing file starts the
// store empty (ready for first pin); a corrupt file resets to empty and
// wraps ErrCorrupt; a newer-version file resets to empty and wraps
// ErrNewerVersion.
func (ps *Store) Load() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if err := ps.ensurePath(); err != nil {
		return err
	}
	data, err := os.ReadFile(ps.path)
	if err != nil {
		if os.IsNotExist(err) {
			ps.data = ps.emptyPinFile()
			return nil
		}
		return fmt.Errorf("skillpins: reading pin file: %w", err)
	}

	var pf PinFile
	if err := json.Unmarshal(data, &pf); err != nil {
		ps.data = ps.emptyPinFile()
		return fmt.Errorf("skillpins: %w at %s: %v", ErrCorrupt, ps.path, err)
	}
	switch pf.Version {
	case "", fileVersion:
	default:
		ps.data = ps.emptyPinFile()
		return fmt.Errorf("skillpins: %w: %s has version %q; upgrade gridctl to use it", ErrNewerVersion, ps.path, pf.Version)
	}
	if pf.Skills == nil {
		pf.Skills = make(map[string]*SkillPin)
	}
	ps.data = &pf
	return nil
}

// GetAll returns a deep-copied snapshot of every skill pin, keyed by skill
// name. Copies for the same reason pkg/pins copies: callers marshal outside
// the lock while Sync mutates records in place.
func (ps *Store) GetAll() map[string]*SkillPin {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	out := make(map[string]*SkillPin, len(ps.data.Skills))
	for name, pin := range ps.data.Skills {
		out[name] = copySkillPin(pin)
	}
	return out
}

// Get returns a deep-copied pin for one skill.
func (ps *Store) Get(name string) (*SkillPin, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	pin, ok := ps.data.Skills[name]
	if !ok {
		return nil, false
	}
	return copySkillPin(pin), true
}

func copySkillPin(pin *SkillPin) *SkillPin {
	if pin == nil {
		return nil
	}
	out := *pin
	out.Files = append([]FileDigest(nil), pin.Files...)
	out.Findings = append([]pins.Finding(nil), pin.Findings...)
	if pin.Origin != nil {
		origin := *pin.Origin
		out.Origin = &origin
	}
	return &out
}

// Sync is the primary entry point, called after every registry refresh. It
// TOFU-pins skills seen for the first time (silently), verifies the rest,
// and marks drift. It never auto-approves and never prunes records for
// skills missing from the registry — both wait for a human. One save per
// pass, only when something changed.
func (ps *Store) Sync(src SkillSource) (*SyncResult, error) {
	result := &SyncResult{}
	err := ps.withFileLock(func() error {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		now := time.Now().UTC()
		dirty := false
		seen := make(map[string]bool)

		for _, sk := range src.ListSkills() {
			seen[sk.Name] = true
			skillHash, files, err := ComputeDigests(src, sk)
			if err != nil {
				// Fail closed: a pinned skill whose content cannot be hashed
				// (an unreadable file is all it takes) must surface as pin
				// drift, not stay quietly "pinned" — the gate this store
				// exists to provide would otherwise be disabled by a
				// dangling symlink. An unpinned skill is skipped: there is
				// nothing trustworthy to pin.
				pin := ps.data.Skills[sk.Name]
				if pin == nil {
					slog.Warn("skillpins: hashing unpinned skill failed, not pinning", "skill", sk.Name, "error", err)
					continue
				}
				result.Drifted = append(result.Drifted, sk.Name)
				pin.LastVerifiedAt = now
				if pin.Status != StatusDrift {
					slog.Warn("skillpins: hashing pinned skill failed; marking pin drift", "skill", sk.Name, "error", err)
					pin.Status = StatusDrift
					dirty = true
				}
				continue
			}

			pin := ps.data.Skills[sk.Name]
			if pin == nil {
				ps.pinSkill(src, sk, skillHash, files, now, "")
				result.Pinned = append(result.Pinned, sk.Name)
				dirty = true
				continue
			}

			drifted := skillHash != pin.SkillHash || !equalFileDigests(pin.Files, files)
			status := StatusPinned
			if drifted {
				status = StatusDrift
				result.Drifted = append(result.Drifted, sk.Name)
			}
			// LastVerifiedAt is informational; persisting it on every pass
			// would rewrite the file on each registry refresh, so only a
			// status transition makes the pass dirty.
			pin.LastVerifiedAt = now
			if pin.Status != status {
				if status == StatusDrift {
					slog.Warn("skillpins: pin drift detected", "skill", sk.Name,
						"hint", "review with 'gridctl skill pins diff' or the Pins workspace")
				}
				pin.Status = status
				dirty = true
			}
		}

		for name := range ps.data.Skills {
			if !seen[name] {
				result.Missing = append(result.Missing, name)
			}
		}

		if !dirty {
			return nil
		}
		return ps.saveLocked()
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(result.Pinned)
	sort.Strings(result.Drifted)
	sort.Strings(result.Missing)
	if len(result.Pinned) > 0 {
		slog.Info("skillpins: pinned skills", "count", len(result.Pinned))
	}
	return result, nil
}

// Verify builds the diff for one skill against its pin without writing
// anything. Returns ErrNotPinned when the skill has no pin yet and
// registry.ErrNotFound (wrapped by src) when the skill does not exist.
func (ps *Store) Verify(name string, src SkillSource) (*VerifyResult, error) {
	sk, err := src.GetSkill(name)
	if err != nil {
		return nil, err
	}
	skillHash, files, err := ComputeDigests(src, sk)
	if err != nil {
		return nil, err
	}

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	pin := ps.data.Skills[name]
	if pin == nil {
		return nil, fmt.Errorf("skillpins: skill %q: %w", name, ErrNotPinned)
	}

	// The composite hash is computed from the SAME digests the diff below
	// describes, so an approval bound to it can never validate content the
	// reviewer did not see (content changing between two separate reads).
	result := &VerifyResult{SkillName: name, Status: StatusPinned, CompositeHash: CompositeHash(skillHash, files)}
	added, removed, modified := diffFileSets(pin.Files, files)
	if skillHash != pin.SkillHash || len(added)+len(removed)+len(modified) > 0 {
		result.Status = StatusDrift
		result.Diff = &SkillDiff{
			Name:          name,
			OldSkillHash:  pin.SkillHash,
			NewSkillHash:  skillHash,
			OldDocument:   pin.Document,
			AddedFiles:    emptyIfNil(added),
			RemovedFiles:  emptyIfNil(removed),
			ModifiedFiles: emptyIfNil(modified),
			Findings:      ps.scanFindings(sk),
		}
		if result.Diff.Findings == nil {
			result.Diff.Findings = []pins.Finding{}
		}
		if doc, err := registry.RenderSkillMD(sk); err == nil {
			result.Diff.NewDocument = string(doc)
		}
	}
	return result, nil
}

// emptyIfNil normalizes a nil slice to an empty one so diff consumers can
// marshal without per-field guards.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// CurrentCompositeHash computes the approval fingerprint for a skill's
// current content, the value a reviewed diff carries and Approve checks.
func (ps *Store) CurrentCompositeHash(name string, src SkillSource) (string, error) {
	sk, err := src.GetSkill(name)
	if err != nil {
		return "", err
	}
	skillHash, files, err := ComputeDigests(src, sk)
	if err != nil {
		return "", err
	}
	return CompositeHash(skillHash, files), nil
}

// Approve re-pins a skill's current content, clearing pin drift.
// expectedHash, when non-empty, must match the current composite hash or
// ErrHashMismatch is returned — binding the approval to the reviewed
// content. When the current content carries unresolved advisory findings,
// a non-empty reason is required (ErrReasonRequired otherwise) and is
// persisted on the record.
func (ps *Store) Approve(name string, src SkillSource, expectedHash, reason string) error {
	sk, err := src.GetSkill(name)
	if err != nil {
		return err
	}
	skillHash, files, err := ComputeDigests(src, sk)
	if err != nil {
		return err
	}
	if expectedHash != "" && expectedHash != CompositeHash(skillHash, files) {
		return fmt.Errorf("skillpins: skill %q: %w", name, ErrHashMismatch)
	}

	return ps.withFileLock(func() error {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		if findings := ps.scanFindings(sk); len(findings) > 0 && reason == "" {
			return fmt.Errorf("skillpins: skill %q has %d advisory finding(s): %w", name, len(findings), ErrReasonRequired)
		}
		ps.pinSkill(src, sk, skillHash, files, time.Now().UTC(), reason)
		return ps.saveLocked()
	})
}

// Reset deletes the pin record for a skill. The next Sync re-pins it fresh.
func (ps *Store) Reset(name string) error {
	return ps.withFileLock(func() error {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		delete(ps.data.Skills, name)
		return ps.saveLocked()
	})
}

// pinSkill records a fresh pin for a skill. Caller must hold ps.mu.Lock().
func (ps *Store) pinSkill(src SkillSource, sk *registry.AgentSkill, skillHash string, files []FileDigest, now time.Time, reason string) {
	// Preserve the original PinnedAt across re-pins (the approve flow).
	pinnedAt := now
	if existing := ps.data.Skills[sk.Name]; existing != nil {
		pinnedAt = existing.PinnedAt
	}

	source, origin := provenance(src, sk.Name)
	pin := &SkillPin{
		SkillHash:      skillHash,
		Files:          files,
		Source:         source,
		Origin:         origin,
		ApprovedReason: reason,
		PinnedAt:       pinnedAt,
		LastVerifiedAt: now,
		Status:         StatusPinned,
		Findings:       ps.scanFindings(sk),
	}
	if doc, err := registry.RenderSkillMD(sk); err == nil {
		pin.Document = string(doc)
	}
	ps.data.Skills[sk.Name] = pin
}

// provenance reads a skill's .origin.json sidecar, classifying the skill as
// git-imported when one exists and locally authored otherwise. The sidecar
// is decoded with a local struct rather than pkg/skills.ReadOrigin so the
// store's registry access stays behind the SkillSource seam.
func provenance(src SkillSource, name string) (string, *OriginRef) {
	data, err := src.ReadFile(name, ".origin.json")
	if err != nil {
		return SourceLocal, nil
	}
	var origin struct {
		Repo      string `json:"repo"`
		Ref       string `json:"ref"`
		CommitSHA string `json:"commitSha"`
	}
	if err := json.Unmarshal(data, &origin); err != nil {
		return SourceGit, nil
	}
	return SourceGit, &OriginRef{Repo: origin.Repo, Ref: origin.Ref, CommitSHA: origin.CommitSHA}
}

func equalFileDigests(a, b []FileDigest) bool {
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

// withFileLock runs fn under a file-level lock to serialize writes across
// processes. The lock name is prefixed distinctly from tool pins so the two
// stores never serialize against each other.
func (ps *Store) withFileLock(fn func() error) error {
	return state.WithLock("skillpins-"+ps.stackName, lockTimeout, fn)
}

// saveLocked writes the pin file atomically. Caller must hold ps.mu.Lock().
func (ps *Store) saveLocked() error {
	if err := ps.ensurePath(); err != nil {
		return err
	}
	// The directory derives from ps.path, not state.PinsDir(): a store built
	// via NewWithPath must never create the real pins directory.
	if err := os.MkdirAll(filepath.Dir(ps.path), 0755); err != nil {
		return fmt.Errorf("skillpins: creating pins directory: %w", err)
	}

	ps.data.Version = fileVersion

	data, err := json.MarshalIndent(ps.data, "", "  ")
	if err != nil {
		return fmt.Errorf("skillpins: marshaling pin file: %w", err)
	}

	tmp := ps.path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("skillpins: writing temp file: %w", err)
	}
	if err := os.Rename(tmp, ps.path); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			slog.Warn("skillpins: failed to remove temp file after rename error", "path", tmp, "error", removeErr)
		}
		return fmt.Errorf("skillpins: renaming temp file: %w", err)
	}
	return nil
}

// emptyPinFile returns a fresh PinFile for this stack.
func (ps *Store) emptyPinFile() *PinFile {
	return &PinFile{
		Version:   fileVersion,
		Stack:     ps.stackName,
		CreatedAt: time.Now().UTC(),
		Skills:    make(map[string]*SkillPin),
	}
}
