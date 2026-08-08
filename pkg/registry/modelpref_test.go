package registry

import (
	"bytes"
	"testing"
)

func TestExtractModelPreference_Precedence(t *testing.T) {
	tests := []struct {
		name      string
		skill     *AgentSkill
		wantVal   string
		wantKey   string
		wantIsNil bool
	}{
		{
			name:      "no declaration",
			skill:     &AgentSkill{Name: "plain"},
			wantIsNil: true,
		},
		{
			name:    "top-level model",
			skill:   &AgentSkill{Name: "s", Extra: map[string]any{"model": "opus"}},
			wantVal: "opus",
			wantKey: ModelSourceTopLevel,
		},
		{
			name: "top-level beats metadata",
			skill: &AgentSkill{Name: "s",
				Extra:    map[string]any{"model": "opus"},
				Metadata: SkillMetadata{"preferred-model": "haiku"}},
			wantVal: "opus",
			wantKey: ModelSourceTopLevel,
		},
		{
			name:    "metadata preferred-model beats metadata model",
			skill:   &AgentSkill{Name: "s", Metadata: SkillMetadata{"preferred-model": "haiku", "model": "sonnet"}},
			wantVal: "haiku",
			wantKey: ModelSourceMetaPreferred,
		},
		{
			name:    "metadata model alone",
			skill:   &AgentSkill{Name: "s", Metadata: SkillMetadata{"model": "sonnet"}},
			wantVal: "sonnet",
			wantKey: ModelSourceMetaModel,
		},
		{
			name:      "non-string top-level ignored",
			skill:     &AgentSkill{Name: "s", Extra: map[string]any{"model": 3}},
			wantIsNil: true,
		},
		{
			name:      "empty values ignored",
			skill:     &AgentSkill{Name: "s", Extra: map[string]any{"model": "  "}, Metadata: SkillMetadata{"model": ""}},
			wantIsNil: true,
		},
		{
			name:    "value is trimmed",
			skill:   &AgentSkill{Name: "s", Extra: map[string]any{"model": " sonnet "}},
			wantVal: "sonnet",
			wantKey: ModelSourceTopLevel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractModelPreference(tt.skill)
			if tt.wantIsNil {
				if got != nil {
					t.Fatalf("expected nil preference, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a preference, got nil")
			}
			if got.Value() != tt.wantVal {
				t.Errorf("Value() = %q, want %q", got.Value(), tt.wantVal)
			}
			if got.SourceKey != tt.wantKey {
				t.Errorf("SourceKey = %q, want %q", got.SourceKey, tt.wantKey)
			}
		})
	}
}

func TestExtractModelPreference_NilSkill(t *testing.T) {
	if got := ExtractModelPreference(nil); got != nil {
		t.Fatalf("nil skill should yield nil preference, got %+v", got)
	}
	var p *ModelPreference
	if p.Value() != "" {
		t.Fatalf("nil preference Value() should be empty")
	}
}

func TestModelPreferenceFromKeys(t *testing.T) {
	if p := ModelPreferenceFromKeys("opus", map[string]string{"model": "haiku"}); p.Value() != "opus" || p.SourceKey != ModelSourceTopLevel {
		t.Fatalf("top-level should win: %+v", p)
	}
	if p := ModelPreferenceFromKeys("", map[string]string{"preferred-model": "haiku"}); p.Value() != "haiku" || p.SourceKey != ModelSourceMetaPreferred {
		t.Fatalf("metadata preferred-model: %+v", p)
	}
	if p := ModelPreferenceFromKeys("", nil); p != nil {
		t.Fatalf("no declaration should be nil, got %+v", p)
	}
}

func TestNormalizeModelValue(t *testing.T) {
	if NormalizeModelValue(" Opus ") != "opus" {
		t.Fatal("normalize should trim and lowercase")
	}
	if NormalizeModelValue("") != "" {
		t.Fatal("empty stays empty")
	}
}

func TestIsKnownModelValue(t *testing.T) {
	known := []string{"sonnet", "Opus", "haiku", "fable", "default", "best", "opusplan", "inherit", "sonnet[1m]", "opus[1m]", "claude-opus-5", "anthropic.claude-sonnet-4-20250514-v1:0", "us.anthropic.claude-opus-5"}
	for _, v := range known {
		if !IsKnownModelValue(v) {
			t.Errorf("IsKnownModelValue(%q) = false, want true", v)
		}
	}
	unknown := []string{"", "gpt-4o", "fastest", "banana[1m]", "gemini-2.5-pro"}
	for _, v := range unknown {
		if IsKnownModelValue(v) {
			t.Errorf("IsKnownModelValue(%q) = true, want false", v)
		}
	}
}

func TestHonorMatrices(t *testing.T) {
	if SkillHonor("claude-code") != HonorHonored {
		t.Error("claude-code skills honor model inline (turn-scoped)")
	}
	if SkillHonor("antigravity") != HonorIgnored {
		t.Error("antigravity has no documented skill model key")
	}
	if SkillHonor("agents") != HonorUnknown {
		t.Error("interop dir honor is consumer-dependent")
	}
	if SkillHonor("never-heard-of-it") != HonorUnknown {
		t.Error("unknown slugs report unknown, never a guess")
	}
	if AgentHonor("claude-code") != HonorHonored {
		t.Error("claude-code agent model frontmatter is a resolution input")
	}
	for _, slug := range []string{"opencode", "copilot", "gemini"} {
		if AgentHonor(slug) != HonorDropped {
			t.Errorf("%s render drops model", slug)
		}
	}
	// The matrix copies must not alias internal state.
	m := SkillHonorMatrix()
	m["claude-code"] = HonorIgnored
	if SkillHonor("claude-code") != HonorHonored {
		t.Error("SkillHonorMatrix must return a copy")
	}
}

func TestRenderWithModelPreference(t *testing.T) {
	src := []byte("---\nname: demo\ndescription: d\nstate: active\nmodel: opus\nmetadata:\n  model: opus\n---\n\nBody.\n")
	sk, err := ParseSkillMD(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RenderWithModelPreference(sk, "haiku")
	if err != nil {
		t.Fatal(err)
	}
	re, err := ParseSkillMD(out)
	if err != nil {
		t.Fatal(err)
	}
	if re.Extra["model"] != "haiku" {
		t.Errorf("top-level model = %v, want haiku", re.Extra["model"])
	}
	if re.Metadata["model"] != "haiku" {
		t.Errorf("metadata model = %q, want haiku (no disagreeing declarations)", re.Metadata["model"])
	}
	if re.Body != sk.Body {
		t.Error("body must ride through unchanged")
	}
	// The original skill is never mutated (registry canonical safety).
	if sk.Extra["model"] != "opus" || sk.Metadata["model"] != "opus" {
		t.Fatal("RenderWithModelPreference mutated its input")
	}
}

func TestRenderWithModelPreference_InjectsHonoredKey(t *testing.T) {
	// A metadata-only author still gets the honored top-level key.
	src := []byte("---\nname: demo\ndescription: d\nmetadata:\n  preferred-model: opus\n---\n\nBody.\n")
	sk, err := ParseSkillMD(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RenderWithModelPreference(sk, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	re, err := ParseSkillMD(out)
	if err != nil {
		t.Fatal(err)
	}
	if re.Extra["model"] != "sonnet" {
		t.Errorf("top-level model = %v, want sonnet", re.Extra["model"])
	}
	if re.Metadata["preferred-model"] != "sonnet" {
		t.Errorf("metadata preferred-model = %q, want sonnet", re.Metadata["preferred-model"])
	}
	if re.Metadata["model"] != "" {
		t.Errorf("metadata model should not be invented, got %q", re.Metadata["model"])
	}
}

func TestRenderWithModelPreference_Deterministic(t *testing.T) {
	src := []byte("---\nname: demo\ndescription: d\n---\n\nBody.\n")
	sk, err := ParseSkillMD(src)
	if err != nil {
		t.Fatal(err)
	}
	a, err := RenderWithModelPreference(sk, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderWithModelPreference(sk, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("rewrite render must be deterministic")
	}
}
