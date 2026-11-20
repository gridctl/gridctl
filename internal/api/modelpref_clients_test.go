package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

func getAgentModelPreference(t *testing.T, srv *Server, path string) map[string]any {
	t.Helper()
	rec := doJSON(t, srv, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	var agent map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	pref, _ := agent["modelPreference"].(map[string]any)
	return pref
}

func TestAgentModelPreference_Clients(t *testing.T) {
	srv, registryDir, _ := setupAgentTestServer(t)
	seedAgent(t, registryDir, "reviewer", sampleAgentRaw)
	seedAgent(t, registryDir, "plain", "---\nname: plain\ndescription: Plain\n---\nBody.")

	// No stack loaded: no clients key, honor unchanged.
	pref := getAgentModelPreference(t, srv, "/api/registry/agents/reviewer")
	if _, ok := pref["clients"]; ok {
		t.Fatalf("clients must be absent without a policy: %+v", pref)
	}
	honor, _ := pref["honor"].(map[string]any)
	if honor["opencode"] != string(registry.HonorDropped) {
		t.Fatalf("honor changed: %+v", honor)
	}

	agents := &registry.ModelPolicy{
		Rewrite: true,
		Clients: map[string]registry.ClientModelPolicy{
			"opencode": {Default: "anthropic/claude-sonnet-5-5", Overrides: map[string]string{"reviewer": "anthropic/claude-opus-5-5#high"}},
			"gemini":   {Overrides: map[string]string{"reviewer": "inherit"}},
		},
	}
	srv.SetModelPolicyProvider(func() (*registry.ModelPolicy, *registry.ModelPolicy) { return &registry.ModelPolicy{}, agents })

	pref = getAgentModelPreference(t, srv, "/api/registry/agents/reviewer")
	clients, ok := pref["clients"].(map[string]any)
	if !ok {
		t.Fatalf("clients missing: %+v", pref)
	}
	want := map[string][2]string{
		"opencode": {"anthropic/claude-opus-5-5#high", "override"},
		"gemini":   {"inherit", "override"},
	}
	if len(clients) != len(want) {
		t.Fatalf("clients = %+v, want only targets with a value", clients)
	}
	for slug, w := range want {
		c, _ := clients[slug].(map[string]any)
		if c["value"] != w[0] || c["resolution"] != w[1] {
			t.Errorf("clients.%s = %+v, want %v", slug, c, w)
		}
	}
	honor, _ = pref["honor"].(map[string]any)
	if honor["opencode"] != string(registry.HonorDropped) || honor["claude-code"] != string(registry.HonorHonored) {
		t.Errorf("honor vocabulary must be unchanged: %+v", honor)
	}

	// A client-only configuration yields a view for an agent with no
	// declaration and no top-level resolution.
	pref = getAgentModelPreference(t, srv, "/api/registry/agents/plain")
	if pref == nil {
		t.Fatal("client-only policy must still yield a modelPreference view")
	}
	if _, ok := pref["declared"]; ok {
		t.Errorf("plain declares nothing: %+v", pref)
	}
	clients, _ = pref["clients"].(map[string]any)
	c, _ := clients["opencode"].(map[string]any)
	if c["value"] != "anthropic/claude-sonnet-5-5" || c["resolution"] != "default" {
		t.Errorf("plain clients.opencode = %+v", c)
	}

	// The list endpoint carries the same view.
	rec := doJSON(t, srv, http.MethodGet, "/api/registry/agents", "")
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		pref, _ := item["modelPreference"].(map[string]any)
		if _, ok := pref["clients"]; !ok {
			t.Errorf("list item %v lacks clients", item["name"])
		}
	}

	// Block absent (a loaded stack without clients): key omitted again.
	srv.SetModelPolicyProvider(func() (*registry.ModelPolicy, *registry.ModelPolicy) {
		return &registry.ModelPolicy{}, &registry.ModelPolicy{}
	})
	pref = getAgentModelPreference(t, srv, "/api/registry/agents/reviewer")
	if _, ok := pref["clients"]; ok {
		t.Fatalf("clients must be omitted when the block is absent: %+v", pref)
	}
	if pref := getAgentModelPreference(t, srv, "/api/registry/agents/plain"); pref != nil {
		t.Fatalf("nothing declared or resolved: modelPreference must stay omitted, got %+v", pref)
	}
}
