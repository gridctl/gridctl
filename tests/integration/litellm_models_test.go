//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/modelsync"
)

// litellmImage is the pinned LiteLLM release the rendered fragment is
// validated against. LiteLLM publishes no JSON schema for config.yaml,
// so booting the real proxy is the only reliable contract check; the
// digest pins the exact artifact. Auto Router v2 (the schema the
// renderer emits) requires v1.94.0 or later. Bump deliberately, with
// the renderer, never as a routine dependency update.
const litellmImage = "ghcr.io/berriai/litellm:v1.97.0@sha256:468c25f35f3e5ec4e414974f00deab93337b1b4d9953cabcfd3722e59415f834"

const litellmMasterKey = "sk-gridctl-integration-test"

// TestLiteLLMBootsRenderedFragment renders the models fragment from a
// realistic policy, includes it from a human-shaped parent config
// (own model_list, comments, router_settings), boots the pinned
// LiteLLM image against it, and asserts the proxy comes up serving the
// router model. This is the contract check for the renderer's schema
// assumptions (include semantics, auto_router key names, default-model
// sibling placement).
func TestLiteLLMBootsRenderedFragment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skipf("docker CLI not available: %v", err)
	}
	infoCtx, cancelInfo := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelInfo()
	if err := exec.CommandContext(infoCtx, docker, "info").Run(); err != nil {
		t.Skipf("docker daemon not available: %v", err)
	}

	// The image is multi-GB, so the pull happens OUTSIDE the test (a
	// `task test:litellm` / CI pre-pull step); a cold cache skips here
	// rather than eating the whole suite's timeout. The dedicated CI job
	// always pre-pulls, so this skip never hides the contract check
	// there.
	inspectCtx, cancelInspect := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelInspect()
	if err := exec.CommandContext(inspectCtx, docker, "image", "inspect", litellmImage).Run(); err != nil {
		t.Skipf("LiteLLM image not cached; run 'task test:litellm' (pre-pulls %s)", litellmImage)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	policy, err := modelsync.ParsePolicy([]byte(`name: default
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
weights:
  tokenCount: 0.0
  reasoningMarkers: 0.40
  technicalTerms: 0.25
  codePresence: 0.20
  simpleIndicators: 0.10
  multiStepPatterns: 0.05
`))
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := modelsync.RenderLiteLLM(policy, policy.Hash())
	if err != nil {
		t.Fatal(err)
	}

	cfgDir := t.TempDir()
	parent := `# human-owned config; the fragment must not clobber any of this
model_list:
  - model_name: qwen-local
    litellm_params:
      model: openai/qwen-local
      api_base: http://127.0.0.1:9   # never called; boot-time only
      api_key: os.environ/DUMMY_KEY
  - model_name: fable
    litellm_params:
      model: openai/fable
      api_base: http://127.0.0.1:9
      api_key: os.environ/DUMMY_KEY

router_settings:
  num_retries: 2

general_settings:
  master_key: ` + litellmMasterKey + `

include:
  - gridctl-models.yaml
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "gridctl-models.yaml"), fragment, 0644); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	name := fmt.Sprintf("gridctl-litellm-it-%d", port)
	run := exec.CommandContext(ctx, docker, "run", "-d", "--rm",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:4000", port),
		"-v", cfgDir+":/app/cfg:ro",
		"-e", "DUMMY_KEY=dummy",
		litellmImage,
		"--config", "/app/cfg/config.yaml", "--port", "4000")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = exec.CommandContext(stopCtx, docker, "rm", "-f", name).Run()
	})

	models, err := waitForLiteLLMModels(ctx, docker, name, port)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"smart-router": false, "qwen-local": false, "fable": false}
	for _, m := range models {
		if _, ok := want[m]; ok {
			want[m] = true
		}
	}
	for m, seen := range want {
		if !seen {
			t.Errorf("/v1/models is missing %q (got %v): the include or router schema was rejected", m, models)
		}
	}
}

// waitForLiteLLMModels polls /v1/models until the proxy answers,
// returning the served model ids. A dead container short-circuits with
// its logs so a schema rejection reads as itself, not as a timeout.
func waitForLiteLLMModels(ctx context.Context, docker, name string, port int) ([]string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+litellmMasterKey)
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				var doc struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				derr := json.NewDecoder(resp.Body).Decode(&doc)
				resp.Body.Close()
				if derr != nil {
					return nil, derr
				}
				ids := make([]string, 0, len(doc.Data))
				for _, d := range doc.Data {
					ids = append(ids, d.ID)
				}
				return ids, nil
			}
			resp.Body.Close()
		}
		// A container that already exited means the config was rejected.
		psCtx, psCancel := context.WithTimeout(ctx, 5*time.Second)
		out, psErr := exec.CommandContext(psCtx, docker, "inspect", "-f", "{{.State.Running}}", name).CombinedOutput()
		psCancel()
		if psErr == nil && strings.TrimSpace(string(out)) == "false" {
			logCtx, logCancel := context.WithTimeout(ctx, 10*time.Second)
			logs, _ := exec.CommandContext(logCtx, docker, "logs", "--tail", "60", name).CombinedOutput()
			logCancel()
			return nil, fmt.Errorf("LiteLLM exited during boot (config rejected?):\n%s", logs)
		}
		time.Sleep(2 * time.Second)
	}
	logCtx, logCancel := context.WithTimeout(ctx, 10*time.Second)
	logs, _ := exec.CommandContext(logCtx, docker, "logs", "--tail", "60", name).CombinedOutput()
	logCancel()
	return nil, fmt.Errorf("LiteLLM did not become ready in time; last logs:\n%s", logs)
}
