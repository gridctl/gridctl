package secreport

import "time"

const (
	SchemaVersion = "gridctl.security-report.v1"

	OutcomePass          = "pass"
	OutcomeFail          = "fail"
	OutcomeWarn          = "warn"
	OutcomeUnknown       = "unknown"
	OutcomeNotApplicable = "not-applicable"

	BasisDeclared = "declared"
	BasisObserved = "observed"
	BasisVerified = "verified"

	AvailabilityAvailable   = "available"
	AvailabilityUnavailable = "unavailable"
	AvailabilityPartial     = "partial"

	FreshnessCurrent = "current"
	FreshnessStale   = "stale"
	FreshnessUnknown = "unknown"

	SourceFile     = "file"
	SourceSnapshot = "snapshot"
	SourceGateway  = "gateway"

	SubjectGateway = "gateway"
	SubjectServer  = "server"
	SubjectSkill   = "skill"
	SubjectReplica = "replica"

	ActionViewPins      = "view_pins"
	ActionViewSkillPins = "view_skill_pins"

	CoverageComplete = "complete"
	CoveragePartial  = "partial"
	CoverageNone     = "none"
)

const (
	PredPinStore              = "pin.schema.store"
	PredPinBaseline           = "pin.schema.baseline"
	PredPinContinuity         = "pin.schema.continuity"
	PredPinScheme             = "pin.schema.scheme"
	PredPinScanCoverage       = "pin.scan.coverage"
	PredPinScanFindings       = "pin.scan.findings"
	PredSkillPinBaseline      = "skill.pin.baseline"
	PredSkillPinContinuity    = "skill.pin.continuity"
	PredSourceDeclared        = "source.declared"
	PredSourceBuildDigest     = "source.build_digest"
	PredSourceImageObserved   = "source.image_observed"
	PredSourceManifest        = "source.manifest"
	PredSourceSignature       = "source.signature"
	PredVarCompleteness       = "var.completeness"
	PredVarReferences         = "var.references"
	PredGatewayAuthDeclared   = "gateway.auth.declared"
	PredGatewayAuthStartup    = "gateway.auth.startup"
	PredExecutionAvailability = "execution.availability"
	PredExecutionEnforcement  = "execution.enforcement"
)

// PredicateInventory is the finite named check catalog. Coverage "complete"
// refers only to this list, not to complete security knowledge.
var PredicateInventory = []string{
	PredPinStore,
	PredPinBaseline,
	PredPinContinuity,
	PredPinScheme,
	PredPinScanCoverage,
	PredPinScanFindings,
	PredSkillPinBaseline,
	PredSkillPinContinuity,
	PredSourceDeclared,
	PredSourceBuildDigest,
	PredSourceImageObserved,
	PredSourceManifest,
	PredSourceSignature,
	PredVarCompleteness,
	PredVarReferences,
	PredGatewayAuthDeclared,
	PredGatewayAuthStartup,
	PredExecutionAvailability,
	PredExecutionEnforcement,
}

// FailPredicates are the only predicates that may produce exit-one failures.
// They reuse producer-recorded negatives or documented enforcement outcomes.
var FailPredicates = []string{
	PredPinContinuity,
	PredSkillPinContinuity,
	PredExecutionEnforcement,
}

// Report is the versioned value-free security evidence DTO.
type Report struct {
	SchemaVersion      string         `json:"schema_version"`
	GeneratedAt        time.Time      `json:"generated_at"`
	Source             SourceIdentity `json:"source"`
	Coverage           Coverage       `json:"coverage"`
	Checks             []Check        `json:"checks"`
	Limitations        []string       `json:"limitations"`
	GenerationOK       bool           `json:"-"`
	FailCount          int            `json:"fail_count"`
	WarnCount          int            `json:"warn_count"`
	UnknownCount       int            `json:"unknown_count"`
	NotApplicableCount int            `json:"not_applicable_count"`
	PassCount          int            `json:"pass_count"`
}

// SourceIdentity names the explicit selected source without credentials.
type SourceIdentity struct {
	Kind       string `json:"kind"`
	Display    string `json:"display"`
	Historical bool   `json:"historical"`
	StackName  string `json:"stack_name,omitempty"`
}

// Coverage describes finite predicate inventory results, not assurance.
type Coverage struct {
	Status              string   `json:"status"`
	PredicatesTotal     int      `json:"predicates_total"`
	PredicatesEvaluated int      `json:"predicates_evaluated"`
	PredicatesUnknown   int      `json:"predicates_unknown"`
	IncludedScopes      []string `json:"included_scopes"`
	ExcludedScopes      []string `json:"excluded_scopes"`
	UnknownGaps         int      `json:"unknown_gaps"`
}

// Check is one scoped predicate outcome with independent evidence facets.
type Check struct {
	ID          string       `json:"id"`
	Predicate   string       `json:"predicate"`
	Subject     Subject      `json:"subject"`
	Outcome     string       `json:"outcome"`
	ReasonCode  string       `json:"reason_code"`
	Explanation string       `json:"explanation"`
	Evidence    Evidence     `json:"evidence"`
	Facts       CheckFacts   `json:"facts,omitempty"`
	Suppression *Suppression `json:"suppression,omitempty"`
	Actions     []Action     `json:"actions,omitempty"`
	Limitations []string     `json:"limitations,omitempty"`
}

// CheckFacts are typed, allowlisted values used to rebuild explanations.
type CheckFacts struct {
	DeclaredSource     string   `json:"declared_source,omitempty"`
	ReferenceSites     *int     `json:"reference_sites,omitempty"`
	WorkloadConsumers  *int     `json:"workload_consumers,omitempty"`
	UnscopedConsumers  *int     `json:"unscoped_consumers,omitempty"`
	FindingCodes       []string `json:"finding_codes,omitempty"`
	FindingSeverities  []string `json:"finding_severities,omitempty"`
	FindingConfidences []string `json:"finding_confidences,omitempty"`
	AuthType           string   `json:"auth_type,omitempty"`
	Bind               string   `json:"bind,omitempty"`
	EffectiveBind      string   `json:"effective_bind,omitempty"`
	Kind               string   `json:"kind,omitempty"`
	RecordedOutcome    string   `json:"recorded_outcome,omitempty"`
	Instance           string   `json:"instance,omitempty"`
	Revision           string   `json:"revision,omitempty"`
}

// Subject is a bounded identity for the check.
type Subject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Evidence separates basis, availability, and freshness.
type Evidence struct {
	Basis              string     `json:"basis"`
	Availability       string     `json:"availability"`
	Freshness          string     `json:"freshness"`
	FreshnessCondition string     `json:"freshness_condition,omitempty"`
	ObservedAt         *time.Time `json:"observed_at,omitempty"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
	ScannedAt          *time.Time `json:"scanned_at,omitempty"`
	Producer           string     `json:"producer,omitempty"`
	ProducerVersion    string     `json:"producer_version,omitempty"`
	Ruleset            string     `json:"ruleset,omitempty"`
	Digest             string     `json:"digest,omitempty"`
	VerificationMethod string     `json:"verification_method,omitempty"`
	SubjectBinding     string     `json:"subject_binding,omitempty"`
	PredicateScope     string     `json:"predicate_scope,omitempty"`
}

func intPtr(v int) *int { return &v }

// Suppression is independent metadata and never a pass substitute.
type Suppression struct {
	Codes      []string `json:"codes,omitempty"`
	ReasonCode string   `json:"reason_code"`
}

// Action is an allowlisted link to an existing review surface.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}
