package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

// stageWarningsRegistry points state.BaseDir at a temp home and writes
// active skills (and optionally an agent) into its registry.
func stageWarningsRegistry(t *testing.T, skillFrontmatter map[string]string, agentModel string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for name, fm := range skillFrontmatter {
		dir := filepath.Join(home, ".gridctl", "registry", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: d\nstate: active\n" + fm + "---\nBody.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if agentModel != "" {
		dir := filepath.Join(home, ".gridctl", "registry", "agents", "helper")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: helper\ndescription: d\nmodel: " + agentModel + "\n---\nBody.\n"
		if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func warningsStack(mp *config.ModelPreferencesConfig) *config.Stack {
	return &config.Stack{Name: "t", ModelPreferences: mp}
}

func TestModelPreferenceWarnings_NilBlockIsNoOp(t *testing.T) {
	stageWarningsRegistry(t, map[string]string{"plain": "model: opus\n"}, "")
	if got := ModelPreferenceWarnings(nil); got != nil {
		t.Fatalf("nil stack must yield nothing, got %v", got)
	}
	if got := ModelPreferenceWarnings(warningsStack(nil)); got != nil {
		t.Fatalf("absent block must yield nothing, got %v", got)
	}
}

func TestModelPreferenceWarnings_UnhonoredGatedOnRewrite(t *testing.T) {
	stageWarningsRegistry(t, map[string]string{
		"declared": "model: opus\n",
		"plain":    "",
	}, "")

	// Surfacing-only scope: no unhonored finding at all.
	got := ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Default: "sonnet"},
	}))
	for _, w := range got {
		if strings.Contains(w, "model-preference-unhonored") {
			t.Fatalf("surfacing-only scope must not warn unhonored: %v", got)
		}
	}

	// Rewrite scope: one aggregate line, not one per skill.
	got = ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet"},
	}))
	unhonored := 0
	for _, w := range got {
		if strings.Contains(w, "model-preference-unhonored") {
			unhonored++
			if !strings.Contains(w, "2 active skill(s)") {
				t.Errorf("aggregate should count both resolving skills: %q", w)
			}
			if !strings.Contains(w, "antigravity") {
				t.Errorf("aggregate should name the ignoring target: %q", w)
			}
		}
	}
	if unhonored != 1 {
		t.Fatalf("expected exactly one aggregate unhonored line, got %d in %v", unhonored, got)
	}
}

func agentUnhonored(t *testing.T, got []string) []string {
	t.Helper()
	var out []string
	for _, w := range got {
		if strings.Contains(w, "model-preference-unhonored") && strings.Contains(w, "agents") {
			out = append(out, w)
		}
	}
	return out
}

func TestModelPreferenceWarnings_AgentsScopeAggregates(t *testing.T) {
	stageWarningsRegistry(t, nil, "opus")
	got := agentUnhonored(t, ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Agents: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet"},
	})))
	if len(got) != 1 {
		t.Fatalf("expected one agent-scope unhonored line, got %v", got)
	}
	for _, want := range []string{"copilot (1 agent(s))", "gemini (1 agent(s))", "opencode (1 agent(s))", "set model_preferences.agents.clients.<slug>"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("agent line should contain %q: %q", want, got[0])
		}
	}
}

func TestModelPreferenceWarnings_AgentsPerTargetClients(t *testing.T) {
	stageWarningsRegistry(t, nil, "opus")
	client := func(v string) *config.ModelPreferenceClient { return &config.ModelPreferenceClient{Default: v} }

	// Only opencode has a client value: copilot and gemini stay listed.
	got := agentUnhonored(t, ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Agents: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet", Clients: map[string]*config.ModelPreferenceClient{
			"opencode": client("anthropic/claude-sonnet-5-5"),
		}},
	})))
	if len(got) != 1 {
		t.Fatalf("expected exactly one agent unhonored line, got %v", got)
	}
	if !strings.Contains(got[0], "copilot (1 agent(s))") || !strings.Contains(got[0], "gemini (1 agent(s))") || strings.Contains(got[0], "opencode") {
		t.Errorf("line should name copilot and gemini only: %q", got[0])
	}
	if !strings.Contains(got[0], "model_preferences.agents.clients.<slug>") {
		t.Errorf("line should name the remedy: %q", got[0])
	}

	// An override-only client value covers the named agent.
	got = agentUnhonored(t, ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Agents: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet", Clients: map[string]*config.ModelPreferenceClient{
			"opencode": client("anthropic/claude-sonnet-5-5"),
			"copilot":  {Overrides: map[string]string{"helper": "gpt-5"}},
			"gemini":   client("inherit"),
		}},
	})))
	if len(got) != 0 {
		t.Fatalf("every target covered: no unhonored line expected, got %v", got)
	}
}

func TestModelPreferenceWarnings_PortabilityOnlyForTopLevel(t *testing.T) {
	stageWarningsRegistry(t, map[string]string{
		"top":  "model: opus\n",
		"meta": "metadata:\n  preferred-model: opus\n",
		"none": "",
	}, "")
	got := ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{},
	}))
	var portability []string
	for _, w := range got {
		if strings.Contains(w, "model-preference-portability") {
			portability = append(portability, w)
		}
	}
	if len(portability) != 1 || !strings.Contains(portability[0], `"top"`) {
		t.Fatalf("portability must fire once, only for the top-level declaration: %v", portability)
	}
}

func TestModelPreferenceWarnings_UnreadableRegistryIsEmpty(t *testing.T) {
	// A home with no registry at all: best-effort empty, never an error
	// or a panic.
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := ModelPreferenceWarnings(warningsStack(&config.ModelPreferencesConfig{
		Skills: &config.ModelPreferenceScope{Rewrite: true, Default: "sonnet"},
	}))
	if len(got) != 0 {
		t.Fatalf("missing registry must yield no findings, got %v", got)
	}
}
