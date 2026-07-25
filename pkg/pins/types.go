package pins

import "time"

// Status values for ServerPins.
const (
	StatusPinned                  = "pinned"
	StatusDrift                   = "drift"
	StatusApprovedPendingRedeploy = "approved_pending_redeploy"
)

// Verify status values returned in VerifyResult.
const (
	VerifyStatusPinned       = "pinned"       // first pin, just stored
	VerifyStatusVerified     = "verified"     // hashes match
	VerifyStatusDrift        = "drift"        // tool hashes changed
	VerifyStatusNewTools     = "new_tools"    // server added tools (no drift, auto-pinned)
	VerifyStatusRemovedTools = "removed_tools" // server removed tools (warning only)
)

// PinFile is the top-level JSON structure stored at ~/.gridctl/pins/{stackName}.json.
type PinFile struct {
	Version   string                `json:"version"`
	Stack     string                `json:"stack"`
	CreatedAt time.Time             `json:"created_at"`
	Servers   map[string]*ServerPins `json:"servers"`
}

// ServerPins holds the pin state for a single MCP server.
type ServerPins struct {
	ServerHash     string               `json:"server_hash"`
	PinnedAt       time.Time            `json:"pinned_at"`
	LastVerifiedAt time.Time            `json:"last_verified_at"`
	ToolCount      int                  `json:"tool_count"`
	Status         string               `json:"status"`
	Tools          map[string]*PinRecord `json:"tools"`
}

// PinRecord holds the hash and metadata for a single tool definition.
// Description is stored to enable human-readable diff output on drift.
// Findings are the poisoning-scan results captured when the tool was pinned;
// they are derived, advisory data (an older gridctl rewriting the file simply
// drops them, so their presence does not require a file-version bump).
// InputSchema and OutputSchema hold the canonical serialization of the pinned
// schemas so a schema-only drift can show what changed; like Findings they are
// derived data (an older gridctl rewriting the file drops them, and pins
// recorded before schema capture simply lack them), so no file-version bump.
type PinRecord struct {
	Hash         string    `json:"hash"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	PinnedAt     time.Time `json:"pinned_at"`
	Findings     []Finding `json:"findings,omitempty"`
	InputSchema  string    `json:"input_schema,omitempty"`
	OutputSchema string    `json:"output_schema,omitempty"`
}

// VerifyResult contains the result of a VerifyOrPin or Verify call.
type VerifyResult struct {
	ServerName    string
	Status        string
	ModifiedTools []ToolDiff
	NewTools      []string
	RemovedTools  []string
}

// HasDrift returns true if any pinned tools have changed hashes.
func (r *VerifyResult) HasDrift() bool {
	return len(r.ModifiedTools) > 0
}

// Change kinds carried on ToolDiff.ChangeKinds, describing which parts of a
// tool's definition moved. ChangeKindSchemaUncaptured marks the legacy state
// where the pin predates schema capture: the old schemas are unrecoverable,
// so the hash move may include a schema change that cannot be shown. It is
// reported alongside description when the prose also moved.
const (
	ChangeKindDescription      = "description"
	ChangeKindInputSchema      = "input_schema"
	ChangeKindOutputSchema     = "output_schema"
	ChangeKindSchemaUncaptured = "schema_uncaptured"
)

// ToolDiff describes a change in a single tool's definition.
// Findings are poisoning-scan results for the NEW definition, computed at
// verify time so the reviewer sees them beside the diff they annotate.
// Schema fields carry canonical serializations; Old* are empty for pins
// recorded before schema capture (see ChangeKindSchemaUncaptured).
type ToolDiff struct {
	Name            string
	OldHash         string
	NewHash         string
	OldDescription  string
	NewDescription  string
	OldInputSchema  string
	NewInputSchema  string
	OldOutputSchema string
	NewOutputSchema string
	ChangeKinds     []string
	Findings        []Finding
}
