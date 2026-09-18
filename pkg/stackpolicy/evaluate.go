package stackpolicy

import (
	"context"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/depcheck"
)

const (
	reasonDigest                 = "digest"
	reasonMutableTag             = "mutable-tag"
	reasonUnresolvedImage        = "unresolved-image"
	reasonInvalidImage           = "invalid-image"
	reasonSourceBuilt            = "source-built"
	reasonNonImage               = "non-image"
	reasonNotMCPServer           = "not-mcp-server"
	reasonLocalCommand           = "local-command"
	reasonNotLocalCommand        = "not-local-command"
	reasonSSHServer              = "ssh-server"
	reasonNotSSHServer           = "not-ssh-server"
	reasonPinningEnabled         = "pinning-enabled"
	reasonPinningDisabled        = "pinning-disabled"
	reasonPinningBlock           = "pinning-block"
	reasonPinningNotBlock        = "pinning-not-block"
	reasonUnknownApplicability   = "unknown-applicability"
	reasonUncertainMembership    = "uncertain-membership"
	reasonNonemptyTools          = "nonempty-tools"
	reasonEmptyTools             = "empty-tools"
	reasonUnresolvedToolName     = "unresolved-tool-name"
	reasonEmptyToolName          = "empty-tool-name"
	reasonMalformedServer        = "malformed-server"
)

// Evaluate captures the selected policy and candidate stack graph, then
// reports per-subject outcomes for the enabled rules. It always returns a
// report; callers should use ExitCode and FormatJSON or FormatText.
func Evaluate(ctx context.Context, stackPath, policyPath string) *Report {
	report := newReport()
	if err := ctx.Err(); err != nil {
		attachPolicyError(report, err)
		report.finalize()
		return report
	}
	policy, err := readPolicy(ctx, policyPath)
	if policy != nil {
		report.Policy.Digest = policy.Digest
		report.Policy.Version = policy.Version
		if policy.Enabled != nil {
			report.Policy.Enabled = append([]string{}, policy.Enabled...)
		}
	}
	if err != nil {
		attachPolicyError(report, err)
		report.finalize()
		return report
	}
	if !policy.HasIdent {
		attachPolicyError(report, codeErr("policy-identity"))
		report.finalize()
		return report
	}
	snap, err := collectCandidate(ctx, stackPath, policy.Ident, true)
	if err != nil {
		attachInputError(report, err, "entry")
		report.finalize()
		return report
	}
	declared, diags := interpretSnapshot(snap)
	if len(diags) > 0 {
		report.Diagnostics = append(report.Diagnostics, diags...)
		report.finalize()
		return report
	}
	evaluateRules(report, declared)
	report.EvaluationComplete = true
	report.finalize()
	return report
}

func evaluateRules(report *Report, stack *declaredStack) {
	for _, rule := range report.Policy.Enabled {
		switch rule {
		case RuleExplicitImageDigests:
			evalExplicitImageDigests(report, stack)
		case RuleDenyLocalCommandServers:
			evalDenyLocal(report, stack)
		case RuleDenySSHServers:
			evalDenySSH(report, stack)
		case RuleSchemaPinningEnabled:
			evalPinningEnabled(report, stack)
		case RuleSchemaPinningBlock:
			evalPinningBlock(report, stack)
		case RuleNonemptyServerToolLists:
			evalToolLists(report, stack)
		}
	}
}

func evalExplicitImageDigests(report *Report, stack *declaredStack) {
	if stack.membershipUnknown {
		report.Results = append(report.Results, Result{
			Rule:        RuleExplicitImageDigests,
			Outcome:     OutcomeUnknown,
			Location:    Location{Source: "entry", Path: "mcp-servers"},
			ReasonCode:  reasonUncertainMembership,
			Message:     "Effective image membership cannot be determined.",
			Remediation: "Use literal names so inheritance and overrides are determinate.",
		})
		return
	}
	excluded := map[string]int{}
	for _, srv := range stack.servers {
		if srv.kind == kindMalformed && !srv.kindUnknown {
			report.addDiagnostic(reasonMalformedServer, srv.loc.Source, srv.loc.Path, diagnosticMessage("malformed-server-kind"))
			continue
		}
		if srv.imagePresent || srv.imageDynamic || srv.kind == kindImage {
			loc := srv.imageLoc
			if loc.Path == "" {
				loc = srv.loc
				loc.Path = srv.loc.Path + ".image"
			}
			evalImageField(report, loc, srv.image, srv.imagePresent, srv.imageDynamic, srv.imageInvalid)
			continue
		}
		if srv.kindUnknown {
			report.Results = append(report.Results, unknownResult(RuleExplicitImageDigests, srv.loc, reasonUnknownApplicability, "Image applicability cannot be determined for this server.", "Declare a single supported server kind with a literal image or an excluded category."))
			continue
		}
		switch srv.kind {
		case kindSource:
			excluded[reasonSourceBuilt]++
			report.Results = append(report.Results, naResult(RuleExplicitImageDigests, srv.loc, reasonSourceBuilt, "Source-built servers are excluded from explicit image digest checks."))
		case kindLocal, kindSSH, kindURL, kindOpenAPI, kindA2A:
			excluded[reasonNonImage]++
			report.Results = append(report.Results, naResult(RuleExplicitImageDigests, srv.loc, reasonNonImage, "This server kind has no explicit image field."))
		default:
			report.Results = append(report.Results, unknownResult(RuleExplicitImageDigests, srv.loc, reasonUnknownApplicability, "Image applicability cannot be determined for this server.", "Declare a single supported server kind with a literal image or an excluded category."))
		}
	}
	for _, res := range stack.resources {
		loc := res.imageLoc
		if loc.Path == "" {
			loc = res.loc
			loc.Path = res.loc.Path + ".image"
		}
		if res.imageInvalid && !res.imageDynamic {
			report.addDiagnostic("malformed-resource", res.loc.Source, res.loc.Path, diagnosticMessage("malformed-resource"))
			continue
		}
		if !res.imagePresent && !res.imageDynamic {
			report.addDiagnostic("malformed-resource", res.loc.Source, res.loc.Path, diagnosticMessage("malformed-resource"))
			continue
		}
		evalImageField(report, loc, res.image, res.imagePresent, res.imageDynamic, res.imageInvalid)
	}
	for reason, count := range excluded {
		report.Coverage.Exclusions = append(report.Coverage.Exclusions, Exclusion{
			Rule:       RuleExplicitImageDigests,
			ReasonCode: reason,
			Count:      count,
		})
	}
}

func evalImageField(report *Report, loc Location, image string, present, dynamic, invalid bool) {
	if invalid {
		report.addDiagnostic("invalid-image", loc.Source, loc.Path, "Image declaration is not a string.")
		return
	}
	if !present {
		report.Results = append(report.Results, Result{
			Rule:        RuleExplicitImageDigests,
			Outcome:     OutcomeViolation,
			Location:    loc,
			ReasonCode:  reasonMutableTag,
			Message:     "Explicit image field is missing a digest-qualified reference.",
			Remediation: "Provide a literal digest-qualified declaration for this check.",
		})
		return
	}
	if dynamic || config.ContainsExpansion(image) {
		report.Results = append(report.Results, unknownResult(RuleExplicitImageDigests, loc, reasonUnresolvedImage, "Policy-relevant image reference is unresolved.", "Provide a literal digest-qualified declaration for this check."))
		return
	}
	classified := depcheck.ClassifyImage(image)
	switch classified.Status {
	case depcheck.StatusPinned:
		report.Results = append(report.Results, Result{
			Rule:       RuleExplicitImageDigests,
			Outcome:    OutcomePass,
			Location:   loc,
			ReasonCode: reasonDigest,
			Message:    "Explicit image is a syntactically valid digest reference.",
		})
	case depcheck.StatusMutable:
		report.Results = append(report.Results, Result{
			Rule:        RuleExplicitImageDigests,
			Outcome:     OutcomeViolation,
			Location:    loc,
			ReasonCode:  reasonMutableTag,
			Message:     "Explicit image is not a digest-qualified reference.",
			Remediation: "Provide a literal digest-qualified declaration for this check.",
		})
	default:
		switch classified.Reason {
		case "variable":
			report.Results = append(report.Results, unknownResult(RuleExplicitImageDigests, loc, reasonUnresolvedImage, "Policy-relevant image reference is unresolved.", "Provide a literal digest-qualified declaration for this check."))
		case "empty":
			report.Results = append(report.Results, Result{
				Rule:        RuleExplicitImageDigests,
				Outcome:     OutcomeViolation,
				Location:    loc,
				ReasonCode:  reasonMutableTag,
				Message:     "Explicit image field is missing a digest-qualified reference.",
				Remediation: "Provide a literal digest-qualified declaration for this check.",
			})
		default:
			report.addDiagnostic("invalid-image", loc.Source, loc.Path, "Image declaration is not a supported digest reference.")
		}
	}
}

func evalDenyLocal(report *Report, stack *declaredStack) {
	if stack.membershipUnknown {
		report.Results = append(report.Results, unknownResult(RuleDenyLocalCommandServers, Location{Source: "entry", Path: "mcp-servers"}, reasonUncertainMembership, "Effective server membership cannot be determined.", "Use literal names so inheritance and overrides are determinate."))
		return
	}
	for _, srv := range stack.servers {
		evalKindProhibition(report, stack, srv, RuleDenyLocalCommandServers, kindLocal, reasonLocalCommand, reasonNotLocalCommand, "Command-only local MCP execution is not allowed.", "Use a container image, source build, or remote transport instead.")
	}
	for _, res := range stack.resources {
		report.Results = append(report.Results, naResult(RuleDenyLocalCommandServers, res.loc, reasonNotMCPServer, "Supporting resources are not MCP servers."))
	}
}

func evalDenySSH(report *Report, stack *declaredStack) {
	if stack.membershipUnknown {
		report.Results = append(report.Results, unknownResult(RuleDenySSHServers, Location{Source: "entry", Path: "mcp-servers"}, reasonUncertainMembership, "Effective server membership cannot be determined.", "Use literal names so inheritance and overrides are determinate."))
		return
	}
	for _, srv := range stack.servers {
		evalKindProhibition(report, stack, srv, RuleDenySSHServers, kindSSH, reasonSSHServer, reasonNotSSHServer, "Declared SSH execution is not allowed.", "Remove the SSH server declaration or use a different transport.")
	}
	for _, res := range stack.resources {
		report.Results = append(report.Results, naResult(RuleDenySSHServers, res.loc, reasonNotMCPServer, "Supporting resources are not MCP servers."))
	}
}

func evalKindProhibition(report *Report, _ *declaredStack, srv mcpSubject, rule string, forbidden serverKind, violateCode, passCode, violateMsg, remediation string) {
	if srv.kind == kindMalformed && !srv.kindUnknown {
		report.addDiagnostic(reasonMalformedServer, srv.loc.Source, srv.loc.Path, diagnosticMessage("malformed-server-kind"))
		return
	}
	if srv.kindUnknown {
		report.Results = append(report.Results, unknownResult(rule, srv.loc, reasonUnknownApplicability, "Server kind cannot be determined.", "Declare a single supported server kind."))
		return
	}
	if srv.kind == forbidden {
		report.Results = append(report.Results, Result{
			Rule:        rule,
			Outcome:     OutcomeViolation,
			Location:    srv.loc,
			ReasonCode:  violateCode,
			Message:     violateMsg,
			Remediation: remediation,
		})
		return
	}
	report.Results = append(report.Results, Result{
		Rule:       rule,
		Outcome:    OutcomePass,
		Location:   srv.loc,
		ReasonCode: passCode,
		Message:    "This server is outside the prohibited execution class.",
	})
}

func evalPinningEnabled(report *Report, stack *declaredStack) {
	evalPinning(report, stack, RuleSchemaPinningEnabled, false)
}

func evalPinningBlock(report *Report, stack *declaredStack) {
	evalPinning(report, stack, RuleSchemaPinningBlock, true)
}

func evalPinning(report *Report, stack *declaredStack, rule string, requireBlock bool) {
	if stack.membershipUnknown {
		report.Results = append(report.Results, unknownResult(rule, Location{Source: "entry", Path: "mcp-servers"}, reasonUncertainMembership, "Effective server membership cannot be determined.", "Use literal names so inheritance and overrides are determinate."))
		return
	}
	for _, srv := range stack.servers {
		if srv.kind == kindMalformed && !srv.kindUnknown {
			report.addDiagnostic(reasonMalformedServer, srv.loc.Source, srv.loc.Path, diagnosticMessage("malformed-server-kind"))
			continue
		}
		if srv.kindUnknown {
			report.Results = append(report.Results, unknownResult(rule, srv.loc, reasonUnknownApplicability, "Schema pinning applicability cannot be determined.", "Declare a single supported MCP server kind."))
			continue
		}
		enabled, enabledUnk := stack.effectivePinEnabled(srv)
		action, actionUnk := stack.effectivePinAction()
		if enabledUnk || (requireBlock && actionUnk) {
			report.Results = append(report.Results, unknownResult(rule, srv.loc, reasonUnknownApplicability, "Effective schema pinning cannot be determined.", "Declare literal schema pinning enablement and action."))
			continue
		}
		if requireBlock {
			if enabled && action == "block" {
				report.Results = append(report.Results, Result{
					Rule:       rule,
					Outcome:    OutcomePass,
					Location:   srv.loc,
					ReasonCode: reasonPinningBlock,
					Message:    "Effective schema pinning is enabled with action block.",
				})
				continue
			}
			report.Results = append(report.Results, Result{
				Rule:        rule,
				Outcome:     OutcomeViolation,
				Location:    srv.loc,
				ReasonCode:  reasonPinningNotBlock,
				Message:     "Effective schema pinning is not enabled with action block.",
				Remediation: "Enable schema pinning globally and set action to block; a per-server true does not override a global disable.",
			})
			continue
		}
		if enabled {
			report.Results = append(report.Results, Result{
				Rule:       rule,
				Outcome:    OutcomePass,
				Location:   srv.loc,
				ReasonCode: reasonPinningEnabled,
				Message:    "Effective schema pinning is enabled for this server.",
			})
			continue
		}
		report.Results = append(report.Results, Result{
			Rule:        rule,
			Outcome:     OutcomeViolation,
			Location:    srv.loc,
			ReasonCode:  reasonPinningDisabled,
			Message:     "Effective schema pinning is not enabled for this server.",
			Remediation: "Enable schema pinning globally; a per-server true does not override a global disable.",
		})
	}
	for _, res := range stack.resources {
		report.Results = append(report.Results, naResult(rule, res.loc, reasonNotMCPServer, "Supporting resources are not MCP servers."))
	}
}

func evalToolLists(report *Report, stack *declaredStack) {
	if stack.membershipUnknown {
		report.Results = append(report.Results, unknownResult(RuleNonemptyServerToolLists, Location{Source: "entry", Path: "mcp-servers"}, reasonUncertainMembership, "Effective server membership cannot be determined.", "Use literal names so inheritance and overrides are determinate."))
		return
	}
	for _, srv := range stack.servers {
		if srv.kind == kindMalformed && !srv.kindUnknown {
			report.addDiagnostic(reasonMalformedServer, srv.loc.Source, srv.loc.Path, diagnosticMessage("malformed-server-kind"))
			continue
		}
		if srv.kindUnknown {
			report.Results = append(report.Results, unknownResult(RuleNonemptyServerToolLists, srv.loc, reasonUnknownApplicability, "Tool-list applicability cannot be determined.", "Declare a single supported MCP server kind."))
			continue
		}
		if srv.toolsInvalid {
			report.addDiagnostic("input-invalid", srv.loc.Source, srv.loc.Path+".tools", "Tools list must be a sequence of names.")
			continue
		}
		if srv.toolsDynamic && len(srv.tools) == 0 {
			report.Results = append(report.Results, unknownResult(RuleNonemptyServerToolLists, Location{Source: srv.loc.Source, Path: srv.loc.Path + ".tools", Line: srv.loc.Line, Column: srv.loc.Column}, reasonUnresolvedToolName, "Tool list contains unresolved names.", "Provide nonempty literal exact tool names."))
			continue
		}
		if !srv.toolsPresent || len(srv.tools) == 0 {
			report.Results = append(report.Results, Result{
				Rule:        RuleNonemptyServerToolLists,
				Outcome:     OutcomeViolation,
				Location:    Location{Source: srv.loc.Source, Path: srv.loc.Path + ".tools", Line: srv.loc.Line, Column: srv.loc.Column},
				ReasonCode:  reasonEmptyTools,
				Message:     "MCP server tools list is missing or empty.",
				Remediation: "Provide a nonempty list of nonempty literal exact tool names.",
			})
			continue
		}
		unknown := false
		empty := false
		var emptyLoc Location
		var unknownLoc Location
		for _, tool := range srv.tools {
			if tool.empty && !tool.dynamic {
				empty = true
				emptyLoc = tool.loc
			}
			if tool.dynamic {
				unknown = true
				unknownLoc = tool.loc
			}
			if strings.Contains(tool.value, "*") && !tool.dynamic {
				report.Warnings = append(report.Warnings, Warning{
					Code:     WarningLiteralToolNameNotPattern,
					Location: tool.loc,
					Message:  literalToolWarningMessage,
				})
			}
		}
		if empty {
			report.Results = append(report.Results, Result{
				Rule:        RuleNonemptyServerToolLists,
				Outcome:     OutcomeViolation,
				Location:    emptyLoc,
				ReasonCode:  reasonEmptyToolName,
				Message:     "Tool list contains an empty name.",
				Remediation: "Provide nonempty literal exact tool names.",
			})
			continue
		}
		if unknown {
			report.Results = append(report.Results, unknownResult(RuleNonemptyServerToolLists, unknownLoc, reasonUnresolvedToolName, "Tool list contains unresolved names.", "Provide nonempty literal exact tool names."))
			continue
		}
		report.Results = append(report.Results, Result{
			Rule:       RuleNonemptyServerToolLists,
			Outcome:    OutcomePass,
			Location:   Location{Source: srv.loc.Source, Path: srv.loc.Path + ".tools", Line: srv.loc.Line, Column: srv.loc.Column},
			ReasonCode: reasonNonemptyTools,
			Message:    "Tools list contains nonempty literal exact names. Existence and authorization were not checked.",
		})
	}
	for _, res := range stack.resources {
		report.Results = append(report.Results, naResult(RuleNonemptyServerToolLists, res.loc, reasonNotMCPServer, "Supporting resources are not MCP servers."))
	}
}

func unknownResult(rule string, loc Location, code, message, remediation string) Result {
	return Result{
		Rule:        rule,
		Outcome:     OutcomeUnknown,
		Location:    loc,
		ReasonCode:  code,
		Message:     message,
		Remediation: remediation,
	}
}

func naResult(rule string, loc Location, code, message string) Result {
	return Result{
		Rule:       rule,
		Outcome:    OutcomeNotApplicable,
		Location:   loc,
		ReasonCode: code,
		Message:    message,
	}
}
