package modelsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func issueFor(issues []Issue, field string) *Issue {
	for i := range issues {
		if issues[i].Field == field {
			return &issues[i]
		}
	}
	return nil
}

func TestValidate_CleanPolicy(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())
	p := testPolicy(t)
	p.Targets.LiteLLM = nil // no parent to lint against
	if issues := m.Validate(p); len(issues) != 0 {
		t.Errorf("clean policy produced issues: %+v", issues)
	}
}

func TestValidate_Templates(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())
	for _, name := range TemplateNames() {
		p, err := ParsePolicy([]byte(templates[name]))
		if err != nil {
			t.Fatalf("template %s: %v", name, err)
		}
		if issues := m.Validate(p); HasErrors(issues) {
			t.Errorf("template %s has validation errors: %+v", name, issues)
		}
	}
}

func TestValidate_Errors(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())

	cases := []struct {
		name   string
		mutate func(p *Policy)
		field  string
	}{
		{"missing tier", func(p *Policy) { p.Tiers.Complex = "" }, "tiers.COMPLEX"},
		{"undeclared backend", func(p *Policy) { p.Tiers.Simple = "ghost" }, "tiers.SIMPLE"},
		{"bad default tier", func(p *Policy) { p.Router.DefaultTier = "HUGE" }, "router.default_tier"},
		{"bad env var", func(p *Policy) { p.Clients.OpenCode.APIKeyEnv = "lower_case" }, "clients.opencode.api_key_env"},
		{"literal secret as env", func(p *Policy) { p.Clients.OpenCode.APIKeyEnv = "sk-1234567890abcdef1234" }, "clients.opencode.api_key_env"},
		{"bad base url", func(p *Policy) { p.Clients.OpenCode.BaseURL = "not a url" }, "clients.opencode.base_url"},
		{"bad schema", func(p *Policy) { p.Clients.OpenCode.Schema = "v3" }, "clients.opencode.schema"},
		{"passthrough tiers", func(p *Policy) { p.Passthrough = map[string]any{"tiers": map[string]any{}} }, "passthrough.tiers"},
		{"missing config path", func(p *Policy) { p.Targets.LiteLLM = &LiteLLMTarget{} }, "targets.litellm.config_path"},
		{"wrong kind", func(p *Policy) { p.Kind = "skills" }, "kind"},
	}
	for _, tc := range cases {
		p := testPolicy(t)
		p.Targets.LiteLLM = nil
		tc.mutate(p)
		issues := m.Validate(p)
		found := issueFor(issues, tc.field)
		if found == nil || found.Severity != SeverityError {
			t.Errorf("%s: expected error on %s, got %+v", tc.name, tc.field, issues)
		}
	}
}

func TestValidate_DangerousTopLevelKeys(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())
	p, err := ParsePolicy([]byte(`name: x
kind: models
router: {entry_model: r, default_tier: MEDIUM}
backends: [a]
tiers: {SIMPLE: a, MEDIUM: a, COMPLEX: a, REASONING: a}
router_settings:
  num_retries: 2
fallbacks:
  - r: [a]
`))
	if err != nil {
		t.Fatal(err)
	}
	issues := m.Validate(p)
	for _, field := range []string{"router_settings", "fallbacks"} {
		found := issueFor(issues, field)
		if found == nil || found.Severity != SeverityError {
			t.Errorf("expected error on dangerous key %s, got %+v", field, issues)
		}
		if found != nil && !strings.Contains(found.Message, "include directive") {
			t.Errorf("%s message must explain the include hazard: %q", field, found.Message)
		}
	}
}

func TestValidate_SecretInPassthrough(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())
	p := testPolicy(t)
	p.Targets.LiteLLM = nil
	p.Passthrough = map[string]any{
		"classifier_llm_config": map[string]any{"api_key": "sk-abcdef1234567890abcdef"},
	}
	issues := m.Validate(p)
	if !HasErrors(issues) {
		t.Errorf("literal secret in passthrough must error, got %+v", issues)
	}
	// An env reference is fine.
	p.Passthrough = map[string]any{
		"classifier_llm_config": map[string]any{"api_key": "os.environ/OPENAI_API_KEY"},
	}
	if issues := m.Validate(p); HasErrors(issues) {
		t.Errorf("env reference must pass, got %+v", issues)
	}
}

func TestValidate_ParentBackendWarning(t *testing.T) {
	home := t.TempDir()
	m := NewManagerWithHome(home)
	parent := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(parent, []byte("model_list:\n  - model_name: qwen-local\n    litellm_params:\n      model: openai/qwen3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := testPolicy(t)
	p.Targets.LiteLLM = &LiteLLMTarget{ConfigPath: parent}
	issues := m.Validate(p)
	warn := issueFor(issues, "backends")
	if warn == nil || warn.Severity != SeverityWarning || !strings.Contains(warn.Message, "fable") {
		t.Errorf("expected warning about fable missing from parent, got %+v", issues)
	}
	if HasErrors(issues) {
		t.Errorf("missing parent backend is a warning, not an error: %+v", issues)
	}
}
