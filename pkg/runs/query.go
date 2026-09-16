package runs

import "time"

// Filter selects persisted records. Empty fields are unconstrained.
type Filter struct {
	Since             time.Time `json:"since,omitempty"`
	Until             time.Time `json:"until,omitempty"`
	RequestedName     string    `json:"requested,omitempty"`
	ResolvedServer    string    `json:"server,omitempty"`
	ResolvedTool      string    `json:"tool,omitempty"`
	Disposition       string    `json:"disposition,omitempty"`
	ClientLabel       string    `json:"client,omitempty"`
	AccessLabel       string    `json:"access,omitempty"`
	AttemptID         string    `json:"attempt,omitempty"`
	ParentAttemptID   string    `json:"parent,omitempty"`
	RootAttemptID     string    `json:"root,omitempty"`
	PreviousAttemptID string    `json:"previous,omitempty"`
	TraceID           string    `json:"trace,omitempty"`
}

// Cursor identifies a pagination position. WipeEpoch and SourceToken must
// match the current writer generation and on-disk file set or the cursor
// is invalid.
type Cursor struct {
	WipeEpoch   uint64    `json:"wipe_epoch"`
	SourceToken string    `json:"source_token,omitempty"`
	ReturnedAt  time.Time `json:"returned_at"`
	Sequence    uint64    `json:"sequence"`
	AttemptID   string    `json:"attempt_id"`
}

// QuerySource names the stack or path a query read.
type QuerySource struct {
	Kind  string `json:"kind"`
	Stack string `json:"stack,omitempty"`
	Path  string `json:"path,omitempty"`
}

// QueryEnvelope is the shared live/offline/export query contract.
type QueryEnvelope struct {
	SchemaVersion int         `json:"schema_version"`
	Source        QuerySource `json:"source"`
	Filters       Filter      `json:"filters"`
	Records       []Record    `json:"records"`
	Warnings      []Warning   `json:"warnings"`
	NextCursor    *Cursor     `json:"next_cursor,omitempty"`
	Partial       bool        `json:"partial"`
	WipeEpoch     uint64      `json:"wipe_epoch"`
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

// EnvelopeFromResult builds the shared query/export envelope.
func EnvelopeFromResult(res QueryResult, source QuerySource, filter Filter) QueryEnvelope {
	warnings := res.Warnings
	if warnings == nil {
		warnings = []Warning{}
	}
	records := res.Records
	if records == nil {
		records = []Record{}
	}
	return QueryEnvelope{
		SchemaVersion: SchemaVersion,
		Source:        source,
		Filters:       filter,
		Records:       records,
		Warnings:      warnings,
		NextCursor:    res.NextCursor,
		Partial:       res.Partial,
		WipeEpoch:     res.WipeEpoch,
	}
}
