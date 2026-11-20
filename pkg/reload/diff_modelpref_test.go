package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func modelPrefTestStack(mp *config.ModelPreferencesConfig) *config.Stack {
	return &config.Stack{
		Name:             "test",
		Network:          config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers:       []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}},
		ModelPreferences: mp,
	}
}

func TestComputeDiff_ModelPreferencesOnlyChange(t *testing.T) {
	old := modelPrefTestStack(nil)
	new := modelPrefTestStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet"},
	})

	diff := ComputeDiff(old, new)
	if !diff.ModelPreferencesChanged {
		t.Error("expected ModelPreferencesChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for a model_preferences-only change")
	}
	// A policy-only change must not touch containers, networks, or resources.
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Removed) != 0 ||
		len(diff.MCPServers.Modified) != 0 {
		t.Error("expected no MCP server changes for a model_preferences-only change")
	}
	if diff.NetworkChanged || diff.ClientsChanged || diff.SkillsPolicyChanged {
		t.Error("model_preferences-only change flagged unrelated diffs")
	}
}

func TestComputeDiff_ModelPreferencesIdentical(t *testing.T) {
	mk := func() *config.ModelPreferencesConfig {
		return &config.ModelPreferencesConfig{
			Skills: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet",
				Overrides: map[string]string{"a": "opus"}},
			Agents: &config.ModelPreferenceScope{Default: "haiku"},
		}
	}
	diff := ComputeDiff(modelPrefTestStack(mk()), modelPrefTestStack(mk()))
	if diff.ModelPreferencesChanged {
		t.Error("identical blocks must not flag a change")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff")
	}
}

func TestComputeDiff_ModelPreferencesOverrideEdit(t *testing.T) {
	old := modelPrefTestStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Rewrite: true, Overrides: map[string]string{"a": "opus"}},
	})
	new := modelPrefTestStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Rewrite: true, Overrides: map[string]string{"a": "haiku"}},
	})
	if !ComputeDiff(old, new).ModelPreferencesChanged {
		t.Error("an override edit must flag the change")
	}
}

func TestComputeDiff_ModelPreferencesClientsEdit(t *testing.T) {
	mk := func(def, override string) *config.ModelPreferencesConfig {
		c := &config.ModelPreferenceClient{Default: def}
		if override != "" {
			c.Overrides = map[string]string{"reviewer": override}
		}
		return &config.ModelPreferencesConfig{
			Agents: &config.ModelPreferenceScope{Rewrite: true, Clients: map[string]*config.ModelPreferenceClient{"opencode": c}},
		}
	}
	base := mk("anthropic/claude-sonnet-5-5", "")
	for name, next := range map[string]*config.ModelPreferencesConfig{
		"default edit":  mk("anthropic/claude-opus-5-5", ""),
		"override edit": mk("anthropic/claude-sonnet-5-5", "anthropic/claude-opus-5-5#high"),
		"clients removed": {
			Agents: &config.ModelPreferenceScope{Rewrite: true},
		},
	} {
		if !ComputeDiff(modelPrefTestStack(base), modelPrefTestStack(next)).ModelPreferencesChanged {
			t.Errorf("%s inside clients must flag the change", name)
		}
	}
	if ComputeDiff(modelPrefTestStack(base), modelPrefTestStack(mk("anthropic/claude-sonnet-5-5", ""))).ModelPreferencesChanged {
		t.Error("identical clients blocks must not flag a change")
	}
}
