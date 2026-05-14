package controller

import (
	"testing"
)

// TestUnprefixRegistrySkillName locks in the prefix-strip the
// childRunToolCaller wrapper relies on. The wrapper is the path
// tool()-from-inside-a-run takes when its target is a registered
// typed skill (vs. a real MCP-server tool); skipping this strip
// caused the child-recorder branch to silently miss every realistic
// call, because bindings.go's resolveToolName returns the prefixed
// form (`registry__<skill>`) but the registry store indexes skills
// by their unprefixed name.
func TestUnprefixRegistrySkillName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		in           string
		wantSkill    string
		wantIsSkill  bool
	}{
		{
			name:        "registry-prefixed skill name strips and reports true",
			in:          "registry__audit-repo",
			wantSkill:   "audit-repo",
			wantIsSkill: true,
		},
		{
			name:        "real mcp server tool passes through unchanged",
			in:          "github__get-issue",
			wantSkill:   "github__get-issue",
			wantIsSkill: false,
		},
		{
			name:        "skill name with embedded delimiter keeps the suffix",
			in:          "registry__sub__skill",
			wantSkill:   "sub__skill",
			wantIsSkill: true,
		},
		{
			name:        "bare name without delimiter passes through",
			in:          "naked",
			wantSkill:   "naked",
			wantIsSkill: false,
		},
		{
			name:        "registry prefix without a skill body is not a registry skill",
			in:          "registry__",
			wantSkill:   "",
			wantIsSkill: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotSkill, gotIsSkill := unprefixRegistrySkillName(tc.in)
			if gotSkill != tc.wantSkill || gotIsSkill != tc.wantIsSkill {
				t.Fatalf("unprefixRegistrySkillName(%q) = (%q, %v), want (%q, %v)",
					tc.in, gotSkill, gotIsSkill, tc.wantSkill, tc.wantIsSkill)
			}
		})
	}
}
