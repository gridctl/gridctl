package runs

import (
	"time"
)

// SchemaVersion is the current persisted run-record schema.
const SchemaVersion = 1

// Fixed omission indicators. These are the only values written when an
// identifier cannot be stored; never excerpts of the rejected value.
const (
	OmittedMalformed = "malformed"
	OmittedOversized = "oversized"
)

// Disposition is the final outcome of one returning dispatch attempt.
const (
	DispositionCompleted      = "completed"
	DispositionToolError      = "tool_error"
	DispositionDenied         = "denied"
	DispositionRoutingFailed  = "routing_failed"
	DispositionTransportError = "transport_error"
	DispositionCancelled      = "cancelled"
	DispositionTimeout        = "timeout"
	DispositionInputRequired  = "input_required"
	DispositionRetryRejected  = "retry_rejected"
)

// Stage names the decision point that produced the disposition.
const (
	StageGroup      = "group"
	StageScope      = "scope"
	StageGate       = "gate"
	StagePin        = "pin"
	StageRouting    = "routing"
	StageColdStart  = "cold_start"
	StageMRTR       = "mrtr"
	StageDownstream = "downstream"
	StageCodeMode   = "code_mode"
)

// Reason codes set at the actual decision point. They are not parsed from
// error strings or inferred from a nil Go error.
const (
	ReasonOK               = "ok"
	ReasonGroupMembership  = "group_membership"
	ReasonClientScope      = "client_scope"
	ReasonGateDenied       = "gate_denied"
	ReasonSchemaPin        = "schema_pin"
	ReasonUnknownTool      = "unknown_tool"
	ReasonNoReplica        = "no_replica"
	ReasonColdStart        = "cold_start"
	ReasonMRTRMismatch     = "mrtr_mismatch"
	ReasonTransportError   = "transport_error"
	ReasonToolError        = "tool_error"
	ReasonContextCanceled  = "context_canceled"
	ReasonDeadlineExceeded = "deadline_exceeded"
	ReasonInputRequired    = "input_required"
	ReasonCodeMode         = "code_mode"
)

// Drop reasons for the independent recorder status. These never enter the
// JSONL record stream.
const (
	DropQueueFull     = "queue_full"
	DropTooLarge      = "record_too_large"
	DropClosed        = "writer_closed"
	DropGeneration    = "generation_mismatch"
	DropCapExhausted  = "cap_exhausted"
	DropWriteError    = "write_error"
	DropSyncError     = "sync_error"
	DropShutdownLimit = "shutdown_limit"
)

// Writer health values reported independently of the destination file.
const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
	HealthStopped  = "stopped"
)

// LossUnknown is the post-restart / offline historical-loss value.
const LossUnknown = "unknown"

// Record is the schema-versioned metadata-only final-disposition record.
// It is the allowlist: generated IDs, times/duration, bounded target
// identifiers, disposition/stage/reason, optional replica and genuine
// trace correlation, and optional caller-declared labels. Argument and
// result values, hashes, code, raw errors, tokens, headers, URLs, host
// paths, and approval claims are excluded by construction.
type Record struct {
	SchemaVersion      int       `json:"schema_version"`
	RecorderInstanceID string    `json:"recorder_instance_id"`
	Sequence           uint64    `json:"sequence"`
	AttemptID          string    `json:"attempt_id"`
	ParentAttemptID    string    `json:"parent_attempt_id,omitempty"`
	RootAttemptID      string    `json:"root_attempt_id,omitempty"`
	PreviousAttemptID  string    `json:"previous_attempt_id,omitempty"`
	StartedAt          time.Time `json:"started_at"`
	ReturnedAt         time.Time `json:"returned_at"`
	DurationMS         int64     `json:"duration_ms"`
	DownstreamMS       int64     `json:"downstream_duration_ms,omitempty"`
	RequestedName      string    `json:"requested_name,omitempty"`
	RequestedOmitted   string    `json:"requested_name_omitted,omitempty"`
	ResolvedServer     string    `json:"resolved_server,omitempty"`
	ResolvedServerOmit string    `json:"resolved_server_omitted,omitempty"`
	ResolvedTool       string    `json:"resolved_tool,omitempty"`
	ResolvedToolOmit   string    `json:"resolved_tool_omitted,omitempty"`
	Disposition        string    `json:"disposition"`
	Stage              string    `json:"stage"`
	Reason             string    `json:"reason"`
	ReplicaID          *int      `json:"replica_id,omitempty"`
	TraceID            string    `json:"trace_id,omitempty"`
	ClientLabel        string    `json:"client_label,omitempty"`
	AccessLabel        string    `json:"access_label,omitempty"`
}

// Warning is a value-free reader diagnostic. Malformed contents are never
// included.
type Warning struct {
	Code    string `json:"code"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

const (
	WarnPartialTail       = "partial_tail"
	WarnMalformed         = "malformed"
	WarnUnsupportedSchema = "unsupported_schema"
	WarnTruncatedQuery    = "truncated"
	WarnUnreadable        = "unreadable"
)
