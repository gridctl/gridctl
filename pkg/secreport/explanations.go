package secreport

func explanationFor(reason, subject string) string {
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
		return "Stored advisory findings are present for " + name + "."
	case "no_skill_pin_records":
		return "No skill pin records are stored. That is a configuration fact, not a policy violation."
	case "skill_pin_present":
		return "A stored skill pin exists for " + name + ". Import provenance is not a trust level."
	case "skill_pin_drift":
		return "Stored skill pin status is drift for " + name + "."
	case "skill_pin_pinned":
		return "Stored skill pin status is pinned for " + name + "."
	case "noncontainer_subject":
		return "Container-specific evidence is not applicable for this subject."
	case "source_undeclared":
		return "Authored source identity for " + name + " is unavailable."
	case "source_declared":
		return "Declared source identity is present for " + name + "."
	case "digest_declared":
		return "Declared build-input digest is present for " + name + "."
	case "digest_unavailable":
		return "Build-input digest for " + name + " is unavailable."
	case "image_observed":
		return "Observed image reference is present for " + name + "."
	case "image_unobserved":
		return "Observed image reference for " + name + " is unavailable."
	case "manifest_present":
		return "Registry manifest digest is present for " + name + "."
	case "manifest_unavailable":
		return "Registry manifest digest for " + name + " is unavailable."
	case "signature_bound":
		return "A bound trusted producer supplied signature evidence for " + name + "."
	case "signature_unavailable":
		return "Downstream signature evidence for " + name + " is unavailable."
	case "stack_unavailable":
		return "Authored configuration is unavailable."
	case "membership_complete":
		return "Variable set membership metadata is complete for this report."
	case "membership_unknown":
		return "Variable set membership is unknown; completeness stays partial."
	case "references_counted", "references_partial":
		return "Variable reference sites and declared consumers were counted without resolving values."
	case "declaration_unavailable":
		return "Gateway auth and bind declarations are unavailable."
	case "auth_undeclared":
		return "Gateway auth is not declared."
	case "auth_declared":
		return "Gateway auth is declared. Declaration is not route enforcement."
	case "startup_unavailable":
		return "Active startup security snapshot is unavailable."
	case "startup_auth_disabled":
		return "Startup snapshot reports authentication disabled."
	case "startup_auth_enabled":
		return "Startup snapshot reports authentication enabled."
	case "execution_unavailable":
		return "Per-replica execution evidence is unavailable."
	case "execution_available":
		return "Per-replica execution evidence is available for " + name + "."
	case "execution_hygiene":
		return "Local process hygiene is configured. Environment hygiene does not confine filesystem or network access."
	case "execution_failed":
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
		return "See reason code " + SanitizeIdentifier(reason) + "."
	}
}
