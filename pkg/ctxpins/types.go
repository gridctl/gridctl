// Package ctxpins implements TOFU content pins for global context: one
// record per rule fragment, or the single canonical AGENTS.md when
// fragments mode is off. A pin records a content digest; any later
// mismatch is "pin drift" that persists until a human approves (re-pins)
// or resets. The deterministic hash is the gate; poisoning findings
// attached to a pin are advisory decoration and never block projection.
//
// The store is the stackless sibling of pkg/skillpins. It borrows that
// package's sentinel errors, lock, atomic save, and re-pin conventions,
// and reuses pkg/pins scanner types. It does not keep a Load-once copy
// of the pin file: every public method reloads under the file lock, so a
// CLI write is visible to the API server on its next call. Only hash and
// status are load-bearing. Document, source, pack, approved reason,
// findings, and the projected approvals list are derived; an older
// gridctl rewriting the file drops them without a file-version bump.
//
// Hashes use the same canonical normalization as the projection lockfile
// (CRLF to LF, trailing newlines trimmed) with a c1: prefix. The lockfile
// stores sha256:-prefixed digests, so parity is the unprefixed digest.
// Pin drift is a fact about the source, distinct from client-side stale
// and drifted projection state.
package ctxpins

import (
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
)

// FileVersion is the context pin file version this binary writes and the
// highest version it accepts. Advertised by `gridctl info --json` as
// schemas.context_pins.
const FileVersion = "1"

// CanonicalName is the reserved record key for the single-file store.
const CanonicalName = "AGENTS.md"

// Status values for FragmentPin, matching pkg/pins' vocabulary.
const (
	StatusPinned = "pinned"
	StatusDrift  = "drift"
)

// Source discriminants for FragmentPin.Source. Unknown values load fine
// and pass through untouched.
const (
	SourceLocal = "local"
	SourcePack  = "pack"
)

// PinFile is the top-level JSON structure stored at
// ~/.gridctl/pins/context/global.json.
type PinFile struct {
	Version   string                  `json:"version"`
	Fragments map[string]*FragmentPin `json:"fragments"`
}

// FragmentPin holds the pin state for one fragment, or for AGENTS.md in
// single-file mode. Document, Source, Pack, ApprovedReason, Findings,
// and Approvals are derived. Approvals is rebuilt from the approvals log
// on every load. Only Hash and Status decide drift.
type FragmentPin struct {
	Hash           string               `json:"hash"`
	Document       string               `json:"document,omitempty"`
	Source         string               `json:"source,omitempty"`
	Pack           string               `json:"pack,omitempty"`
	ApprovedReason string               `json:"approved_reason,omitempty"`
	PinnedAt       time.Time            `json:"pinned_at"`
	LastVerifiedAt time.Time            `json:"last_verified_at"`
	Status         string               `json:"status"`
	Findings       []pins.Finding       `json:"findings,omitempty"`
	Approvals      []pinapprovals.Entry `json:"approvals,omitempty"`
}

// VerifyResult is the outcome of verifying one fragment against its pin.
// CompositeHash is the approval-binding fingerprint of the current content.
type VerifyResult struct {
	Name          string
	Status        string
	CompositeHash string
	Diff          *Diff
}

// Diff describes how a fragment moved since its pin. Findings are advisory
// results for the new content, computed at verify time. Findings is never
// nil when Diff itself is non-nil.
type Diff struct {
	Name        string         `json:"name"`
	OldHash     string         `json:"old_hash"`
	NewHash     string         `json:"new_hash"`
	OldDocument string         `json:"old_document,omitempty"`
	NewDocument string         `json:"new_document,omitempty"`
	Findings    []pins.Finding `json:"findings"`
}

// DocumentChanged reports whether the pinned digest moved.
func (d *Diff) DocumentChanged() bool {
	if d == nil {
		return false
	}
	return d.OldHash != d.NewHash
}

// SyncResult summarizes one TOFU/verify pass. Slices are sorted.
type SyncResult struct {
	Pinned  []string
	Drifted []string
	Missing []string
}

// Summary is the read-only pin picture computed from stored records and
// the current documents. Arrays are non-nil.
type Summary struct {
	Drifted  []string
	Missing  []string
	Unpinned []string
}

// ApproveRequest is one explicit fragment approval. ExpectedHash, when
// non-empty, must match the current composite hash. Evidence is stored as
// returned by pinapprovals.ValidateReferences, not as the caller supplied it.
type ApproveRequest struct {
	ExpectedHash     string
	Reason           string
	Evidence         []string
	ApprovedBy       string
	ApprovedBySource string
}
