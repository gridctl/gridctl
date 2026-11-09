// Package proposals stores staged changes to governed documents: a skill,
// a context fragment (or AGENTS.md when fragments mode is off), or a hook.
// A proposal carries a unified diff against the content its author read,
// the composite hash of that content, a classification, prevalence
// counts, opaque evidence references, and a reason. Nothing reaches a
// governed document until a human approves the proposal.
//
// Approve is compare-and-swap on the base hash. On the apply path (a
// locally authored skill or local fragment whose pin shows no drift) the
// diff is applied, the target is re-pinned through its own pin store, and
// that store appends the approvals log entry. On the record path (any
// imported target, and every hook) only the verdict is written, to the
// proposal file: no target bytes, no pin store call, and no log entry.
//
// The store is stateless between calls, like pkg/ctxpins: every public
// method reloads under the proposals file lock. It never fetches
// evidence, calls a model, or reads transcripts. Proposals are untrusted
// input until approved; findings from the poisoning scanner are advisory
// and gate only by requiring a reason.
package proposals

import (
	"errors"
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
)

// FileVersion is the proposal file version this binary writes and the
// highest version it accepts. Advertised by `gridctl info --json` as
// schemas.proposals.
const FileVersion = "1"

// Target kinds. KindAgent is a recognized token that Create refuses:
// agents have no pin store yet.
const (
	KindSkill   = "skill"
	KindContext = "context"
	KindHook    = "hook"
	KindAgent   = "agent"
)

// Classifications.
const (
	ClassKnowledge = "knowledge"
	ClassGuarantee = "guarantee"
)

// Proposal status values.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Approve paths. PathApply writes the diff and re-pins; PathRecord writes
// the verdict on the proposal only.
const (
	PathApply  = "apply"
	PathRecord = "record"
)

// Provenance kinds.
const (
	ProvenanceLocal       = "local"
	ProvenanceLocalImport = "local-import"
	ProvenanceGit         = "git"
	ProvenancePack        = "pack"
)

// Pin status values computed for show and the REST get. They are never
// stored.
const (
	PinStatusPinned   = "pinned"
	PinStatusDrift    = "drift"
	PinStatusUnpinned = "unpinned"
	PinStatusUnknown  = "unknown"
)

// Bounds.
const (
	// MaxDiffBytes caps the stored unified diff.
	MaxDiffBytes = 256 << 10
	// MaxSourceBytes caps the free-form source label.
	MaxSourceBytes = 256
	// MaxListFiles caps how many proposal files List reads.
	MaxListFiles = 10000
	// MaxEvidence caps a proposal's evidence references, including any an
	// approver adds. The approvals log allows 16; one slot is held for
	// the synthetic proposal:<id> reference an apply-path approve adds.
	MaxEvidence = 15
)

// Sentinel errors. Callers map them with errors.Is; the wrapped text says
// what was refused and how to retry.
var (
	ErrCorrupt         = errors.New("corrupt proposal file")
	ErrNewerVersion    = errors.New("proposal file written by a newer gridctl")
	ErrNotFound        = errors.New("proposal not found")
	ErrNotPending      = errors.New("proposal is not pending")
	ErrBaseStale       = errors.New("target changed since the proposal's base")
	ErrTargetDrift     = errors.New("target has unreviewed pin drift")
	ErrReasonRequired  = errors.New("approving a proposal with advisory findings requires a reason")
	ErrKindUnsupported = errors.New("proposal kind is not supported")
	ErrInvalidInput    = errors.New("invalid proposal")
	ErrDiffUnsupported = errors.New("unsupported diff")
	ErrTargetNotFound  = errors.New("proposal target not found")
	ErrApplyFailed     = errors.New("proposal did not apply")
	// ErrPinFailed marks an apply-path approve whose writes landed but
	// whose re-pin failed. The proposal is still marked approved with
	// verdict.pin_error set.
	ErrPinFailed = errors.New("proposal applied but re-pinning failed")
	// ErrStoreUnavailable means a store the target needs (the registry or
	// a pin store) was not supplied.
	ErrStoreUnavailable = errors.New("store unavailable")
)

// Prevalence counts how often the proposer saw the pattern behind the
// change. Both values are non-negative.
type Prevalence struct {
	Sessions int `json:"sessions"`
	Runs     int `json:"runs"`
}

// Provenance says where the target came from, which decides the path.
// Pack is set for ProvenancePack when the pack name is known. SourceDir
// is set for ProvenanceLocalImport.
type Provenance struct {
	Kind      string `json:"kind"`
	Pack      string `json:"pack,omitempty"`
	SourceDir string `json:"source_dir,omitempty"`
}

// Verdict records an approve or reject. ApprovedHash is the base hash the
// verdict bound to. NewHash is the post-apply composite hash on the apply
// path. Stack is the skill pin stack an apply-path skill approval used.
// PinError is set when the target was written but re-pinning failed.
type Verdict struct {
	At           time.Time `json:"at"`
	By           string    `json:"by,omitempty"`
	BySource     string    `json:"by_source,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Evidence     []string  `json:"evidence,omitempty"`
	Applied      bool      `json:"applied"`
	ApprovedHash string    `json:"approved_hash,omitempty"`
	NewHash      string    `json:"new_hash,omitempty"`
	Stack        string    `json:"stack,omitempty"`
	PinError     string    `json:"pin_error,omitempty"`
}

// Proposal is one staged change, stored at
// ~/.gridctl/proposals/<id>.json. PinStatus is computed for display and
// is never written to the file.
type Proposal struct {
	ID              string         `json:"id"`
	Version         string         `json:"version"`
	Kind            string         `json:"kind"`
	Target          string         `json:"target"`
	BaseHash        string         `json:"base_hash"`
	BaseCurrent     bool           `json:"base_current"`
	Diff            string         `json:"diff"`
	Classification  string         `json:"classification"`
	Prevalence      Prevalence     `json:"prevalence"`
	Evidence        []string       `json:"evidence"`
	Reason          string         `json:"reason,omitempty"`
	Source          string         `json:"source,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	CreatedBy       string         `json:"created_by,omitempty"`
	CreatedBySource string         `json:"created_by_source,omitempty"`
	Status          string         `json:"status"`
	Path            string         `json:"path"`
	Provenance      Provenance     `json:"provenance"`
	Findings        []pins.Finding `json:"findings"`
	Verdict         *Verdict       `json:"verdict"`
	Files           []string       `json:"files"`
	PinStatus       string         `json:"pin_status,omitempty"`
}

// CreateRequest is the JSON create document plus caller attribution.
// Principal is a verified identity (an OAuth subject) and wins over the
// declared CreatedBy; it is never read from the document.
type CreateRequest struct {
	Kind           string     `json:"kind"`
	Target         string     `json:"target"`
	BaseHash       string     `json:"base_hash"`
	Diff           string     `json:"diff"`
	Classification string     `json:"classification"`
	Prevalence     Prevalence `json:"prevalence"`
	Evidence       []string   `json:"evidence"`
	Reason         string     `json:"reason"`
	Source         string     `json:"source"`
	CreatedBy      string     `json:"created_by"`
	Principal      string     `json:"-"`
}

// ApproveRequest is one approval. Reason, when non-empty, replaces the
// proposal's reason. Evidence is appended to the proposal's evidence.
// ApprovedBy and ApprovedBySource are already resolved attribution.
type ApproveRequest struct {
	Reason           string
	Evidence         []string
	ApprovedBy       string
	ApprovedBySource string
}

// RejectRequest is one rejection. Reason is required.
type RejectRequest struct {
	Reason           string
	ApprovedBy       string
	ApprovedBySource string
}

// Filter narrows List. Empty Status or "all" matches every status; empty
// Kind matches every kind.
type Filter struct {
	Status string
	Kind   string
}

// Skipped is a proposal file List could not load. Category is one of
// corrupt, newer_version, permission, or unreadable. Contents are never
// reported.
type Skipped struct {
	Path     string `json:"path"`
	Category string `json:"category"`
}

// ListResult is the outcome of List. Proposals are sorted pending first,
// then newest first. Truncated is true when the directory held more than
// MaxListFiles proposal files.
type ListResult struct {
	Proposals []*Proposal
	Skipped   []Skipped
	Truncated bool
}
