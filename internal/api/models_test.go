package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/modelsync"
)

// modelsTestPolicy declares every target so status reports all three
// rows: fragment, include (config_path), and OpenCode.
const modelsTestPolicy = `name: default
kind: models
router:
  entry_model: smart-router
  default_tier: MEDIUM
backends:
  - qwen-local
  - claude-sonnet
tiers:
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: claude-sonnet
  REASONING: claude-sonnet
clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
targets:
  litellm:
    config_path: ~/litellm/config.yaml
`

// setupModelsTestServer builds a Server whose models manager is rooted
// at a temp home, with a parent LiteLLM config in place so include sync
// has a file to edit.
func setupModelsTestServer(t *testing.T) (*Server, *modelsync.Manager, string) {
	t.Helper()
	srv := newTestServer(t)
	home := t.TempDir()
	mgr := modelsync.NewManagerWithHome(home)
	srv.SetModelsManager(mgr)
	litellmDir := filepath.Join(home, "litellm")
	if err := os.MkdirAll(litellmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := "model_list:\n  - model_name: qwen-local\n  - model_name: claude-sonnet\n"
	if err := os.WriteFile(filepath.Join(litellmDir, "config.yaml"), []byte(parent), 0o644); err != nil {
		t.Fatal(err)
	}
	return srv, mgr, home
}

func seedModelsPolicy(t *testing.T, mgr *modelsync.Manager, content string) {
	t.Helper()
	if err := mgr.SavePolicy([]byte(content)); err != nil {
		t.Fatalf("seeding policy: %v", err)
	}
}

func TestHandleModelsStatus_NoPolicy(t *testing.T) {
	srv, _, _ := setupModelsTestServer(t)
	rec := doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["policy_exists"] != false {
		t.Errorf("policy_exists = %v, want false", doc["policy_exists"])
	}
	if _, ok := doc["routing"]; ok {
		t.Error("routing must be absent with no policy")
	}
	targets, ok := doc["targets"].([]any)
	if !ok || len(targets) != 1 {
		t.Fatalf("targets = %v, want one never-synced fragment row", doc["targets"])
	}
	row := targets[0].(map[string]any)
	if row["target"] != "litellm-fragment" || row["state"] != "never-synced" {
		t.Errorf("row = %v, want never-synced litellm-fragment", row)
	}
}

func TestHandleModelsStatus_RoutingSummaryAndTargets(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)
	seedModelsPolicy(t, mgr, modelsTestPolicy)

	rec := doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		PolicyExists bool `json:"policy_exists"`
		Routing      *struct {
			EntryModel  string            `json:"entry_model"`
			DefaultTier string            `json:"default_tier"`
			Backends    []string          `json:"backends"`
			Tiers       map[string]string `json:"tiers"`
		} `json:"routing"`
		Targets []modelsync.Status `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.PolicyExists || doc.Routing == nil {
		t.Fatalf("doc = %+v, want policy with routing summary", doc)
	}
	if doc.Routing.EntryModel != "smart-router" || doc.Routing.DefaultTier != "MEDIUM" {
		t.Errorf("routing = %+v", doc.Routing)
	}
	if doc.Routing.Tiers["REASONING"] != "claude-sonnet" {
		t.Errorf("tiers = %v", doc.Routing.Tiers)
	}
	// All three targets are declared, so all three rows appear.
	got := map[string]bool{}
	for _, s := range doc.Targets {
		got[s.Target] = true
	}
	for _, want := range []string{"litellm-fragment", "litellm-include", "opencode"} {
		if !got[want] {
			t.Errorf("missing target row %q in %v", want, got)
		}
	}
}

func TestHandleModelsStatus_VariableTargetRows(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)
	// No clients block and no config_path: only the fragment row exists.
	seedModelsPolicy(t, mgr, strings.Split(modelsTestPolicy, "clients:")[0])

	rec := doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	var doc struct {
		Targets []modelsync.Status `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Targets) != 1 || doc.Targets[0].Target != "litellm-fragment" {
		t.Fatalf("targets = %+v, want the fragment row alone", doc.Targets)
	}
}

func TestHandleModelsStatus_ParseFailureIs200(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)
	seedModelsPolicy(t, mgr, modelsTestPolicy)
	if err := os.WriteFile(mgr.PolicyPath(), []byte("kind: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with policy_error", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["policy_error"] == nil || doc["policy_error"] == "" {
		t.Error("policy_error must carry the parse failure")
	}
	if _, ok := doc["routing"]; ok {
		t.Error("routing must be absent when the policy does not parse")
	}
}

func TestHandleModelsSync_FullLifecycle(t *testing.T) {
	srv, mgr, home := setupModelsTestServer(t)
	seedModelsPolicy(t, mgr, modelsTestPolicy)

	// Dry-run with diffs: nothing written, would-update rows with diffs.
	rec := doJSON(t, srv, http.MethodPost, "/api/project/models/sync", `{"dry_run":true,"diff":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run status = %d: %s", rec.Code, rec.Body.String())
	}
	var results []modelsync.SyncResult
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	var sawDiff bool
	for _, r := range results {
		if r.Action == modelsync.ActionWouldUpdate && r.Diff != "" {
			sawDiff = true
		}
	}
	if !sawDiff {
		t.Errorf("dry-run results carry no diff: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(home, "litellm", "gridctl-models.yaml")); err == nil {
		t.Fatal("dry-run wrote the fragment")
	}

	// Real sync writes the fragment and latches restart-pending.
	rec = doJSON(t, srv, http.MethodPost, "/api/project/models/sync", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sync status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	var doc struct {
		NeedsAttention bool               `json:"needs_attention"`
		Targets        []modelsync.Status `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var frag *modelsync.Status
	for i := range doc.Targets {
		if doc.Targets[i].Target == "litellm-fragment" {
			frag = &doc.Targets[i]
		}
	}
	if frag == nil || frag.State != "in-sync" || !frag.RestartPending {
		t.Fatalf("fragment row = %+v, want in-sync restart-pending", frag)
	}
	// Restart-pending alone is not attention: the engine contract the UI
	// mirrors.
	if doc.NeedsAttention {
		t.Error("needs_attention = true on restart-pending alone")
	}

	// Ack clears the latch.
	rec = doJSON(t, srv, http.MethodPost, "/api/project/models/ack-restart", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ack status = %d: %s", rec.Code, rec.Body.String())
	}
	var ack map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	if !ack["acknowledged"] {
		t.Errorf("ack body = %v", ack)
	}
	// A fresh doc: reusing the earlier one would keep stale values for
	// omitempty fields the new JSON no longer carries.
	rec = doJSON(t, srv, http.MethodGet, "/api/project/models/status", "")
	var after struct {
		Targets []modelsync.Status `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	for _, s := range after.Targets {
		if s.Target == "litellm-fragment" && s.RestartPending {
			t.Error("restart-pending survived ack-restart")
		}
	}
}

func TestHandleModelsSync_InvalidPolicyIs409WithIssues(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)
	// entry_model missing: a validation error, not a parse error.
	seedModelsPolicy(t, mgr, strings.Replace(modelsTestPolicy, "  entry_model: smart-router\n", "", 1))

	rec := doJSON(t, srv, http.MethodPost, "/api/project/models/sync", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error  string            `json:"error"`
		Issues []modelsync.Issue `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || len(body.Issues) == 0 {
		t.Errorf("409 body must carry the findings: %+v", body)
	}
}

func TestHandleModelsSync_NoPolicyIs404(t *testing.T) {
	srv, _, _ := setupModelsTestServer(t)
	rec := doJSON(t, srv, http.MethodPost, "/api/project/models/sync", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleModelsAdoptAndAck_NothingSyncedIs409(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)
	seedModelsPolicy(t, mgr, modelsTestPolicy)
	for _, path := range []string{"/api/project/models/adopt", "/api/project/models/ack-restart"} {
		rec := doJSON(t, srv, http.MethodPost, path, "")
		if rec.Code != http.StatusConflict {
			t.Errorf("%s status = %d, want 409: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestHandleModelsAdopt_RecordsDriftedFragment(t *testing.T) {
	srv, mgr, home := setupModelsTestServer(t)
	seedModelsPolicy(t, mgr, modelsTestPolicy)
	if rec := doJSON(t, srv, http.MethodPost, "/api/project/models/sync", ""); rec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body.String())
	}
	fragPath := filepath.Join(home, "litellm", "gridctl-models.yaml")
	if err := os.WriteFile(fragPath, []byte("# hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/project/models/adopt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt status = %d: %s", rec.Code, rec.Body.String())
	}
	var results []modelsync.AdoptResult
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	var adopted bool
	for _, r := range results {
		if r.Target == "litellm-fragment" && r.Action == modelsync.ActionAdopted {
			adopted = true
		}
	}
	if !adopted {
		t.Fatalf("adopt results = %+v", results)
	}
	// Adopt records hashes without touching the file.
	disk, err := os.ReadFile(fragPath)
	if err != nil || string(disk) != "# hand edit\n" {
		t.Errorf("adopt modified the fragment: %q, %v", disk, err)
	}
}

func TestHandleModelsValidate(t *testing.T) {
	srv, mgr, _ := setupModelsTestServer(t)

	// No policy: 404, matching the CLI's infrastructure error.
	rec := doJSON(t, srv, http.MethodGet, "/api/project/models/validate", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no-policy status = %d, want 404", rec.Code)
	}

	seedModelsPolicy(t, mgr, modelsTestPolicy)
	rec = doJSON(t, srv, http.MethodGet, "/api/project/models/validate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		PolicyPath string            `json:"policy_path"`
		Valid      bool              `json:"valid"`
		Issues     []modelsync.Issue `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Valid || doc.PolicyPath == "" {
		t.Errorf("doc = %+v", doc)
	}
	if doc.Issues == nil {
		t.Error("issues must be [] not null")
	}
}
