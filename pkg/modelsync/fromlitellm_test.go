package modelsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLiteLLMConfig_FollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "config.yaml")
	extra := filepath.Join(dir, "extra.yaml")
	if err := os.WriteFile(base, []byte(`include:
  - extra.yaml
model_list:
  - model_name: qwen-local
    litellm_params:
      model: openai/qwen3
  - model_name: my-router
    litellm_params:
      model: auto_router/complexity_router
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte(`model_list:
  - model_name: fable
    litellm_params:
      model: openai/fable
  - model_name: qwen-local
    litellm_params:
      model: openai/duplicate-ignored
`), 0644); err != nil {
		t.Fatal(err)
	}

	scan, err := ParseLiteLLMConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(scan.ModelNames, ",") != "qwen-local,fable" {
		t.Errorf("model names = %v", scan.ModelNames)
	}
	if strings.Join(scan.AutoRouterNames, ",") != "my-router" {
		t.Errorf("auto router names = %v", scan.AutoRouterNames)
	}
}

func TestParseLiteLLMConfig_IncludeCycleStops(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(a, []byte("include: b.yaml\nmodel_list:\n  - model_name: one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("include: a.yaml\nmodel_list:\n  - model_name: two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scan, err := ParseLiteLLMConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.ModelNames) != 2 {
		t.Errorf("cycle scan = %v", scan.ModelNames)
	}
}

func TestInitFromLiteLLM_ScaffoldsReferences(t *testing.T) {
	home := t.TempDir()
	m := NewManagerWithHome(home)
	cfg := filepath.Join(home, ".litellm", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`model_list:
  - model_name: qwen-local
    litellm_params:
      model: openai/qwen3
      api_base: http://127.0.0.1:8000/v1
      api_key: os.environ/DUMMY_KEY
  - model_name: smart-router
    litellm_params:
      model: auto_router/complexity_router
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := m.InitFromLiteLLM(cfg, false); err != nil {
		t.Fatal(err)
	}
	p, err := m.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	// References only: names, never litellm_params inventory.
	if strings.Join(p.Backends, ",") != "qwen-local" {
		t.Errorf("backends = %v", p.Backends)
	}
	if strings.Contains(string(mustRead(t, m.PolicyPath())), "api_base") {
		t.Error("scaffold must not copy model inventory")
	}
	// The existing smart-router auto-router name forces a different
	// entry model.
	if p.Router.EntryModel != "gridctl-router" {
		t.Errorf("entry model = %q", p.Router.EntryModel)
	}
	if p.Targets.LiteLLM == nil || p.Targets.LiteLLM.ConfigPath != cfg {
		t.Errorf("targets = %+v", p.Targets.LiteLLM)
	}
	if issues := m.Validate(p); HasErrors(issues) {
		t.Errorf("scaffold must validate cleanly: %+v", issues)
	}

	// Refuses to overwrite without force.
	if err := m.InitFromLiteLLM(cfg, false); err == nil {
		t.Error("second init must refuse without force")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
