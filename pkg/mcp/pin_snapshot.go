package mcp

import "context"

// PinSnapshot is immutable approval evidence for one registration generation.
// Records returns an owned deep copy, including hidden trust records. It is not
// a callable tool inventory and must never be passed to the router.
type PinSnapshot interface {
	Records() []Tool
	Hash() string
	Generation() uint64
}

// PinSnapshotSource separates trust evidence from a client's filtered Tools.
// Approval must use the synchronized card-trust service, not this view alone.
// Initialization must register the snapshot and its lifecycle callbacks with
// the gateway's CardTrustService before gateway registration can publish tools.
type PinSnapshotSource interface {
	PinSnapshot() PinSnapshot
}

// CardPinDecision describes mandatory card trust, independently of tool policy.
type CardPinDecision struct {
	Trusted     bool
	FirstUse    bool
	NewIdentity bool
}

// CardPinStorage persists complete snapshots before granting card trust. Verify
// never approves changed bytes under an existing configured identity.
type CardPinStorage interface {
	VerifyCard(context.Context, string, PinSnapshot) (CardPinDecision, error)
	CheckCard(context.Context, string, PinSnapshot) (CardPinDecision, error)
	ApproveCard(context.Context, string, PinSnapshot) error
}
