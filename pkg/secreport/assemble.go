package secreport

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

func nowUTC() time.Time { return time.Now().UTC() }

func Assemble(generatedAt time.Time, in Inputs) *Report {
	generatedAt = generatedAt.UTC()
	report := &Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   generatedAt,
		Source:        in.Source,
		Checks:        []Check{},
		Limitations:   defaultLimitations(in.Source.Historical),
		GenerationOK:  true,
	}
	report.Source.Display = SanitizeIdentifier(report.Source.Display)
	report.Source.StackName = SanitizeIdentifier(report.Source.StackName)
	report.Checks = append(report.Checks, pinChecks(in)...)
	report.Checks = append(report.Checks, skillPinChecks(in)...)
	report.Checks = append(report.Checks, sourceChecks(in)...)
	report.Checks = append(report.Checks, varChecks(in)...)
	report.Checks = append(report.Checks, gatewayChecks(in)...)
	report.Checks = append(report.Checks, executionChecks(in)...)
	sort.SliceStable(report.Checks, func(i, j int) bool {
		if report.Checks[i].Predicate != report.Checks[j].Predicate {
			return predicateIndex(report.Checks[i].Predicate) < predicateIndex(report.Checks[j].Predicate)
		}
		if report.Checks[i].Subject.Kind != report.Checks[j].Subject.Kind {
			return report.Checks[i].Subject.Kind < report.Checks[j].Subject.Kind
		}
		return report.Checks[i].Subject.Name < report.Checks[j].Subject.Name
	})
	summarize(report)
	return report
}

func predicateIndex(id string) int {
	for i, pred := range PredicateInventory {
		if pred == id {
			return i
		}
	}
	return len(PredicateInventory)
}

func summarize(report *Report) {
	report.FailCount = 0
	report.WarnCount = 0
	report.UnknownCount = 0
	report.NotApplicableCount = 0
	report.PassCount = 0
	unknownPred := map[string]bool{}
	evaluated := map[string]bool{}
	for i := range report.Checks {
		c := &report.Checks[i]
		if knownPredicate(c.Predicate) {
			evaluated[c.Predicate] = true
		}
		switch c.Outcome {
		case OutcomeFail:
			report.FailCount++
		case OutcomeWarn:
			report.WarnCount++
		case OutcomeUnknown:
			report.UnknownCount++
			unknownPred[c.Predicate] = true
		case OutcomeNotApplicable:
			report.NotApplicableCount++
		case OutcomePass:
			report.PassCount++
		default:
			report.UnknownCount++
			unknownPred[c.Predicate] = true
		}
	}
	included := make([]string, 0, len(PredicateInventory))
	excluded := make([]string, 0)
	for _, pred := range PredicateInventory {
		if evaluated[pred] {
			included = append(included, pred)
		} else {
			excluded = append(excluded, pred)
		}
	}
	report.Coverage = Coverage{
		PredicatesTotal:     len(PredicateInventory),
		PredicatesEvaluated: len(included),
		PredicatesUnknown:   len(unknownPred),
		IncludedScopes:      included,
		ExcludedScopes:      excluded,
		UnknownGaps:         report.UnknownCount,
		Status:              CoverageComplete,
	}
	if len(excluded) > 0 || report.UnknownCount > 0 {
		report.Coverage.Status = CoveragePartial
	}
	if report.PassCount+report.FailCount+report.WarnCount == 0 {
		report.Coverage.Status = CoverageNone
	}
}

func gatewaySubject() Subject {
	return Subject{Kind: SubjectGateway, Name: "gateway"}
}

func serverSubject(name string) Subject {
	return Subject{Kind: SubjectServer, Name: SanitizeIdentifier(name)}
}

func skillSubject(name string) Subject {
	return Subject{Kind: SubjectSkill, Name: SanitizeIdentifier(name)}
}

func replicaSubject(server, replica string) Subject {
	name := SanitizeIdentifier(server)
	if replica != "" {
		name = name + ":" + SanitizeIdentifier(replica)
	}
	return Subject{Kind: SubjectReplica, Name: name}
}

func check(id, predicate string, subject Subject, outcome, reason, explanation string, ev Evidence, limitations []string, actions ...*Action) Check {
	item := Check{
		ID:          id,
		Predicate:   predicate,
		Subject:     subject,
		Outcome:     outcome,
		ReasonCode:  reason,
		Explanation: explanation,
		Evidence:    ev,
		Limitations: limitations,
	}
	for _, action := range actions {
		if action != nil {
			item.Actions = append(item.Actions, *action)
		}
	}
	if item.Evidence.Basis == BasisVerified && (item.Evidence.VerificationMethod == "" || item.Evidence.SubjectBinding == "" || item.Evidence.PredicateScope == "") {
		item.Evidence.Basis = BasisDeclared
		item.Evidence.VerificationMethod = ""
		item.Evidence.SubjectBinding = ""
		item.Evidence.PredicateScope = ""
	}
	return item
}

func declaredEvidence() Evidence {
	return Evidence{Basis: BasisDeclared, Availability: AvailabilityAvailable, Freshness: FreshnessUnknown, FreshnessCondition: "no_identity_freshness_condition"}
}

func unknownEvidence() Evidence {
	return Evidence{Basis: BasisDeclared, Availability: AvailabilityUnavailable, Freshness: FreshnessUnknown, FreshnessCondition: "no_identity_freshness_condition"}
}

func observedEvidence(at time.Time) Evidence {
	ev := Evidence{Basis: BasisObserved, Availability: AvailabilityAvailable, Freshness: FreshnessUnknown, FreshnessCondition: "producer_timestamp_present_without_current_binding"}
	if !at.IsZero() {
		t := at.UTC()
		ev.ObservedAt = &t
	}
	return ev
}

func pinChecks(in Inputs) []Check {
	var out []Check
	if in.Pins == nil {
		out = append(out, check("pin.schema.store.gateway", PredPinStore, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Schema pin store snapshot unavailable.", unknownEvidence(), []string{"Pin store was not supplied to this report."}))
		out = append(out, check("pin.schema.baseline.gateway", PredPinBaseline, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Schema pin record presence cannot be established.", unknownEvidence(), nil))
		out = append(out, check("pin.schema.continuity.gateway", PredPinContinuity, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Schema pin continuity cannot be established.", unknownEvidence(), nil))
		out = append(out, check("pin.schema.scheme.gateway", PredPinScheme, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Pin hash scheme coverage cannot be established.", unknownEvidence(), nil))
		out = append(out, check("pin.scan.coverage.gateway", PredPinScanCoverage, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Advisory scan coverage metadata is unavailable.", unknownEvidence(), []string{"Cross-server P006 evidence is not collected at report time."}))
		out = append(out, check("pin.scan.findings.gateway", PredPinScanFindings, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Stored advisory findings are unavailable.", unknownEvidence(), []string{"Absence of findings is not a completed clean scan."}))
		return out
	}
	storeOutcome, storeReason, storeText := OutcomePass, "store_available", "Schema pin store snapshot is available. Records are not explicit approvals."
	if !in.Pins.Enabled {
		storeReason, storeText = "store_disabled", "Schema pinning is declared disabled. Absence of pins is a configuration fact, not a policy violation."
	}
	out = append(out, check("pin.schema.store.gateway", PredPinStore, gatewaySubject(), storeOutcome, storeReason, storeText, declaredEvidence(), []string{"Pins record definition continuity, not publisher trust."}))
	servers := serverNames(in)
	if len(servers) == 0 {
		out = append(out, check("pin.schema.baseline.gateway", PredPinBaseline, gatewaySubject(), OutcomePass, "no_declared_servers", "No declared MCP servers; pin baseline is a known empty configuration.", declaredEvidence(), nil))
	}
	for _, name := range servers {
		rec, ok := in.Pins.Servers[name]
		if !ok {
			out = append(out, check("pin.schema.baseline."+name, PredPinBaseline, serverSubject(name), OutcomePass, "pin_record_absent", "No pin record is stored for "+name+". That is a configuration fact, not a policy violation.", declaredEvidence(), nil, viewPinsAction(name)))
			out = append(out, check("pin.schema.continuity."+name, PredPinContinuity, serverSubject(name), OutcomeNotApplicable, "no_pin_record", "Continuity is not applicable without a stored pin record.", declaredEvidence(), []string{"Not applicable because no pin record exists."}, viewPinsAction(name)))
			out = append(out, check("pin.schema.scheme."+name, PredPinScheme, serverSubject(name), OutcomeNotApplicable, "no_pin_record", "Hash scheme coverage is not applicable without a stored pin record.", declaredEvidence(), []string{"Not applicable because no pin record exists."}))
			out = append(out, scanFindingCheck(name, rec, false, in.Pins.Scan))
			continue
		}
		out = append(out, check("pin.schema.baseline."+name, PredPinBaseline, serverSubject(name), OutcomePass, "pin_record_present", "A stored pin record exists for "+name+". Presence is not explicit approval.", declaredEvidence(), []string{"Automatic additions are not distinguished from human approval."}, viewPinsAction(name)))
		out = append(out, continuityCheck(name, rec))
		out = append(out, schemeCheck(name, rec))
		out = append(out, scanFindingCheck(name, rec, true, in.Pins.Scan))
	}
	out = append(out, scanCoverageCheck(in.Pins.Scan, in.Pins.Servers))
	return out
}

func continuityCheck(name string, rec ServerPinView) Check {
	ev := declaredEvidence()
	if !rec.LastVerifiedAt.IsZero() {
		t := rec.LastVerifiedAt
		ev.VerifiedAt = &t
		ev.Freshness = FreshnessUnknown
		ev.FreshnessCondition = "last_verified_at_is_not_scan_time"
	}
	switch rec.Status {
	case "drift":
		return check("pin.schema.continuity."+name, PredPinContinuity, serverSubject(name), OutcomeFail, "pin_status_drift", "Stored pin status is drift for "+name+".", ev, []string{"Known negative observation from the pin store."}, viewPinsAction(name))
	case "approved_pending_redeploy":
		return check("pin.schema.continuity."+name, PredPinContinuity, serverSubject(name), OutcomeWarn, "pin_status_pending_redeploy", "Stored pin status is approved pending redeploy for "+name+".", ev, nil, viewPinsAction(name))
	default:
		return check("pin.schema.continuity."+name, PredPinContinuity, serverSubject(name), OutcomePass, "pin_status_pinned", "Stored pin status is pinned for "+name+". Continuity is recorded definition matching, not publisher trust.", ev, nil, viewPinsAction(name))
	}
}

func schemeCheck(name string, rec ServerPinView) Check {
	switch {
	case rec.LegacySchemeCount > 0 && rec.CurrentSchemeCount > 0:
		return check("pin.schema.scheme."+name, PredPinScheme, serverSubject(name), OutcomeWarn, "scheme_mixed", "Pin records for "+name+" mix current and legacy hash schemes.", declaredEvidence(), []string{"Legacy hashes do not cover outputSchema."})
	case rec.LegacySchemeCount > 0:
		return check("pin.schema.scheme."+name, PredPinScheme, serverSubject(name), OutcomeWarn, "scheme_legacy", "Pin records for "+name+" use the legacy hash scheme.", declaredEvidence(), []string{"Legacy hashes do not cover outputSchema."})
	case rec.CurrentSchemeCount > 0:
		return check("pin.schema.scheme."+name, PredPinScheme, serverSubject(name), OutcomePass, "scheme_current", "Pin records for "+name+" use the current hash scheme.", declaredEvidence(), nil)
	default:
		return check("pin.schema.scheme."+name, PredPinScheme, serverSubject(name), OutcomeUnknown, "scheme_unknown", "Pin hash scheme coverage for "+name+" is unknown.", unknownEvidence(), nil)
	}
}

func scanCoverageCheck(scan *ScanDeclView, servers map[string]ServerPinView) Check {
	lim := []string{"Cross-server P006 evidence is not collected at report time.", "No findings without coverage metadata does not establish a completed clean scan."}
	if scan == nil {
		return check("pin.scan.coverage.gateway", PredPinScanCoverage, gatewaySubject(), OutcomeUnknown, "scan_config_unknown", "Advisory scan configuration is unknown.", unknownEvidence(), lim)
	}
	if !scan.Enabled {
		return check("pin.scan.coverage.gateway", PredPinScanCoverage, gatewaySubject(), OutcomePass, "scan_disabled", "Advisory scanning is declared disabled.", declaredEvidence(), lim)
	}
	hasTime := false
	for _, rec := range servers {
		for range rec.Findings {
			if !rec.PinnedAt.IsZero() {
				hasTime = true
			}
		}
	}
	if !hasTime {
		return check("pin.scan.coverage.gateway", PredPinScanCoverage, gatewaySubject(), OutcomeUnknown, "scan_time_unknown", "Scan is declared enabled; scan time and ruleset are unknown.", unknownEvidence(), lim)
	}
	return check("pin.scan.coverage.gateway", PredPinScanCoverage, gatewaySubject(), OutcomePass, "scan_config_known", "Advisory scanning is declared enabled. Stored findings are historical.", declaredEvidence(), lim)
}

func scanFindingCheck(name string, rec ServerPinView, hasRecord bool, scan *ScanDeclView) Check {
	lim := []string{"Heuristic findings remain warnings.", "Absence of findings is not a completed clean scan.", "Finding snippets are omitted."}
	if !hasRecord {
		if scan != nil && !scan.Enabled {
			return check("pin.scan.findings."+name, PredPinScanFindings, serverSubject(name), OutcomeNotApplicable, "scan_disabled", "Advisory findings are not applicable while scanning is disabled.", declaredEvidence(), []string{"Not applicable because scanning is disabled."})
		}
		return check("pin.scan.findings."+name, PredPinScanFindings, serverSubject(name), OutcomeUnknown, "findings_unavailable", "Stored advisory findings are unavailable without a pin record.", unknownEvidence(), lim)
	}
	var codes, severities, confidences, ignored []string
	for _, f := range rec.Findings {
		if f.Code == "" {
			continue
		}
		codes = append(codes, f.Code)
		if f.Severity != "" {
			severities = append(severities, f.Severity)
		}
		if f.Confidence != "" {
			confidences = append(confidences, f.Confidence)
		}
		if f.Suppressed {
			ignored = append(ignored, f.Code)
		}
	}
	var suppression *Suppression
	if len(ignored) > 0 {
		suppression = &Suppression{Codes: unique(ignored), ReasonCode: "configured_scan_ignore"}
	}
	if scan != nil && !scan.Enabled {
		lim = append(lim, "Historical findings remain available independently of current scan enablement.")
	}
	facts := CheckFacts{FindingCodes: unique(codes), FindingSeverities: unique(severities), FindingConfidences: unique(confidences)}
	if len(codes) == 0 {
		item := check("pin.scan.findings."+name, PredPinScanFindings, serverSubject(name), OutcomeUnknown, "findings_absent_no_coverage", "No stored findings for "+name+" without scan time or ruleset; this is not a completed clean scan.", unknownEvidence(), lim, viewPinsAction(name))
		item.Suppression = suppression
		item.Facts = facts
		return item
	}
	ev := declaredEvidence()
	ev.Producer = "pins.scan"
	reason := "findings_present"
	text := "Stored advisory findings for " + name + ": " + strings.Join(unique(codes), ", ") + "."
	if len(ignored) == len(unique(codes)) {
		reason = "findings_present_suppressed"
		text = "Stored advisory findings for " + name + " are present and suppressed: " + strings.Join(unique(codes), ", ") + "."
	}
	item := check("pin.scan.findings."+name, PredPinScanFindings, serverSubject(name), OutcomeWarn, reason, text, ev, lim, viewPinsAction(name))
	item.Suppression = suppression
	item.Facts = facts
	return item
}

func skillPinChecks(in Inputs) []Check {
	var out []Check
	if in.SkillPins == nil {
		out = append(out, check("skill.pin.baseline.gateway", PredSkillPinBaseline, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Skill pin store snapshot unavailable.", unknownEvidence(), []string{"Skill pins are scoped to skill subjects only."}))
		out = append(out, check("skill.pin.continuity.gateway", PredSkillPinContinuity, gatewaySubject(), OutcomeUnknown, "store_unavailable", "Skill pin continuity cannot be established.", unknownEvidence(), nil))
		return out
	}
	if len(in.SkillPins.Skills) == 0 {
		out = append(out, check("skill.pin.baseline.gateway", PredSkillPinBaseline, gatewaySubject(), OutcomePass, "no_skill_pin_records", "No skill pin records are stored. That is a configuration fact, not a policy violation.", declaredEvidence(), []string{"Skill pins do not imply downstream server trust."}))
		out = append(out, check("skill.pin.continuity.gateway", PredSkillPinContinuity, gatewaySubject(), OutcomeNotApplicable, "no_skill_pin_records", "Skill pin continuity is not applicable without records.", declaredEvidence(), []string{"Not applicable because no skill pin records exist."}))
		return out
	}
	names := make([]string, 0, len(in.SkillPins.Skills))
	for name := range in.SkillPins.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rec := in.SkillPins.Skills[name]
		out = append(out, check("skill.pin.baseline."+name, PredSkillPinBaseline, skillSubject(name), OutcomePass, "skill_pin_present", "A stored skill pin exists for "+name+". Import provenance is not a trust level.", declaredEvidence(), []string{"Skill pins are scoped to skill subjects only."}, viewSkillPinsAction(name)))
		ev := declaredEvidence()
		if !rec.LastVerifiedAt.IsZero() {
			t := rec.LastVerifiedAt
			ev.VerifiedAt = &t
			ev.FreshnessCondition = "last_verified_at_is_not_scan_time"
		}
		if rec.Status == "drift" {
			out = append(out, check("skill.pin.continuity."+name, PredSkillPinContinuity, skillSubject(name), OutcomeFail, "skill_pin_drift", "Stored skill pin status is drift for "+name+".", ev, []string{"Known negative observation from the skill pin store."}, viewSkillPinsAction(name)))
		} else {
			out = append(out, check("skill.pin.continuity."+name, PredSkillPinContinuity, skillSubject(name), OutcomePass, "skill_pin_pinned", "Stored skill pin status is pinned for "+name+".", ev, nil, viewSkillPinsAction(name)))
		}
	}
	return out
}

func sourceChecks(in Inputs) []Check {
	var out []Check
	servers := serversOf(in)
	if len(servers) == 0 {
		for _, pred := range []string{PredSourceDeclared, PredSourceBuildDigest, PredSourceImageObserved, PredSourceManifest, PredSourceSignature} {
			out = append(out, check(pred+".gateway", pred, gatewaySubject(), OutcomeUnknown, "no_declared_servers", "No declared servers; source evidence is unknown.", unknownEvidence(), nil))
		}
		return out
	}
	for _, srv := range servers {
		out = append(out, declaredSourceCheck(srv))
		out = append(out, digestCheck(srv))
		out = append(out, imageObservedCheck(srv))
		out = append(out, manifestCheck(srv))
		out = append(out, signatureCheck(srv))
	}
	return out
}

func declaredSourceCheck(srv ServerView) Check {
	name := srv.Name
	lim := []string{"Declared source; running artifact verification unavailable."}
	if srv.Kind != "container" {
		item := check("source.declared."+name, PredSourceDeclared, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Container source identity is not applicable for "+srv.Kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects. This is not a claim that remote execution is secure."})
		item.Facts = CheckFacts{Kind: srv.Kind}
		return item
	}
	parts := []string{}
	if srv.SourcePackage != "" {
		parts = append(parts, srv.SourcePackage)
		if srv.SourceVersion != "" {
			parts = append(parts, srv.SourceVersion)
		}
	}
	if srv.SourceHostPath != "" {
		parts = append(parts, srv.SourceHostPath)
	}
	if srv.SourceRef != "" {
		parts = append(parts, srv.SourceRef)
	}
	if srv.Image != "" {
		parts = append(parts, srv.Image)
	}
	if len(parts) == 0 {
		return check("source.declared."+name, PredSourceDeclared, serverSubject(name), OutcomeUnknown, "source_undeclared", "Authored source identity for "+name+" is unavailable.", unknownEvidence(), lim)
	}
	identity := strings.Join(parts, " ")
	item := check("source.declared."+name, PredSourceDeclared, serverSubject(name), OutcomePass, "source_declared", "Declared source for "+name+": "+identity+".", declaredEvidence(), lim)
	item.Facts = CheckFacts{DeclaredSource: identity}
	return item
}

func digestCheck(srv ServerView) Check {
	name := srv.Name
	lim := []string{"A digest-shaped declaration is not authenticated verification.", "Build-input digest is not registry manifest identity."}
	if srv.Kind != "container" {
		return check("source.build_digest."+name, PredSourceBuildDigest, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Build-input digest is not applicable for "+srv.Kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects."})
	}
	if d := SanitizeDigest(srv.BuildDigest); d != "" {
		ev := declaredEvidence()
		ev.Digest = d
		return check("source.build_digest."+name, PredSourceBuildDigest, serverSubject(name), OutcomePass, "digest_declared", "Declared build-input digest is present for "+name+".", ev, lim)
	}
	return check("source.build_digest."+name, PredSourceBuildDigest, serverSubject(name), OutcomeUnknown, "digest_unavailable", "Build-input digest for "+name+" is unavailable.", unknownEvidence(), lim)
}

func imageObservedCheck(srv ServerView) Check {
	name := srv.Name
	lim := []string{"Observed image reference is not an immutable deployed snapshot of every replica."}
	if srv.Kind != "container" {
		return check("source.image_observed."+name, PredSourceImageObserved, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Observed image identity is not applicable for "+srv.Kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects."})
	}
	if img := SanitizeIdentifier(srv.ObservedImage); img != "" {
		ev := observedEvidence(time.Time{})
		item := check("source.image_observed."+name, PredSourceImageObserved, serverSubject(name), OutcomePass, "image_observed", "Observed image reference for "+name+": "+img+".", ev, lim)
		item.Facts = CheckFacts{DeclaredSource: img}
		return item
	}
	return check("source.image_observed."+name, PredSourceImageObserved, serverSubject(name), OutcomeUnknown, "image_unobserved", "Observed image reference for "+name+" is unavailable.", unknownEvidence(), lim)
}

func manifestCheck(srv ServerView) Check {
	name := srv.Name
	if srv.Kind != "container" {
		return check("source.manifest."+name, PredSourceManifest, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Registry manifest digest is not applicable for "+srv.Kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects."})
	}
	if d := SanitizeDigest(srv.ManifestDigest); d != "" {
		ev := observedEvidence(time.Time{})
		ev.Digest = d
		return check("source.manifest."+name, PredSourceManifest, serverSubject(name), OutcomePass, "manifest_present", "Registry manifest digest is present for "+name+".", ev, []string{"Manifest identity is producer-supplied."})
	}
	return check("source.manifest."+name, PredSourceManifest, serverSubject(name), OutcomeUnknown, "manifest_unavailable", "Registry manifest digest for "+name+" is unavailable.", unknownEvidence(), []string{"Image labels are source assertions, not proof of immutable running-artifact identity."})
}

func signatureCheck(srv ServerView) Check {
	name := srv.Name
	lim := []string{"Missing downstream signature evidence is unknown, not unsigned."}
	if srv.SignatureBound {
		ev := declaredEvidence()
		ev.Basis = BasisVerified
		ev.VerificationMethod = "bound_producer"
		ev.SubjectBinding = name
		ev.PredicateScope = PredSourceSignature
		item := check("source.signature."+name, PredSourceSignature, serverSubject(name), OutcomePass, "signature_bound", "A bound trusted producer supplied signature evidence for "+name+".", ev, lim)
		return item
	}
	return check("source.signature."+name, PredSourceSignature, serverSubject(name), OutcomeUnknown, "signature_unavailable", "Downstream signature evidence for "+name+" is unavailable.", unknownEvidence(), lim)
}

func varChecks(in Inputs) []Check {
	if in.Stack == nil {
		return []Check{
			check("var.completeness.gateway", PredVarCompleteness, gatewaySubject(), OutcomeUnknown, "stack_unavailable", "Variable completeness cannot be established without authored configuration.", unknownEvidence(), nil),
			check("var.references.gateway", PredVarReferences, gatewaySubject(), OutcomeUnknown, "stack_unavailable", "Variable references cannot be established without authored configuration.", unknownEvidence(), nil),
		}
	}
	lim := []string{"Nil set membership is unknown, not a known empty set.", "Without classification metadata this report names variables and references, not secret exposure."}
	complete := in.Stack.SetMembers != nil
	if complete {
		out := []Check{check("var.completeness.gateway", PredVarCompleteness, gatewaySubject(), OutcomePass, "membership_complete", "Variable set membership metadata is complete for this report.", declaredEvidence(), lim)}
		out = append(out, referenceCheck(in.Stack, true))
		return out
	}
	return []Check{
		check("var.completeness.gateway", PredVarCompleteness, gatewaySubject(), OutcomeUnknown, "membership_unknown", "Variable set membership is unknown; completeness stays partial.", unknownEvidence(), lim),
		referenceCheck(in.Stack, false),
	}
}

func referenceCheck(stack *StackView, complete bool) Check {
	sites := 0
	consumers := map[string]bool{}
	untargeted := 0
	for _, refs := range stack.References {
		sites += len(refs)
		for _, ref := range refs {
			if ref.Kind == "secrets-set" {
				if ref.Untargeted || ref.Target == "" {
					untargeted++
					continue
				}
				if isWorkloadKind(ref.TargetKind) && ref.Target != "" {
					consumers[ref.TargetKind+":"+ref.Target] = true
				}
				continue
			}
			if isWorkloadKind(ref.Kind) && ref.Name != "" {
				consumers[ref.Kind+":"+ref.Name] = true
			}
		}
	}
	if stack.UnscopedSetCount > untargeted {
		untargeted = stack.UnscopedSetCount
	}
	text := "Counted " + strconv.Itoa(sites) + " variable reference sites and " + strconv.Itoa(len(consumers)) + " distinct declared workload consumers"
	if untargeted > 0 {
		text += ", plus " + strconv.Itoa(untargeted) + " unscoped set consumers"
	}
	if !complete {
		text += "; membership is incomplete"
	}
	text += "."
	reason := "references_counted"
	if !complete {
		reason = "references_partial"
	}
	lim := []string{"Reference count and workload breadth are different units."}
	if !complete && untargeted > 0 {
		lim = append(lim, "Unscoped set expansion cannot name distinct workloads without membership.")
	}
	item := check("var.references.gateway", PredVarReferences, gatewaySubject(), OutcomePass, reason, text, declaredEvidence(), lim)
	item.Facts = CheckFacts{
		ReferenceSites:    intPtr(sites),
		WorkloadConsumers: intPtr(len(consumers)),
		UnscopedConsumers: intPtr(untargeted),
	}
	return item
}

func isWorkloadKind(kind string) bool {
	return kind == "mcp-server" || kind == "resource"
}

func gatewayChecks(in Inputs) []Check {
	var out []Check
	if in.Stack == nil || in.Stack.Gateway == nil {
		out = append(out, check("gateway.auth.declared.gateway", PredGatewayAuthDeclared, gatewaySubject(), OutcomeUnknown, "declaration_unavailable", "Gateway auth and bind declarations are unavailable.", unknownEvidence(), []string{"A saved file does not prove route enforcement."}))
	} else {
		g := in.Stack.Gateway
		text := "Gateway auth is not declared."
		reason := "auth_undeclared"
		if g.AuthDeclared {
			text = "Gateway auth is declared"
			if g.AuthType != "" {
				text += " as " + g.AuthType
			}
			text += ". Declaration is not route enforcement."
			reason = "auth_declared"
		}
		if g.Bind != "" {
			text += " Declared bind " + g.Bind + "."
		}
		item := check("gateway.auth.declared.gateway", PredGatewayAuthDeclared, gatewaySubject(), OutcomePass, reason, text, declaredEvidence(), []string{"A saved token presence does not prove route enforcement."})
		item.Facts = CheckFacts{AuthType: g.AuthType, Bind: g.Bind}
		out = append(out, item)
	}
	if in.Startup == nil {
		out = append(out, check("gateway.auth.startup.gateway", PredGatewayAuthStartup, gatewaySubject(), OutcomeUnknown, "startup_unavailable", "Active startup security snapshot is unavailable.", unknownEvidence(), []string{"Only declarations exist; enforcement remains unverified."}))
		return out
	}
	text := "Startup snapshot reports authentication disabled."
	reason := "startup_auth_disabled"
	if in.Startup.AuthEnabled {
		text = "Startup snapshot reports authentication enabled"
		if in.Startup.AuthType != "" {
			text += " as " + SanitizeIdentifier(in.Startup.AuthType)
		}
		text += "."
		reason = "startup_auth_enabled"
	}
	if in.Startup.Bind != "" {
		text += " Effective bind " + SanitizeIdentifier(in.Startup.Bind) + "."
	}
	ev := observedEvidence(time.Time{})
	item := check("gateway.auth.startup.gateway", PredGatewayAuthStartup, gatewaySubject(), OutcomePass, reason, text, ev, []string{"Startup snapshot is not a proof of route correctness."})
	item.Facts = CheckFacts{AuthType: SanitizeIdentifier(in.Startup.AuthType), EffectiveBind: SanitizeIdentifier(in.Startup.Bind)}
	out = append(out, item)
	return out
}

func executionChecks(in Inputs) []Check {
	var out []Check
	byServer := map[string][]ExecutionView{}
	for _, ev := range in.Execution {
		byServer[ev.Server] = append(byServer[ev.Server], ev)
	}
	servers := serversOf(in)
	if len(servers) == 0 && len(byServer) == 0 {
		out = append(out, check("execution.availability.gateway", PredExecutionAvailability, gatewaySubject(), OutcomeUnknown, "execution_unavailable", "Per-replica execution evidence is unavailable.", unknownEvidence(), []string{"Missing current control evidence is unknown."}))
		out = append(out, check("execution.enforcement.gateway", PredExecutionEnforcement, gatewaySubject(), OutcomeUnknown, "execution_unavailable", "Execution enforcement evidence is unavailable.", unknownEvidence(), []string{"A generated nonroot image does not substitute for verified runtime controls."}))
		return out
	}
	names := serverNames(in)
	if len(names) == 0 {
		for name := range byServer {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		kind := "container"
		for _, srv := range servers {
			if srv.Name == name {
				kind = srv.Kind
				break
			}
		}
		replicas := byServer[name]
		if kind == "openapi" || kind == "external" || kind == "ssh" {
			out = append(out, check("execution.availability."+name, PredExecutionAvailability, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Container execution evidence is not applicable for "+kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects. This is not a claim that remote execution is secure."}))
			out = append(out, check("execution.enforcement."+name, PredExecutionEnforcement, serverSubject(name), OutcomeNotApplicable, "noncontainer_subject", "Container execution enforcement is not applicable for "+kind+" subjects.", declaredEvidence(), []string{"Not applicable for non-container subjects."}))
			continue
		}
		if len(replicas) == 0 {
			out = append(out, check("execution.availability."+name, PredExecutionAvailability, serverSubject(name), OutcomeUnknown, "execution_unavailable", "Per-replica execution evidence for "+name+" is unavailable.", unknownEvidence(), []string{"Missing current control evidence is unknown."}))
			out = append(out, check("execution.enforcement."+name, PredExecutionEnforcement, serverSubject(name), OutcomeUnknown, "execution_unavailable", "Execution enforcement evidence for "+name+" is unavailable.", unknownEvidence(), []string{"A source label or digest-shaped declaration does not substitute for verified runtime controls."}))
			continue
		}
		out = append(out, check("execution.availability."+name, PredExecutionAvailability, serverSubject(name), OutcomePass, "execution_available", "Per-replica execution evidence is available for "+name+".", observedEvidence(replicas[0].ObservedAt), []string{"This is a launch/admission snapshot, not continuous protection."}))
		for _, replica := range replicas {
			out = append(out, enforcementCheck(name, replica))
		}
	}
	return out
}

func enforcementCheck(server string, replica ExecutionView) Check {
	subject := replicaSubject(server, replica.Replica)
	id := "execution.enforcement." + subject.Name
	ev := observedEvidence(replica.ObservedAt)
	ev.Producer = SanitizeIdentifier(replica.Runtime)
	if replica.Instance != "" {
		ev.SubjectBinding = SanitizeIdentifier(replica.Instance)
	}
	lim := []string{"Observed execution evidence is not continuous monitoring.", "A generated nonroot image does not substitute for verified runtime controls."}
	facts := CheckFacts{
		Kind:            SanitizeIdentifier(replica.Kind),
		RecordedOutcome: SanitizeIdentifier(replica.Outcome),
		Instance:        SanitizeIdentifier(replica.Instance),
		Revision:        SanitizeIdentifier(replica.Revision),
	}
	withFacts := func(item Check) Check {
		item.Facts = facts
		return item
	}
	switch replica.Outcome {
	case "failed", "refused", "ineligible":
		return withFacts(check(id, PredExecutionEnforcement, subject, OutcomeFail, "execution_failed", "Recorded execution outcome for "+subject.Name+" is "+SanitizeIdentifier(replica.Outcome)+".", ev, lim))
	case "observed":
		if replica.Eligible {
			return withFacts(check(id, PredExecutionEnforcement, subject, OutcomePass, "execution_observed", "Recorded execution outcome for "+subject.Name+" is observed and eligible.", ev, lim))
		}
		return withFacts(check(id, PredExecutionEnforcement, subject, OutcomeFail, "execution_ineligible", "Recorded execution outcome for "+subject.Name+" is observed but not eligible.", ev, lim))
	}
	if replica.Kind == "local-process" || replica.Mode == "local" || replica.Mode == "unsandboxed-local" {
		return withFacts(check(id, PredExecutionEnforcement, subject, OutcomePass, "execution_hygiene", "Local process hygiene is configured for "+subject.Name+". Environment hygiene does not confine filesystem or network access.", ev, lim))
	}
	if replica.Eligible {
		return withFacts(check(id, PredExecutionEnforcement, subject, OutcomePass, "execution_eligible", "Recorded execution evidence for "+subject.Name+" is eligible.", ev, lim))
	}
	return withFacts(check(id, PredExecutionEnforcement, subject, OutcomeUnknown, "execution_outcome_unknown", "Execution enforcement outcome for "+subject.Name+" is unknown.", unknownEvidence(), lim))
}

func serverNames(in Inputs) []string {
	seen := map[string]bool{}
	var names []string
	for _, srv := range serversOf(in) {
		if srv.Name == "" || seen[srv.Name] {
			continue
		}
		seen[srv.Name] = true
		names = append(names, srv.Name)
	}
	if in.Pins != nil {
		for name := range in.Pins.Servers {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func serversOf(in Inputs) []ServerView {
	if in.Stack == nil {
		return nil
	}
	return in.Stack.Servers
}

func unique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = SanitizeIdentifier(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func sanitizeReport(report *Report, untrusted bool) *Report {
	if report == nil {
		return nil
	}
	out := *report
	out.SchemaVersion = SchemaVersion
	out.Source.Kind = SanitizeIdentifier(out.Source.Kind)
	out.Source.Display = SanitizeIdentifier(out.Source.Display)
	out.Source.StackName = SanitizeIdentifier(out.Source.StackName)
	out.Checks = make([]Check, 0, len(report.Checks))
	for _, c := range report.Checks {
		item := c
		item.ID = SanitizeIdentifier(c.ID)
		item.Predicate = SanitizeIdentifier(c.Predicate)
		item.Subject.Kind = SanitizeIdentifier(c.Subject.Kind)
		item.Subject.Name = SanitizeIdentifier(c.Subject.Name)
		item.Outcome = SanitizeIdentifier(c.Outcome)
		item.ReasonCode = SanitizeIdentifier(c.ReasonCode)
		item.Facts = sanitizeFacts(c.Facts)
		item.Evidence.Basis = SanitizeIdentifier(c.Evidence.Basis)
		item.Evidence.Availability = SanitizeIdentifier(c.Evidence.Availability)
		item.Evidence.Freshness = SanitizeIdentifier(c.Evidence.Freshness)
		item.Evidence.FreshnessCondition = SanitizeIdentifier(c.Evidence.FreshnessCondition)
		item.Evidence.Producer = SanitizeIdentifier(c.Evidence.Producer)
		item.Evidence.ProducerVersion = SanitizeIdentifier(c.Evidence.ProducerVersion)
		item.Evidence.Ruleset = SanitizeIdentifier(c.Evidence.Ruleset)
		item.Evidence.Digest = SanitizeDigest(c.Evidence.Digest)
		item.Evidence.VerificationMethod = SanitizeIdentifier(c.Evidence.VerificationMethod)
		item.Evidence.SubjectBinding = SanitizeIdentifier(c.Evidence.SubjectBinding)
		item.Evidence.PredicateScope = SanitizeIdentifier(c.Evidence.PredicateScope)
		item.Evidence.ObservedAt = copyTime(c.Evidence.ObservedAt)
		item.Evidence.VerifiedAt = copyTime(c.Evidence.VerifiedAt)
		item.Evidence.ScannedAt = copyTime(c.Evidence.ScannedAt)
		if item.Evidence.Basis == BasisVerified && (item.Evidence.VerificationMethod == "" || item.Evidence.SubjectBinding == "" || item.Evidence.PredicateScope == "") {
			item.Evidence.Basis = BasisDeclared
			item.Evidence.VerificationMethod = ""
			item.Evidence.SubjectBinding = ""
			item.Evidence.PredicateScope = ""
		}
		if untrusted && item.Evidence.Basis == BasisVerified {
			item.Evidence.Basis = BasisDeclared
			if item.ReasonCode == "signature_bound" {
				item.ReasonCode = "signature_claimed"
			}
		}
		if (item.Evidence.Freshness == FreshnessCurrent || item.Evidence.Freshness == FreshnessStale) && item.Evidence.FreshnessCondition == "" {
			item.Evidence.Freshness = FreshnessUnknown
		}
		if untrusted && item.Evidence.Freshness == FreshnessCurrent {
			item.Evidence.Freshness = FreshnessUnknown
			if item.Evidence.FreshnessCondition == "" {
				item.Evidence.FreshnessCondition = "imported_snapshot_is_historical"
			}
		}
		item.Explanation = explanationFor(item.ReasonCode, item.Subject.Name, item.Facts)
		item.Actions = nil
		for _, action := range c.Actions {
			if action.ID == ActionViewPins && action.Path != "" && strings.HasPrefix(action.Path, "/pins?") {
				if rebuilt := viewPinsAction(item.Subject.Name); rebuilt != nil {
					item.Actions = append(item.Actions, *rebuilt)
				}
				continue
			}
			if action.ID == ActionViewSkillPins && strings.HasPrefix(action.Path, "/pins?") {
				if rebuilt := viewSkillPinsAction(item.Subject.Name); rebuilt != nil {
					item.Actions = append(item.Actions, *rebuilt)
				}
			}
		}
		item.Limitations = retainLimitations(c.Limitations)
		if untrusted {
			item.Limitations = retainLimitations(append(item.Limitations, "Imported snapshots cannot authenticate verification claims."))
		}
		if c.Suppression != nil {
			item.Suppression = &Suppression{ReasonCode: SanitizeIdentifier(c.Suppression.ReasonCode), Codes: unique(c.Suppression.Codes)}
		}
		out.Checks = append(out.Checks, item)
	}
	out.Limitations = defaultLimitations(out.Source.Historical || untrusted)
	summarize(&out)
	out.GeneratedAt = report.GeneratedAt.UTC()
	out.GenerationOK = true
	return &out
}

func ExitCode(report *Report) int {
	if report == nil || !report.GenerationOK {
		return 2
	}
	if report.FailCount > 0 {
		return 1
	}
	return 0
}
