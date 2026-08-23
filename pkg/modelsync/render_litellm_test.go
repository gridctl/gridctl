package modelsync

import (
	"strings"
	"testing"
)

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := ParsePolicy([]byte(`name: default
kind: models
description: test policy
router:
  entry_model: smart-router
  default_tier: MEDIUM
backends:
  - qwen-local
  - fable
tiers:
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: fable
  REASONING: fable
weights:
  tokenCount: 0.0
  reasoningMarkers: 0.40
clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: v1
targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderLiteLLM_Golden(t *testing.T) {
	p := testPolicy(t)
	got, err := RenderLiteLLM(p, "sha256:testhash")
	if err != nil {
		t.Fatal(err)
	}

	want := `# MANAGED BY GRIDCTL - do not edit. Edit the models policy and re-run
# 'gridctl models sync'. Source: models policy "default"  policy-hash: sha256:testhash
# The router below references model_name values from the including
# config's own model_list; this fragment never defines backends.
model_list:
  - model_name: smart-router
    litellm_params:
      model: auto_router/complexity_router
      complexity_router_default_model: qwen-local
      complexity_router_config:
        tiers:
          SIMPLE: qwen-local
          MEDIUM: qwen-local
          COMPLEX: fable
          REASONING: fable
        dimension_weights:
          reasoningMarkers: 0.4
          tokenCount: 0.0
`
	if string(got) != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderLiteLLM_Deterministic(t *testing.T) {
	p := testPolicy(t)
	p.Passthrough = map[string]any{
		"session_affinity": true,
		"keyword_tier_rules": map[string]any{
			"zeta": "REASONING", "alpha": "SIMPLE",
		},
	}
	a, err := RenderLiteLLM(p, "sha256:x")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderLiteLLM(p, "sha256:x")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("render is not deterministic:\n%s\nvs\n%s", a, b)
	}
}

func TestRenderLiteLLM_RouterOnly(t *testing.T) {
	p := testPolicy(t)
	got, err := RenderLiteLLM(p, "sha256:x")
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	// The fragment defines exactly one model_list entry (the router);
	// backends stay in the parent, and no top-level settings ride along.
	if n := strings.Count(out, "model_name:"); n != 1 {
		t.Errorf("expected exactly one model_list entry, got %d:\n%s", n, out)
	}
	for _, forbidden := range []string{"router_settings", "general_settings", "litellm_settings", "api_key", "api_base"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("fragment must not contain %q:\n%s", forbidden, out)
		}
	}
	// The default model is a sibling of complexity_router_config, per
	// LiteLLM's documented placement.
	sibling := "      complexity_router_default_model: qwen-local\n      complexity_router_config:"
	if !strings.Contains(out, sibling) {
		t.Errorf("complexity_router_default_model must sit as a sibling of complexity_router_config:\n%s", out)
	}
}

func TestRenderLiteLLM_PassthroughTypedKeysWin(t *testing.T) {
	p := testPolicy(t)
	p.Passthrough = map[string]any{
		"tiers":             map[string]any{"SIMPLE": "evil"},
		"dimension_weights": map[string]any{"tokenCount": 99},
		"session_affinity":  true,
	}
	got, err := RenderLiteLLM(p, "sha256:x")
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if strings.Contains(out, "evil") {
		t.Errorf("passthrough tiers must not override typed tiers:\n%s", out)
	}
	if strings.Contains(out, "99") {
		t.Errorf("passthrough dimension_weights must lose to typed weights:\n%s", out)
	}
	if !strings.Contains(out, "session_affinity: true") {
		t.Errorf("unmodeled passthrough keys must render:\n%s", out)
	}
}
