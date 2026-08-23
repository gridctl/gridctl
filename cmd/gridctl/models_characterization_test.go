package main

// Characterization tests freezing the CLI contract of `gridctl models`.
// Same harness as the ctx/skill project tests: the golden files under
// testdata/characterization capture the output byte-for-byte after
// normalization and must pass unchanged across refactors of
// pkg/modelsync.
//
// Regenerate with: GRIDCTL_UPDATE_GOLDEN=1 go test ./cmd/gridctl -run TestCharacterizationModels

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/modelsync"
)

// newModelsCharFixture stages a manager with every state the status
// table can show: a drifted fragment (hand-edited after sync, still
// restart-pending), a drifted include (line removed by hand), and a
// stale OpenCode stanza (policy edited after sync).
func newModelsCharFixture(t *testing.T) (*modelsync.Manager, string) {
	t.Helper()
	home := t.TempDir()
	mgr := modelsync.NewManagerWithHome(home)

	parent := filepath.Join(home, ".litellm", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(parent), 0o755); err != nil {
		t.Fatal(err)
	}
	parentContent := `model_list:
  - model_name: qwen-local
    litellm_params:
      model: openai/qwen3
      api_base: http://127.0.0.1:8000/v1
      api_key: os.environ/DUMMY_KEY
  - model_name: fable
    litellm_params:
      model: openai/fable
      api_key: os.environ/FABLE_API_KEY
`
	if err := os.WriteFile(parent, []byte(parentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	occfg := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(occfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occfg, []byte("{\n  \"theme\": \"dark\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policy := `name: default
kind: models
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
clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: v1
targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`
	if err := mgr.SavePolicy([]byte(policy)); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Sync(context.Background(), modelsync.SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Drift the fragment.
	frag := filepath.Join(filepath.Dir(parent), "gridctl-models.yaml")
	fragData, err := os.ReadFile(frag)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frag, append(fragData, []byte("# hand edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	// Drift the include line away.
	parentData, err := os.ReadFile(parent)
	if err != nil {
		t.Fatal(err)
	}
	stripped := strings.Replace(string(parentData), "include:\n  - gridctl-models.yaml\n", "", 1)
	if err := os.WriteFile(parent, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the OpenCode stanza stale via a policy edit.
	edited := strings.Replace(policy, "base_url: http://localhost:4000/v1", "base_url: http://localhost:4100/v1", 1)
	if err := mgr.SavePolicy([]byte(edited)); err != nil {
		t.Fatal(err)
	}
	return mgr, home
}

func TestCharacterizationModelsStatus(t *testing.T) {
	mgr, home := newModelsCharFixture(t)

	var stdout, stderr bytes.Buffer
	code := runModelsStatus(context.Background(), &stdout, &stderr, mgr, "", true)
	if code != modelsExitAttention {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, modelsExitAttention, stderr.String())
	}
	assertGolden(t, "models-status-plain", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	code = runModelsStatus(context.Background(), &stdout, &stderr, mgr, "json", false)
	if code != modelsExitAttention {
		t.Fatalf("json exit = %d, want %d", code, modelsExitAttention)
	}
	assertGolden(t, "models-status-json", normalizeCharOutput(stdout.String(), home))
}

func TestCharacterizationModelsSyncDryRun(t *testing.T) {
	mgr, home := newModelsCharFixture(t)
	opts := modelsync.SyncOptions{DryRun: true}

	var stdout, stderr bytes.Buffer
	code := runModelsSync(context.Background(), &stdout, &stderr, mgr, opts, "")
	if code != modelsExitAttention {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, modelsExitAttention, stderr.String())
	}
	assertGolden(t, "models-sync-dry-run", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	code = runModelsSync(context.Background(), &stdout, &stderr, mgr, opts, "json")
	if code != modelsExitAttention {
		t.Fatalf("json exit = %d, want %d", code, modelsExitAttention)
	}
	assertGolden(t, "models-sync-dry-run-json", normalizeCharOutput(stdout.String(), home))
}

func TestCharacterizationModelsValidate(t *testing.T) {
	mgr, home := newModelsCharFixture(t)

	var stdout, stderr bytes.Buffer
	code := runModelsValidate(&stdout, &stderr, mgr, "json", false)
	if code != modelsExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	assertGolden(t, "models-validate-json", normalizeCharOutput(stdout.String(), home))
}
