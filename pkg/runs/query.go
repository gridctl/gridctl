package runs

import "time"

// Filter selects persisted records. Empty fields are unconstrained.
type Filter struct {
	Since             time.Time
	Until             time.Time
	RequestedName     string
	ResolvedServer    string
	ResolvedTool      string
	Disposition       string
	ClientLabel       string
	AccessLabel       string
	AttemptID         string
	ParentAttemptID   string
	RootAttemptID     string
	PreviousAttemptID string
	TraceID           string
}

// Cursor identifies a pagination position. WipeEpoch must match the
// current writer generation or the cursor is invalid.
type Cursor struct {
	WipeEpoch  uint64    `json:"wipe_epoch"`
	ReturnedAt time.Time `json:"returned_at"`
	Sequence   uint64    `json:"sequence"`
	AttemptID  string    `json:"attempt_id"`
}

const (
	DefaultQueryLimit = 100
	MaxQueryLimit     = 1000
	MaxLineBytes      = 16 << 10
)

// QueryResult is a bounded page of records plus explicit warnings.
type QueryResult struct {
	Records    []Record  `json:"records"`
	Warnings   []Warning `json:"warnings"`
	NextCursor *Cursor   `json:"next_cursor,omitempty"`
	Partial    bool      `json:"partial"`
	WipeEpoch  uint64    `json:"wipe_epoch"`
}

func (f Filter) match(rec Record) bool {
	if !f.Since.IsZero() && rec.ReturnedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && rec.ReturnedAt.After(f.Until) {
		return false
	}
	if f.RequestedName != "" && rec.RequestedName != f.RequestedName {
		return false
	}
	if f.ResolvedServer != "" && rec.ResolvedServer != f.ResolvedServer {
		return false
	}
	if f.ResolvedTool != "" && rec.ResolvedTool != f.ResolvedTool {
		return false
	}
	if f.Disposition != "" && rec.Disposition != f.Disposition {
		return false
	}
	if f.ClientLabel != "" && rec.ClientLabel != f.ClientLabel {
		return false
	}
	if f.AccessLabel != "" && rec.AccessLabel != f.AccessLabel {
		return false
	}
	if f.AttemptID != "" && rec.AttemptID != f.AttemptID {
		return false
	}
	if f.ParentAttemptID != "" && rec.ParentAttemptID != f.ParentAttemptID {
		return false
	}
	if f.RootAttemptID != "" && rec.RootAttemptID != f.RootAttemptID {
		return false
	}
	if f.PreviousAttemptID != "" && rec.PreviousAttemptID != f.PreviousAttemptID {
		return false
	}
	if f.TraceID != "" && rec.TraceID != f.TraceID {
		return false
	}
	return true
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return DefaultQueryLimit
	}
	if limit > MaxQueryLimit {
		return MaxQueryLimit
	}
	return limit
}
