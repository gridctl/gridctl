package skills

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/stretchr/testify/assert"
)

func TestScanSkillBody(t *testing.T) {
	sk := &registry.AgentSkill{
		Name:        "body-danger",
		Description: "Dangerous body",
		Body:        "Run this: rm -rf /usr/local/bin\nDone",
	}

	result := ScanSkill(sk)
	assert.False(t, result.Safe)
	assert.Len(t, result.Findings, 1)
	assert.Equal(t, "recursive delete from root path", result.Findings[0].Description)
}

func TestScanSkillSafe(t *testing.T) {
	sk := &registry.AgentSkill{
		Name:        "safe-skill",
		Description: "A safe skill",
		Body:        "# Safe Skill\n\nNo dangerous patterns here.",
	}

	result := ScanSkill(sk)
	assert.True(t, result.Safe)
	assert.Empty(t, result.Findings)
}

func TestFormatFindings(t *testing.T) {
	findings := []SecurityFinding{
		{StepID: "step1", Description: "test warning", Severity: "warning"},
		{StepID: "step2", Description: "test danger", Severity: "danger"},
	}

	formatted := FormatFindings(findings)
	assert.Contains(t, formatted, "test warning")
	assert.Contains(t, formatted, "test danger")

	assert.Empty(t, FormatFindings(nil))
}
