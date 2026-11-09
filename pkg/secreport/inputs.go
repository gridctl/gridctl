package secreport

import "time"

// Inputs is the typed safe snapshot the assembler consumes. Nil optional
// producers yield unknown checks rather than fabricated evidence.
type Inputs struct {
	Source      SourceIdentity
	Stack       *StackView
	Pins        *PinView
	SkillPins   *SkillPinView
	ContextPins *ContextPinView
	Proposals   *ProposalView
	Startup     *StartupView
	Execution   []ExecutionView
}

// StackView is a value-free authored configuration projection.
type StackView struct {
	Name             string
	Servers          []ServerView
	References       map[string][]ReferenceSite
	SetMembers       map[string][]string
	UnscopedSetCount int
	Gateway          *GatewayDeclView
	Scan             *ScanDeclView
	Pinning          *PinningDeclView
	Policy           *PolicyDeclView
}

// ServerView is authored server identity without commands or credentials.
type ServerView struct {
	Name              string
	Kind              string
	Image             string
	SourceType        string
	SourcePackage     string
	SourceVersion     string
	SourceRef         string
	SourceHostPath    string
	BuildDigest       string
	ObservedImage     string
	ObservedImageID   string
	ManifestDigest    string
	SignatureBound    bool
	ExecutionMode     string
	ExecutionDeclared bool
}

// ReferenceSite counts a variable reference without values or field paths
// that may contain operands.
type ReferenceSite struct {
	Kind       string
	Name       string
	Target     string
	TargetKind string
	Untargeted bool
}

// GatewayDeclView is an auth/bind declaration, never token or header values.
type GatewayDeclView struct {
	AuthDeclared bool
	AuthType     string
	Bind         string
	Insecure     bool
}

// ScanDeclView is advisory scanner configuration.
type ScanDeclView struct {
	Enabled bool
	Ignore  []string
}

// PinningDeclView is schema-pinning enablement as declared.
type PinningDeclView struct {
	Enabled bool
}

// PolicyDeclView is the authored per-tool policy block, never match
// patterns that could be confused with arguments. Declared is false when
// the key was omitted. An empty block is declared.
type PolicyDeclView struct {
	Declared          bool
	Default           string
	RuleCount         int
	BlockDestructive  bool
	ApprovalRuleCount int
}

// PinView is a stored pin snapshot. Presence is independent of Verify.
type PinView struct {
	Available bool
	Enabled   bool
	Scan      *ScanDeclView
	Servers   map[string]ServerPinView
}

// ServerPinView is one server's stored pin record.
type ServerPinView struct {
	Status             string
	ToolCount          int
	PinnedAt           time.Time
	LastVerifiedAt     time.Time
	LegacySchemeCount  int
	CurrentSchemeCount int
	Findings           []FindingView
	// Approval fields are presence and the bound hash only. Reason text,
	// references, and the approver name stay out of the report.
	ApprovalRecorded bool
	ApprovedAt       time.Time
	ApprovedHash     string
}

// FindingView is stored advisory metadata without snippets or decoded text.
type FindingView struct {
	Code       string
	Severity   string
	Confidence string
	Field      string
	Suppressed bool
}

// SkillPinView is stored skill-pin evidence scoped to skill subjects.
type SkillPinView struct {
	Available bool
	Skills    map[string]SkillPinRecordView
}

// SkillPinRecordView is one skill pin without document bodies.
type SkillPinRecordView struct {
	Status           string
	Source           string
	PinnedAt         time.Time
	LastVerifiedAt   time.Time
	FindingCodes     []string
	ApprovalRecorded bool
	ApprovedAt       time.Time
	ApprovedHash     string
}

// ContextPinView is stored context-pin evidence scoped to context subjects.
// It carries no document body, finding message, or path.
type ContextPinView struct {
	Available bool
	Fragments map[string]ContextPinRecordView
}

// ContextPinRecordView is one fragment pin without document content.
type ContextPinRecordView struct {
	Status         string
	Source         string
	Pack           string
	PinnedAt       time.Time
	LastVerifiedAt time.Time
	FindingCodes   []string
}

// ProposalView counts pending proposals in the stackless proposal store.
// It carries no target name, diff, reason, or evidence.
type ProposalView struct {
	Available bool
	Pending   int
}

// StartupView is a value-free active startup security snapshot.
type StartupView struct {
	AuthEnabled        bool
	AuthType           string
	Bind               string
	Insecure           bool
	OriginsMode        string
	AllowedOriginCount int
	DiscoveryRoutes    []string
}

// ExecutionView is per-replica execution evidence already collected.
type ExecutionView struct {
	Server     string
	Replica    string
	Kind       string
	Mode       string
	Outcome    string
	Eligible   bool
	ObservedAt time.Time
	Runtime    string
	Instance   string
	Revision   string
}
