package skills

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// SecurityFinding represents a potentially dangerous pattern found in a skill.
type SecurityFinding struct {
	StepID      string `json:"stepId"`
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "warning" or "danger"
}

// ScanResult contains the security scan results for a skill.
type ScanResult struct {
	SkillName string            `json:"skillName"`
	Findings  []SecurityFinding `json:"findings"`
	Safe      bool              `json:"safe"`
}

var dangerousPatterns = []struct {
	pattern     *regexp.Regexp
	description string
	severity    string
}{
	{
		pattern:     regexp.MustCompile(`curl\s.*\|\s*(?:ba)?sh`),
		description: "piped curl to shell execution",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`wget\s.*\|\s*(?:ba)?sh`),
		description: "piped wget to shell execution",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`eval\s+\$`),
		description: "eval with variable expansion",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`rm\s+-rf\s+/[^.]`),
		description: "recursive delete from root path",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`chmod\s+777`),
		description: "world-writable permissions",
		severity:    "warning",
	},
	{
		pattern:     regexp.MustCompile(`>\s*/etc/`),
		description: "write to system configuration directory",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`(?:nc|ncat|netcat)\s+-[el]`),
		description: "network listener (potential reverse shell)",
		severity:    "danger",
	},
	{
		pattern:     regexp.MustCompile(`\bexec\b.*\b(?:bash|sh|zsh)\b`),
		description: "unrestricted shell execution",
		severity:    "warning",
	},
}

// ScanSkill checks a skill for dangerous patterns in its body.
func ScanSkill(sk *registry.AgentSkill) *ScanResult {
	result := &ScanResult{
		SkillName: sk.Name,
		Safe:      true,
	}

	if sk.Body != "" {
		scanText("body", sk.Body, result)
	}

	result.Safe = len(result.Findings) == 0
	return result
}

func scanText(context, text string, result *ScanResult) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		for _, dp := range dangerousPatterns {
			if dp.pattern.MatchString(line) {
				result.Findings = append(result.Findings, SecurityFinding{
					StepID:      fmt.Sprintf("%s:line-%d", context, i+1),
					Pattern:     dp.pattern.String(),
					Description: dp.description,
					Severity:    dp.severity,
				})
			}
		}
	}
}

// FormatFindings returns a human-readable summary of security findings.
func FormatFindings(findings []SecurityFinding) string {
	if len(findings) == 0 {
		return ""
	}

	var b strings.Builder
	for _, f := range findings {
		icon := "WARN"
		if f.Severity == "danger" {
			icon = "DANGER"
		}
		fmt.Fprintf(&b, "  %s [%s] %s: %s\n", icon, f.StepID, f.Severity, f.Description)
	}
	return b.String()
}
