package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

const testSkillMD = `---
name: %s
description: A test skill.
state: active
---

Body of the skill.
`

// setupSkillPinsServer stages a temp registry with one active skill plus a
// synced skill pin store, wired into an API server.
func setupSkillPinsServer(t *testing.T, skillNames ...string) (*Server, *skillpins.Store, *registry.Store) {
	t.Helper()
	regDir := t.TempDir()
	for _, name := range skillNames {
		dir := filepath.Join(regDir, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := strings.ReplaceAll(testSkillMD, "%s", name)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	ps := skillpins.NewWithPath(t.TempDir(), "test-stack")
	if _, err := ps.Sync(store); err != nil {
		t.Fatal(err)
	}

	server := NewServer(mcp.NewGateway(), nil)
	server.SetSkillPinStore(ps)
	server.SetRegistryServer(registry.New(store))
	return server, ps, store
}

func TestHandleSkillPins_NoStore(t *testing.T) {
	server := NewServer(mcp.NewGateway(), nil)

	req := loopbackRequest(http.MethodGet, "/api/skill-pins", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty object for polling UI)", w.Code)
	}
	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

func TestHandleSkillPins_ListAndGet(t *testing.T) {
	server, _, _ := setupSkillPinsServer(t, "alpha")

	req := loopbackRequest(http.MethodGet, "/api/skill-pins", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var listed map[string]*skillpins.SkillPin
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if pin := listed["alpha"]; pin == nil || pin.Status != skillpins.StatusPinned {
		t.Fatalf("listed = %+v, want pinned alpha", listed)
	}

	req = loopbackRequest(http.MethodGet, "/api/skill-pins/alpha", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}

	req = loopbackRequest(http.MethodGet, "/api/skill-pins/absent", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get absent status = %d, want 404", w.Code)
	}
}

func TestHandleSkillPinDiff_DriftAndApprove(t *testing.T) {
	server, _, store := setupSkillPinsServer(t, "alpha")

	// Edit the skill on disk and reload: pin drift.
	path := filepath.Join(store.Dir(), "skills", "alpha", "SKILL.md")
	content := strings.ReplaceAll(testSkillMD, "%s", "alpha") + "\nA new line.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	req := loopbackRequest(http.MethodGet, "/api/skill-pins/alpha/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("diff status = %d: %s", w.Code, w.Body.String())
	}
	var diff struct {
		Status        string `json:"status"`
		CompositeHash string `json:"composite_hash"`
		OldDocument   string `json:"old_document"`
		NewDocument   string `json:"new_document"`
	}
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Status != skillpins.StatusDrift {
		t.Fatalf("diff status = %q, want drift", diff.Status)
	}
	if diff.CompositeHash == "" || diff.OldDocument == "" || diff.NewDocument == "" {
		t.Fatalf("diff payload incomplete: %+v", diff)
	}

	// Stale expected hash: 409.
	body := strings.NewReader(`{"expected_hash":"stale"}`)
	req = loopbackRequest(http.MethodPost, "/api/skill-pins/alpha/approve", body)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale approve status = %d, want 409", w.Code)
	}

	// Correct hash: approved.
	body = strings.NewReader(`{"expected_hash":"` + diff.CompositeHash + `"}`)
	req = loopbackRequest(http.MethodPost, "/api/skill-pins/alpha/approve", body)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status = %d: %s", w.Code, w.Body.String())
	}

	// Diff is clean again.
	req = loopbackRequest(http.MethodGet, "/api/skill-pins/alpha/diff", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode post-approve diff: %v", err)
	}
	if diff.Status != skillpins.StatusPinned {
		t.Fatalf("post-approve status = %q, want pinned", diff.Status)
	}
}

func TestHandleSkillPinReset(t *testing.T) {
	server, ps, _ := setupSkillPinsServer(t, "alpha")

	req := loopbackRequest(http.MethodDelete, "/api/skill-pins/alpha", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d, want 204", w.Code)
	}
	if _, ok := ps.Get("alpha"); ok {
		t.Fatal("pin survived reset")
	}

	req = loopbackRequest(http.MethodDelete, "/api/skill-pins/alpha", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second reset status = %d, want 404", w.Code)
	}
}

func TestRegistrySkillsList_GovernanceFields(t *testing.T) {
	server, _, _ := setupSkillPinsServer(t, "alpha")
	server.gateway.SetSkillPolicy(mcp.NewSkillPolicy(&mcp.SkillPolicySpec{Deny: []string{"alpha"}}))

	req := loopbackRequest(http.MethodGet, "/api/registry/skills", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var items []struct {
		Name       string `json:"name"`
		Governance *struct {
			Source       string `json:"source"`
			PinStatus    string `json:"pinStatus"`
			PolicyDenied bool   `json:"policyDenied"`
			PolicyRule   string `json:"policyRule"`
		} `json:"governance"`
	}
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].Governance == nil {
		t.Fatalf("items = %+v, want alpha with governance", items)
	}
	g := items[0].Governance
	if g.Source != skillpins.SourceLocal || g.PinStatus != skillpins.StatusPinned {
		t.Fatalf("governance = %+v", g)
	}
	if !g.PolicyDenied || g.PolicyRule != "alpha" {
		t.Fatalf("policy verdict missing from governance: %+v", g)
	}
}
