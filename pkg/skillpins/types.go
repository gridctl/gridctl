// Package skillpins implements TOFU content pins for registry skill
// documents, the document-scale sibling of pkg/pins' tool-schema pins.
// A pin records per-file SHA-256 digests over a skill's whole document set
// (canonicalized SKILL.md plus supporting files); any later mismatch is
// "pin drift" that persists until a human approves (re-pins) or resets.
// The deterministic hash is the gate; poisoning findings attached to a pin
// are advisory decoration and never block anything.
//
// "Pin drift" is deliberately distinct from pkg/skills sync drift (local
// edits vs the last git import): the two are different facts about the same
// skill and can both be true at once.
package skillpins

import (
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
)

// Status values for SkillPin, matching pkg/pins' vocabulary.
const (
	StatusPinned = "pinned"
	StatusDrift  = "drift"
)

// Source discriminants for SkillPin.Source. The schema deliberately leaves
// room for future sources (e.g. "upstream" for gateway-ingested skills):
// unknown values load fine and pass through untouched.
const (
	SourceLocal = "local"
	SourceGit   = "git"
)

// PinFile is the top-level JSON structure stored at
// ~/.gridctl/pins/{stackName}.skills.json.
type PinFile struct {
	Version   string               `json:"version"`
	Stack     string               `json:"stack"`
	CreatedAt time.Time            `json:"created_at"`
	Skills    map[string]*SkillPin `json:"skills"`
}

// FileDigest is one supporting file's content digest, path relative to the
// skill directory.
type FileDigest struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// OriginRef is the factual provenance of a git-imported skill, copied from
// its .origin.json sidecar at pin time. Display-only: origin answers "where
// did this come from", never "is this safe". The commitSha tag deliberately
// mirrors the sidecar's camelCase key (pkg/skills.Origin) rather than this
// file's snake_case convention, so the two records stay field-identical.
type OriginRef struct {
	Repo      string `json:"repo,omitempty"`
	Ref       string `json:"ref,omitempty"`
	CommitSHA string `json:"commitSha,omitempty"`
}

// SkillPin holds the pin state for a single skill. Document, Findings,
// Source, and Origin are derived data in the pkg/pins sense: an older
// gridctl rewriting the file drops them without a file-version bump, and
// only the digests are load-bearing for drift.
type SkillPin struct {
	// SkillHash is the digest of the canonical SKILL.md rendering.
	SkillHash string `json:"skill_hash"`
	// Files are the supporting-file digests, sorted by path.
	Files []FileDigest `json:"files,omitempty"`
	// Document is the canonical SKILL.md as pinned, kept so drift review can
	// show a prose diff (the pkg/pins Description/schema-capture precedent).
	Document string `json:"document,omitempty"`
	// Source and Origin are provenance at pin time: "local" or "git".
	Source string     `json:"source,omitempty"`
	Origin *OriginRef `json:"origin,omitempty"`
	// ApprovedReason records the human justification when a pin carrying
	// unresolved findings was approved. Empty for finding-free approvals.
	ApprovedReason string `json:"approved_reason,omitempty"`

	PinnedAt       time.Time `json:"pinned_at"`
	LastVerifiedAt time.Time `json:"last_verified_at"`
	Status         string    `json:"status"`
	// Findings are advisory poisoning-scan results for the pinned content.
	Findings []pins.Finding `json:"findings,omitempty"`
}

// VerifyResult is the outcome of verifying one skill against its pin.
// CompositeHash is the approval-binding fingerprint of the verified content,
// computed from the same digests the Diff describes.
type VerifyResult struct {
	SkillName     string
	Status        string // StatusPinned (first pin or clean) | StatusDrift
	CompositeHash string
	Diff          *SkillDiff
}

// SkillDiff describes how a skill's document set moved since its pin.
// Findings are advisory results for the NEW content, computed at verify
// time so the reviewer sees them beside the diff they annotate.
type SkillDiff struct {
	Name          string         `json:"name"`
	OldSkillHash  string         `json:"old_skill_hash"`
	NewSkillHash  string         `json:"new_skill_hash"`
	OldDocument   string         `json:"old_document,omitempty"`
	NewDocument   string         `json:"new_document,omitempty"`
	AddedFiles    []string       `json:"added_files,omitempty"`
	RemovedFiles  []string       `json:"removed_files,omitempty"`
	ModifiedFiles []string       `json:"modified_files,omitempty"`
	Findings      []pins.Finding `json:"findings,omitempty"`
}

// DocumentChanged reports whether the canonical SKILL.md moved.
func (d *SkillDiff) DocumentChanged() bool {
	return d.OldSkillHash != d.NewSkillHash
}

// FilesChanged reports whether any supporting file was added, removed, or
// modified.
func (d *SkillDiff) FilesChanged() bool {
	return len(d.AddedFiles)+len(d.RemovedFiles)+len(d.ModifiedFiles) > 0
}

// SyncResult summarizes one TOFU/verify pass over the whole registry.
type SyncResult struct {
	// Pinned lists skills pinned for the first time this pass.
	Pinned []string
	// Drifted lists skills whose content no longer matches their pin.
	Drifted []string
	// Missing lists pinned skills absent from the registry. Their records
	// are kept (reset guidance surfaces in the CLI), never auto-pruned.
	Missing []string
}
