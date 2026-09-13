package secreport

func knownLimitation(text string) bool {
	_, ok := limitationCatalog[text]
	return ok
}

func retainLimitations(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		if !knownLimitation(v) || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func knownOutcome(v string) bool {
	switch v {
	case OutcomePass, OutcomeFail, OutcomeWarn, OutcomeUnknown, OutcomeNotApplicable:
		return true
	default:
		return false
	}
}

func knownBasis(v string) bool {
	switch v {
	case BasisDeclared, BasisObserved, BasisVerified:
		return true
	default:
		return false
	}
}

func knownAvailability(v string) bool {
	switch v {
	case AvailabilityAvailable, AvailabilityUnavailable, AvailabilityPartial:
		return true
	default:
		return false
	}
}

func knownFreshness(v string) bool {
	switch v {
	case FreshnessCurrent, FreshnessStale, FreshnessUnknown:
		return true
	default:
		return false
	}
}

func knownSubjectKind(v string) bool {
	switch v {
	case SubjectGateway, SubjectServer, SubjectSkill, SubjectReplica:
		return true
	default:
		return false
	}
}

func knownPredicate(v string) bool {
	for _, pred := range PredicateInventory {
		if pred == v {
			return true
		}
	}
	return false
}

func failPredicateAllowed(pred string) bool {
	for _, p := range FailPredicates {
		if p == pred {
			return true
		}
	}
	return false
}

var limitationCatalog = map[string]bool{
	"This report is not a certification, trust score, or complete security assessment.":                            true,
	"Exit zero means no established failures among documented fail predicates, not that the stack is secure.":      true,
	"Allowlisted identifiers may still contain operator-authored secrets; this passive report cannot detect them.": true,
	"Refresh re-reads this passive report; it does not re-observe downstream evidence.":                            true,
	"Truncation bounds identifier length; it is not redaction.":                                                    true,
	"This rendering is supplied historical evidence, not a fresh verification.":                                    true,
	"Pin store was not supplied to this report.":                                                                   true,
	"Cross-server P006 evidence is not collected at report time.":                                                  true,
	"Absence of findings is not a completed clean scan.":                                                           true,
	"Pins record definition continuity, not publisher trust.":                                                      true,
	"Not applicable because no pin record exists.":                                                                 true,
	"Automatic additions are not distinguished from human approval.":                                               true,
	"Known negative observation from the pin store.":                                                               true,
	"Legacy hashes do not cover outputSchema.":                                                                     true,
	"No findings without coverage metadata does not establish a completed clean scan.":                             true,
	"Heuristic findings remain warnings.":                                                                          true,
	"Finding snippets are omitted.":                                                                                true,
	"Not applicable because scanning is disabled.":                                                                 true,
	"Historical findings remain available independently of current scan enablement.":                               true,
	"Skill pins are scoped to skill subjects only.":                                                                true,
	"Skill pins do not imply downstream server trust.":                                                             true,
	"Not applicable because no skill pin records exist.":                                                           true,
	"Known negative observation from the skill pin store.":                                                         true,
	"Declared source; running artifact verification unavailable.":                                                  true,
	"Not applicable for non-container subjects. This is not a claim that remote execution is secure.":              true,
	"Not applicable for non-container subjects.":                                                                   true,
	"A digest-shaped declaration is not authenticated verification.":                                               true,
	"Build-input digest is not registry manifest identity.":                                                        true,
	"Observed image reference is not an immutable deployed snapshot of every replica.":                             true,
	"Manifest identity is producer-supplied.":                                                                      true,
	"Image labels are source assertions, not proof of immutable running-artifact identity.":                        true,
	"Missing downstream signature evidence is unknown, not unsigned.":                                              true,
	"Nil set membership is unknown, not a known empty set.":                                                        true,
	"Without classification metadata this report names variables and references, not secret exposure.":             true,
	"Reference count and workload breadth are different units.":                                                    true,
	"Unscoped set expansion cannot name distinct workloads without membership.":                                    true,
	"A saved file does not prove route enforcement.":                                                               true,
	"A saved token presence does not prove route enforcement.":                                                     true,
	"Only declarations exist; enforcement remains unverified.":                                                     true,
	"Startup snapshot is not a proof of route correctness.":                                                        true,
	"Missing current control evidence is unknown.":                                                                 true,
	"A generated nonroot image does not substitute for verified runtime controls.":                                 true,
	"A source label or digest-shaped declaration does not substitute for verified runtime controls.":               true,
	"This is a launch/admission snapshot, not continuous protection.":                                              true,
	"Observed execution evidence is not continuous monitoring.":                                                    true,
	"Imported snapshots cannot authenticate verification claims.":                                                  true,
}

func sanitizeFacts(in CheckFacts) CheckFacts {
	out := CheckFacts{
		DeclaredSource:     SanitizeIdentifier(in.DeclaredSource),
		AuthType:           SanitizeIdentifier(in.AuthType),
		Bind:               SanitizeIdentifier(in.Bind),
		EffectiveBind:      SanitizeIdentifier(in.EffectiveBind),
		Kind:               SanitizeIdentifier(in.Kind),
		RecordedOutcome:    SanitizeIdentifier(in.RecordedOutcome),
		Instance:           SanitizeIdentifier(in.Instance),
		Revision:           SanitizeIdentifier(in.Revision),
		FindingCodes:       unique(in.FindingCodes),
		FindingSeverities:  unique(in.FindingSeverities),
		FindingConfidences: unique(in.FindingConfidences),
	}
	if in.ReferenceSites != nil {
		v := *in.ReferenceSites
		if v < 0 {
			v = 0
		}
		out.ReferenceSites = intPtr(v)
	}
	if in.WorkloadConsumers != nil {
		v := *in.WorkloadConsumers
		if v < 0 {
			v = 0
		}
		out.WorkloadConsumers = intPtr(v)
	}
	if in.UnscopedConsumers != nil {
		v := *in.UnscopedConsumers
		if v < 0 {
			v = 0
		}
		out.UnscopedConsumers = intPtr(v)
	}
	return out
}
