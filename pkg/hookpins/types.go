// Package hookpins records TOFU content pins for imported hooks at
// ~/.gridctl/pins/hooks.json. A pin is written by the import that installs
// the hook. A later byte change made any other way is pin drift until a
// human approves it. gridctl never executes a hook.
package hookpins

import (
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

const (
	// FileVersion is the hook pin file version this binary writes and the
	// highest version it accepts.
	FileVersion = "1"

	StatusPinned = "pinned"
	StatusDrift  = "drift"

	SourceLocal = "local"
	SourceGit   = "git"
)

// PinFile is the top-level JSON structure stored at
// ~/.gridctl/pins/hooks.json. Hook pins are not stack-scoped.
type PinFile struct {
	Version string              `json:"version"`
	Hooks   map[string]*HookPin `json:"hooks"`
}

// HookPin is the pin record for one hook. Approvals is rebuilt from the
// approvals log on every load. CompositeHash is recomputed from the digests.
type HookPin struct {
	// DefinitionHash is the SHA-256 of hook.json bytes.
	DefinitionHash string `json:"definition_hash"`
	// Files are per-file digests over every installed file except
	// .origin.json, dot-prefixed entries, .tmp files, symlinks, and
	// irregular files. Sorted by path.
	Files []skillpins.FileDigest `json:"files,omitempty"`
	// Document is the hook.json text as pinned, kept so drift review can
	// show a diff.
	Document       string                `json:"document,omitempty"`
	Source         string                `json:"source,omitempty"`
	Origin         *skillpins.OriginRef  `json:"origin,omitempty"`
	ApprovedReason string                `json:"approved_reason,omitempty"`
	PinnedAt       time.Time             `json:"pinned_at"`
	LastVerifiedAt time.Time             `json:"last_verified_at"`
	Status         string                `json:"status"`
	Findings       []pins.Finding        `json:"findings,omitempty"`
	Approvals      []pinapprovals.Entry  `json:"approvals,omitempty"`
	CompositeHash  string                `json:"composite_hash,omitempty"`
}

// VerifyResult is the outcome of verifying one hook against its pin.
type VerifyResult struct {
	Name          string
	Status        string
	CompositeHash string
	Diff          *HookDiff
}

// HookDiff describes how a hook moved since its pin.
type HookDiff struct {
	Name           string         `json:"name"`
	OldHash        string         `json:"old_definition_hash"`
	NewHash        string         `json:"new_definition_hash"`
	OldDocument    string         `json:"old_document,omitempty"`
	NewDocument    string         `json:"new_document,omitempty"`
	AddedFiles     []string       `json:"added_files,omitempty"`
	RemovedFiles   []string       `json:"removed_files,omitempty"`
	ModifiedFiles  []string       `json:"modified_files,omitempty"`
	Findings       []pins.Finding `json:"findings,omitempty"`
}

// DocumentChanged reports whether hook.json moved.
func (d *HookDiff) DocumentChanged() bool {
	return d != nil && d.OldHash != d.NewHash
}

// SyncResult summarizes one verify pass over pinned hooks. Sync does not
// re-pin changed bytes; that happens only on an accepted import or approve.
type SyncResult struct {
	Drifted []string
	Missing []string
}

// ApproveRequest is one explicit hook approval.
type ApproveRequest struct {
	ExpectedHash     string
	Reason           string
	Evidence         []string
	ApprovedBy       string
	ApprovedBySource string
	// Findings are advisory results for the content being approved. A
	// non-empty list requires Reason.
	Findings []pins.Finding
}

// PinRequest is the content an accepted import pins.
type PinRequest struct {
	Name     string
	Dir      string
	Findings []pins.Finding
	Origin   *skillpins.OriginRef
	Source   string
}
