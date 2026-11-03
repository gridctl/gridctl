// Package pinapprovals records explicit pin approvals in a versioned log
// beside the pin stores. The log is audit data, not recomputable derived
// data, so it lives under pins/approvals/ rather than inside the pin file.
// An older gridctl rewriting a pin file drops the projected approvals list
// and loses nothing: the next load rebuilds it from this log.
//
// approved_by is best-effort local attribution, not an identity system.
// Evidence references are opaque operator text. They are stored as given,
// never fetched, resolved, or opened.
package pinapprovals

import "time"

const (
	// FileVersion is the approvals log version this binary writes and the
	// highest version it accepts. Advertised by `gridctl info --json` as
	// schemas.pin_approvals.
	FileVersion = "1"

	// MaxHistory is the number of approval entries kept per subject, newest
	// first. Older entries are dropped on append.
	MaxHistory = 10

	// KindSkill, KindTool, and KindContext are the log file kind discriminants.
	KindSkill   = "skill"
	KindTool    = "tool"
	KindContext = "context"

	// Attribution sources. os_user is the local OS account, declared is a
	// name the caller supplied and this process did not verify, and oauth
	// is a verified access principal. None of these authenticate the
	// approval.
	SourceOSUser   = "os_user"
	SourceDeclared = "declared"
	SourceOAuth    = "oauth"

	maxReferenceBytes = 2048
	maxReasonBytes    = 4096
	maxReferences     = 16
	// maxApproverBytes caps declared and resolved approver names. The field
	// is attribution text stored in the approvals log, not an identity claim.
	maxApproverBytes = 256
)

// Entry is one explicit approval. ApprovedAt is RFC 3339 UTC. ApprovedHash
// is the hash the approval bound to. Bound is true when the caller supplied
// an expected hash. Evidence order is the order the caller supplied after
// trim and de-duplication.
type Entry struct {
	ApprovedAt       time.Time `json:"approved_at"`
	ApprovedBy       string    `json:"approved_by,omitempty"`
	ApprovedBySource string    `json:"approved_by_source,omitempty"`
	ApprovedHash     string    `json:"approved_hash"`
	Bound            bool      `json:"bound"`
	Reason           string    `json:"reason,omitempty"`
	Evidence         []string  `json:"evidence,omitempty"`
}

// CloneEntries returns a deep copy. A nil or empty input returns nil so
// JSON omitempty drops the field.
func CloneEntries(in []Entry) []Entry {
	if len(in) == 0 {
		return nil
	}
	out := make([]Entry, len(in))
	for i := range in {
		out[i] = cloneEntry(in[i])
	}
	return out
}

func cloneEntry(e Entry) Entry {
	if e.Evidence != nil {
		e.Evidence = append([]string(nil), e.Evidence...)
	}
	return e
}
