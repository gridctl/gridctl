package config

import (
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/depcheck"
)

const (
	prefixMutableImage = "mutable-image-reference: "
	prefixMutablePkg   = "mutable-package-reference: "
	prefixNotAssessed  = "reference-not-assessed: "
)

const (
	msgMutableImage = prefixMutableImage + "version tags can change; use a reviewed digest. Publisher trust and platform support were not checked."
	msgMutablePkg   = prefixMutablePkg + "selectors without an exact version can change; pin an exact release. An exact version is not a transitive lock. Publisher trust was not checked."
	msgCoverage     = "reference-coverage: diagnostics inspect literal selectors only. Exact package versions are not a transitive lock. Digests are content-addressed, not publisher-verified."
)

// CoverageLimitationMessage is the opt-in human-output coverage note.
func CoverageLimitationMessage() string {
	return msgCoverage
}

// DiagnoseMutableRefs inspects literal image and package selectors on an
// unexpanded stack. It performs no I/O, expansion, secret substitution,
// launcher execution, or rewriting.
func DiagnoseMutableRefs(stack *Stack) []ValidationIssue {
	if stack == nil {
		return nil
	}
	var issues []ValidationIssue
	for i, srv := range stack.MCPServers {
		if srv.Image != "" {
			issues = appendFinding(issues, fmt.Sprintf("mcp-servers[%d].image", i), depcheck.ClassifyImage(srv.Image), true)
		}
		if len(srv.Command) > 0 {
			result := depcheck.ClassifyCommand(srv.Command)
			issues = appendFinding(issues, fmt.Sprintf("mcp-servers[%d].command", i), result, true)
		}
	}
	for i, res := range stack.Resources {
		if res.Image != "" {
			issues = appendFinding(issues, fmt.Sprintf("resources[%d].image", i), depcheck.ClassifyImage(res.Image), true)
		}
	}
	return issues
}

// AppendMutableRefIssues copies advisory findings onto result without
// changing validity. Warnings increment WarningCount; informational
// findings do not.
func AppendMutableRefIssues(result *ValidationResult, issues []ValidationIssue) {
	if result == nil {
		return
	}
	for _, issue := range issues {
		result.Issues = append(result.Issues, issue)
		if issue.Severity == SeverityWarning {
			result.WarningCount++
		}
	}
}

func appendFinding(dst []ValidationIssue, field string, result depcheck.Result, emit bool) []ValidationIssue {
	if !emit {
		return dst
	}
	issue, ok := findingFor(field, result)
	if !ok {
		return dst
	}
	return append(dst, issue)
}

func findingFor(field string, result depcheck.Result) (ValidationIssue, bool) {
	switch result.Status {
	case depcheck.StatusMutable:
		msg := msgMutableImage
		if result.Kind == depcheck.KindNPM || result.Kind == depcheck.KindPyPI {
			msg = msgMutablePkg
		}
		return ValidationIssue{Field: field, Message: msg, Severity: SeverityWarning}, true
	case depcheck.StatusNotAssessed:
		return ValidationIssue{Field: field, Message: prefixNotAssessed + notAssessedDetail(result.Reason), Severity: SeverityInfo}, true
	default:
		return ValidationIssue{}, false
	}
}

func notAssessedDetail(reason string) string {
	switch reason {
	case "variable":
		return "selector contains a variable; it was not classified."
	case "invalid-digest":
		return "digest syntax is invalid; it was not classified."
	case "unsupported-option":
		return "launcher options are unsupported; it was not classified."
	case "unsupported-launcher":
		return "launcher is unsupported; it was not classified."
	case "unknown-wrapper":
		return "command wrapper is unsupported; it was not classified."
	case "local-path":
		return "local paths are not classified."
	case "empty":
		return "selector is empty; it was not classified."
	default:
		return "selector is unsupported or dynamic; it was not classified."
	}
}

// MaintenanceRefFinding reports whether an advisory finding must be pinned
// or excepted by example CI. Local-development command wrappers and local
// paths are inventoried separately and do not fail the gate.
func MaintenanceRefFinding(issue ValidationIssue) bool {
	if strings.HasPrefix(issue.Message, prefixMutableImage) || strings.HasPrefix(issue.Message, prefixMutablePkg) {
		return true
	}
	if !strings.HasPrefix(issue.Message, prefixNotAssessed) {
		return false
	}
	detail := strings.TrimPrefix(issue.Message, prefixNotAssessed)
	switch {
	case strings.HasPrefix(detail, "command wrapper is unsupported"),
		strings.HasPrefix(detail, "local paths are not classified"):
		return false
	default:
		return true
	}
}
