package secreport

import "strconv"

func explanationFor(reason, subject string, facts CheckFacts) string {
	name := SanitizeIdentifier(subject)
	switch reason {
	case "store_unavailable":
		return "Required store snapshot is unavailable."
	case "store_available":
		return "Schema pin store snapshot is available. Records are not explicit approvals."
	case "store_disabled":
		return "Schema pinning is declared disabled. Absence of pins is a configuration fact, not a policy violation."
	case "no_declared_servers":
		return "No declared MCP servers."
	case "pin_record_absent":
		return "No pin record is stored for " + name + ". That is a configuration fact, not a policy violation."
	case "pin_record_present":
		return "A stored pin record exists for " + name + ". Presence is not explicit approval."
	case "no_pin_record":
		return "Continuity is not applicable without a stored pin record."
	case "pin_status_drift":
		return "Stored pin status is drift for " + name + "."
	case "pin_status_pending_redeploy":
		return "Stored pin status is approved pending redeploy for " + name + "."
	case "pin_status_pinned":
		return "Stored pin status is pinned for " + name + ". Continuity is recorded definition matching, not publisher trust."
	case "scheme_mixed":
		return "Pin records for " + name + " mix current and legacy hash schemes."
	case "scheme_legacy":
		return "Pin records for " + name + " use the legacy hash scheme."
	case "scheme_current":
		return "Pin records for " + name + " use the current hash scheme."
	case "scheme_unknown":
		return "Pin hash scheme coverage for " + name + " is unknown."
	case "scan_config_unknown":
		return "Advisory scan configuration is unknown."
	case "scan_disabled":
		return "Advisory scanning is declared disabled."
	case "scan_time_unknown":
		return "Scan is declared enabled; scan time and ruleset are unknown."
	case "scan_config_known":
		return "Advisory scanning is declared enabled. Stored findings are historical."
	case "findings_unavailable":
		return "Stored advisory findings are unavailable without a pin record."
	case "findings_absent_no_coverage":
		return "No stored findings for " + name + " without scan time or ruleset; this is not a completed clean scan."
	case "findings_present":
		if len(facts.FindingCodes) > 0 {
			return "Stored advisory findings for " + name + ": " + joinCodes(facts.FindingCodes) + "."
		}
		return "Stored advisory findings are present for " + name + "."
	case "findings_present_suppressed":
		if len(facts.FindingCodes) > 0 {
			return "Stored advisory findings for " + name + " are present and suppressed: " + joinCodes(facts.FindingCodes) + "."
		}
		return "Stored advisory findings for " + name + " are present and suppressed."
	case "no_skill_pin_records":
		return "No skill pin records are stored. That is a configuration fact, not a policy violation."
	case "skill_pin_present":
		return "A stored skill pin exists for " + name + ". Import provenance is not a trust level."
	case "skill_pin_drift":
		return "Stored skill pin status is drift for " + name + "."
	case "skill_pin_pinned":
		return "Stored skill pin status is pinned for " + name + "."
	case "noncontainer_subject":
		if facts.Kind != "" {
			return "Container-specific evidence is not applicable for " + facts.Kind + " subjects."
		}
		return "Container-specific evidence is not applicable for this subject."
	case "source_undeclared":
		return "Authored source identity for " + name + " is unavailable."
	case "source_declared":
		if facts.DeclaredSource != "" {
			return "Declared source for " + name + ": " + facts.DeclaredSource + "."
		}
		return "Declared source identity is present for " + name + "."
	case "digest_declared":
		return "Declared build-input digest is present for " + name + "."
	case "digest_unavailable":
		return "Build-input digest for " + name + " is unavailable."
	case "image_observed":
		if facts.DeclaredSource != "" {
			return "Observed image reference for " + name + ": " + facts.DeclaredSource + "."
		}
		return "Observed image reference is present for " + name + "."
	case "image_unobserved":
		return "Observed image reference for " + name + " is unavailable."
	case "manifest_present":
		return "Registry manifest digest is present for " + name + "."
	case "manifest_unavailable":
		return "Registry manifest digest for " + name + " is unavailable."
	case "signature_bound":
		return "A bound trusted producer supplied signature evidence for " + name + "."
	case "signature_claimed":
		return "Snapshot claims signature evidence for " + name + "; this is not authenticated verification."
	case "signature_unavailable":
		return "Downstream signature evidence for " + name + " is unavailable."
	case "stack_unavailable":
		return "Authored configuration is unavailable."
	case "membership_complete":
		return "Variable set membership metadata is complete for this report."
	case "membership_unknown":
		return "Variable set membership is unknown; completeness stays partial."
	case "references_counted", "references_partial":
		if facts.ReferenceSites != nil && facts.WorkloadConsumers != nil {
			text := "Counted " + strconv.Itoa(*facts.ReferenceSites) + " variable reference sites and " + strconv.Itoa(*facts.WorkloadConsumers) + " distinct declared workload consumers"
			if facts.UnscopedConsumers != nil && *facts.UnscopedConsumers > 0 {
				text += ", plus " + strconv.Itoa(*facts.UnscopedConsumers) + " unscoped set consumers"
			}
			if reason == "references_partial" {
				text += "; membership is incomplete"
			}
			return text + "."
		}
		return "Variable reference sites and declared consumers were counted without resolving values."
	case "declaration_unavailable":
		return "Gateway auth and bind declarations are unavailable."
	case "auth_undeclared":
		text := "Gateway auth is not declared."
		if facts.Bind != "" {
			text += " Declared bind " + facts.Bind + "."
		}
		return text
	case "auth_declared":
		text := "Gateway auth is declared"
		if facts.AuthType != "" {
			text += " as " + facts.AuthType
		}
		text += ". Declaration is not route enforcement."
		if facts.Bind != "" {
			text += " Declared bind " + facts.Bind + "."
		}
		return text
	case "startup_unavailable":
		return "Active startup security snapshot is unavailable."
	case "startup_auth_disabled":
		text := "Startup snapshot reports authentication disabled."
		if facts.EffectiveBind != "" {
			text += " Effective bind " + facts.EffectiveBind + "."
		}
		return text
	case "startup_auth_enabled":
		text := "Startup snapshot reports authentication enabled"
		if facts.AuthType != "" {
			text += " as " + facts.AuthType
		}
		text += "."
		if facts.EffectiveBind != "" {
			text += " Effective bind " + facts.EffectiveBind + "."
		}
		return text
	case "execution_unavailable":
		return "Per-replica execution evidence is unavailable."
	case "execution_available":
		return "Per-replica execution evidence is available for " + name + "."
	case "execution_hygiene":
		return "Local process hygiene is configured. Environment hygiene does not confine filesystem or network access."
	case "execution_failed":
		if facts.RecordedOutcome != "" {
			return "Recorded execution outcome for " + name + " is " + facts.RecordedOutcome + "."
		}
		return "Recorded execution outcome is a known failure for " + name + "."
	case "execution_observed":
		return "Recorded execution outcome is observed and eligible for " + name + "."
	case "execution_ineligible":
		return "Recorded execution outcome is observed but not eligible for " + name + "."
	case "execution_eligible":
		return "Recorded execution evidence is eligible for " + name + "."
	case "execution_outcome_unknown":
		return "Execution enforcement outcome for " + name + " is unknown."
	default:
		return "Unrecognized claim."
	}
}

func joinCodes(codes []string) string {
	out := ""
	for i, c := range codes {
		if c == "" {
			continue
		}
		if i > 0 && out != "" {
			out += ", "
		}
		out += c
	}
	return out
}

func knownReasonCode(reason string) bool {
	_, ok := knownReasons[reason]
	return ok
}

var knownReasons = map[string]bool{
	"store_unavailable": true, "store_available": true, "store_disabled": true,
	"no_declared_servers": true, "pin_record_absent": true, "pin_record_present": true,
	"no_pin_record": true, "pin_status_drift": true, "pin_status_pending_redeploy": true,
	"pin_status_pinned": true, "scheme_mixed": true, "scheme_legacy": true,
	"scheme_current": true, "scheme_unknown": true, "scan_config_unknown": true,
	"scan_disabled": true, "scan_time_unknown": true, "scan_config_known": true,
	"findings_unavailable": true, "findings_absent_no_coverage": true, "findings_present": true,
	"findings_present_suppressed": true, "no_skill_pin_records": true, "skill_pin_present": true,
	"skill_pin_drift": true, "skill_pin_pinned": true, "noncontainer_subject": true,
	"source_undeclared": true, "source_declared": true, "digest_declared": true,
	"digest_unavailable": true, "image_observed": true, "image_unobserved": true,
	"manifest_present": true, "manifest_unavailable": true, "signature_bound": true,
	"signature_claimed": true, "signature_unavailable": true, "stack_unavailable": true,
	"membership_complete": true, "membership_unknown": true, "references_counted": true,
	"references_partial": true, "declaration_unavailable": true, "auth_undeclared": true,
	"auth_declared": true, "startup_unavailable": true, "startup_auth_disabled": true,
	"startup_auth_enabled": true, "execution_unavailable": true, "execution_available": true,
	"execution_hygiene": true, "execution_failed": true, "execution_observed": true,
	"execution_ineligible": true, "execution_eligible": true, "execution_outcome_unknown": true,
}
