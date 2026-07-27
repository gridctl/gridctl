package skills

import (
	"bytes"
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

// isScannable reports whether a supporting file should be scanned.
//
// Every text file counts, not just recognized script extensions. Two reasons:
// an extension allowlist is trivially sidestepped (a git clone only carries
// 644/755, so an unlisted, non-executable payload would never be looked at),
// and reference documents are prose an agent reads and acts on once the skill
// is projected — the same reason the SKILL.md body is scanned at all. Moving
// instructions from SKILL.md into references/setup.md must not be a way
// around the gate.
//
// Binary content (archives, images, compiled schemas) is skipped by content
// sniff rather than by extension, since running shell-shaped patterns over
// bytes is pure false-positive surface.
func isScannable(content []byte) bool {
	head := content
	if len(head) > 8000 {
		head = head[:8000]
	}
	return !bytes.ContainsRune(head, 0)
}

// scanSupportingFiles scans installed-candidate files for dangerous patterns.
//
// Severity policy: only "danger" findings block an import. The pattern set in
// dangerousPatterns is tuned for prose and shell snippets in a SKILL.md body,
// where any hit is worth a human look. Run unchanged over real Python those
// same patterns produce routine false positives, so gating on every severity
// would make ordinary skill packages un-importable without --trust and train
// users to pass it reflexively. Lower-severity hits are surfaced as warnings
// instead, which keeps them visible without blocking.
//
// Body scanning is unchanged: ScanSkill still treats any finding as blocking.
func scanSupportingFiles(files []supportingFile) (findings []SecurityFinding, blocking bool) {
	for _, f := range files {
		if !isScannable(f.content) {
			continue
		}
		sub := &ScanResult{}
		scanText(f.rel, string(f.content), sub)
		for _, finding := range sub.Findings {
			if finding.Severity == "danger" {
				blocking = true
			}
			findings = append(findings, finding)
		}
	}
	return findings, blocking
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
