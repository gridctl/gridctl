package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	agentruntime "github.com/gridctl/gridctl/pkg/agent/runtime"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
)

// agentDevSkillProject scaffolds a tmpdir with a single SKILL.md +
// skill.ts pair so devserver.NewServer can parse a non-empty skill
// list. Returns the absolute project root.
func agentDevSkillProject(t *testing.T, skillName string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+skillName+"\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.ts"), []byte("await tool(\"x\");\n"), 0o644); err != nil {
		t.Fatalf("write skill.ts: %v", err)
	}
	return root
}

func TestHandleAgentDev_Unwired_Returns503(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := body["error"], "agent dev server not configured"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestHandleAgentDev_LegacyWired_Delegates(t *testing.T) {
	root := agentDevSkillProject(t, "legacy")
	dev, err := devserver.NewServer(root, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	srv := newTestServer(t)
	srv.SetAgentDevServer(dev)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Skills []devserver.SkillEntry `json:"skills"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Skills) != 1 || body.Skills[0].Name != "legacy" {
		t.Fatalf("skills = %+v, want one skill named legacy", body.Skills)
	}
}

func TestHandleAgentDev_RuntimeWired_PrefersRuntime(t *testing.T) {
	runtimeRoot := agentDevSkillProject(t, "runtime-skill")
	legacyRoot := agentDevSkillProject(t, "legacy-skill")

	runtimeDev, err := devserver.NewServer(runtimeRoot, nil)
	if err != nil {
		t.Fatalf("runtime NewServer: %v", err)
	}
	legacyDev, err := devserver.NewServer(legacyRoot, nil)
	if err != nil {
		t.Fatalf("legacy NewServer: %v", err)
	}

	rt := agentruntime.NewRuntime(
		persist.NewStore(t.TempDir()),
		compose.NewRegistry(),
		sandbox.New(0),
	)
	rt.SetDevServer(runtimeDev)

	srv := newTestServer(t)
	srv.SetAgentDevServer(legacyDev)
	srv.SetAgentRuntime(rt)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Skills []devserver.SkillEntry `json:"skills"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Skills) != 1 || body.Skills[0].Name != "runtime-skill" {
		t.Fatalf("skills = %+v, want runtime-skill (runtime aggregate must win over legacy setter)", body.Skills)
	}
}
