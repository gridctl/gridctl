package config

import (
	"strings"
	"testing"
)

func minimalStackWithSkills(sp *SkillsPolicyConfig) *Stack {
	return &Stack{
		Version: "1",
		Name:    "test",
		Network: Network{Name: "n"},
		MCPServers: []MCPServer{
			{Name: "srv", Transport: "sse", URL: "http://localhost:9000/sse"},
		},
		Skills: sp,
	}
}

func TestValidateSkillsPolicy_AbsentBlockValid(t *testing.T) {
	if err := Validate(minimalStackWithSkills(nil)); err != nil {
		t.Fatalf("absent skills block failed validation: %v", err)
	}
}

func TestValidateSkillsPolicy_ValidBlock(t *testing.T) {
	sp := &SkillsPolicyConfig{
		Default: "deny",
		Allow:   []string{"incident-*", "exact"},
		Deny:    []string{"*refund*"},
	}
	if err := Validate(minimalStackWithSkills(sp)); err != nil {
		t.Fatalf("valid skills block failed validation: %v", err)
	}
}

func TestValidateSkillsPolicy_BadDefault(t *testing.T) {
	err := Validate(minimalStackWithSkills(&SkillsPolicyConfig{Default: "block"}))
	if err == nil || !strings.Contains(err.Error(), "skills.default") {
		t.Fatalf("bad default not rejected: %v", err)
	}
}

func TestValidateSkillsPolicy_BadPattern(t *testing.T) {
	err := Validate(minimalStackWithSkills(&SkillsPolicyConfig{Deny: []string{"[unclosed"}}))
	if err == nil || !strings.Contains(err.Error(), "skills.deny[0]") {
		t.Fatalf("unparseable glob not rejected: %v", err)
	}
}

func TestValidateSkillsPolicy_EmptyPattern(t *testing.T) {
	err := Validate(minimalStackWithSkills(&SkillsPolicyConfig{Allow: []string{""}}))
	if err == nil || !strings.Contains(err.Error(), "skills.allow[0]") {
		t.Fatalf("empty pattern not rejected: %v", err)
	}
}
