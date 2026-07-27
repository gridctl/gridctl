package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestBuildVariableUsage(t *testing.T) {
	t.Run("nil stack yields empty non-nil map", func(t *testing.T) {
		got := buildVariableUsage(nil)
		if got == nil || len(got) != 0 {
			t.Fatalf("got %v, want empty non-nil map", got)
		}
	})

	t.Run("nil references yields empty map", func(t *testing.T) {
		got := buildVariableUsage(&config.Stack{Name: "s"})
		if len(got) != 0 {
			t.Fatalf("got %v, want empty map", got)
		}
	})

	t.Run("references are passed through", func(t *testing.T) {
		spec := &config.Stack{References: config.ReferenceIndex{
			"TOKEN": {{Kind: config.RefKindMCPServer, Name: "github", Field: "env.TOKEN"}},
		}}
		got := buildVariableUsage(spec)
		if len(got["TOKEN"]) != 1 || got["TOKEN"][0].Name != "github" {
			t.Fatalf("got %v, want TOKEN used by github", got)
		}
	})
}

// writeStackWithState points the daemon state for stackName at a freshly written
// stack file under an isolated HOME, so loadRunningSpec resolves to it.
func writeStackWithState(t *testing.T, stackName, stackYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	stackPath := filepath.Join(home, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(stackYAML), 0600); err != nil {
		t.Fatalf("write stack: %v", err)
	}
	if err := state.Save(&state.DaemonState{StackName: stackName, StackFile: stackPath}); err != nil {
		t.Fatalf("save state: %v", err)
	}
}

func getUsage(t *testing.T, server *Server) (int, map[string][]config.Consumer) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/var/usage", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var body map[string][]config.Consumer
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, body
}

func TestHandleVariableUsage_NoStackLoaded(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // empty state dir → no running spec
	server := &Server{stackName: "ghost"}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(body) != 0 {
		t.Fatalf("body = %v, want empty object", body)
	}
}

const usageStackYAML = `name: test
mcp-servers:
  - name: github
    url: https://api.example.com
    env:
      GITHUB_TOKEN: "${var:GITHUB_TOKEN}"
`

func TestHandleVariableUsage_ReturnsConsumers(t *testing.T) {
	writeStackWithState(t, "test", usageStackYAML)
	server := &Server{stackName: "test"}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	consumers := body["GITHUB_TOKEN"]
	if len(consumers) != 1 {
		t.Fatalf("GITHUB_TOKEN consumers = %v, want 1", consumers)
	}
	c := consumers[0]
	if c.Kind != config.RefKindMCPServer || c.Name != "github" || c.Field != "env.GITHUB_TOKEN" {
		t.Errorf("consumer = %+v, want {mcp-server github env.GITHUB_TOKEN}", c)
	}
}

const setInjectionStackYAML = `name: test
secrets:
  sets:
    - dev
mcp-servers:
  - name: github
    url: https://api.example.com
    env:
      GITHUB_TOKEN: "${var:GITHUB_TOKEN}"
`

// newSetVault returns an unlocked store whose set "dev" holds the given keys.
func newSetVault(t *testing.T, keys ...string) *vault.Store {
	t.Helper()
	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("store Load(): %v", err)
	}
	for _, key := range keys {
		if err := store.Set(key, "value-of-"+key); err != nil {
			t.Fatalf("store Set(%s): %v", key, err)
		}
		if err := store.SetSecretSet(key, "dev"); err != nil {
			t.Fatalf("store SetSecretSet(%s): %v", key, err)
		}
	}
	return store
}

func TestHandleVariableUsage_SynthesizesSetConsumers(t *testing.T) {
	writeStackWithState(t, "test", setInjectionStackYAML)
	server := &Server{stackName: "test", vaultStore: newSetVault(t, "ANTHROPIC_API_KEY", "ZAPIER_TOKEN")}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ZAPIER_TOKEN"} {
		consumers := body[key]
		if len(consumers) != 1 {
			t.Fatalf("%s consumers = %v, want 1 synthetic consumer", key, consumers)
		}
		c := consumers[0]
		if c.Kind != config.RefKindSecretsSet || c.Name != "dev" || c.Field != "secrets.sets" {
			t.Errorf("%s consumer = %+v, want {secrets-set dev secrets.sets}", key, c)
		}
	}
	// Values must never leak into the usage payload.
	if raw, _ := json.Marshal(body); strings.Contains(string(raw), "value-of-") {
		t.Fatal("response leaked a secret value")
	}
}

func TestHandleVariableUsage_ExplicitAndSetConsumersCombine(t *testing.T) {
	writeStackWithState(t, "test", setInjectionStackYAML)
	server := &Server{stackName: "test", vaultStore: newSetVault(t, "GITHUB_TOKEN")}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	consumers := body["GITHUB_TOKEN"]
	if len(consumers) != 2 {
		t.Fatalf("GITHUB_TOKEN consumers = %v, want explicit + synthetic", consumers)
	}
	kinds := map[config.ReferenceKind]bool{}
	for _, c := range consumers {
		kinds[c.Kind] = true
	}
	if !kinds[config.RefKindMCPServer] || !kinds[config.RefKindSecretsSet] {
		t.Errorf("consumer kinds = %v, want mcp-server and secrets-set", kinds)
	}
}

func TestHandleVariableUsage_NoSyntheticConsumersWhenVaultLocked(t *testing.T) {
	writeStackWithState(t, "test", setInjectionStackYAML)

	vaultDir := t.TempDir()
	writer := vault.NewStore(vaultDir)
	if err := writer.Load(); err != nil {
		t.Fatalf("writer Load(): %v", err)
	}
	if err := writer.Set("ANTHROPIC_API_KEY", "sk-secret"); err != nil {
		t.Fatalf("writer Set(): %v", err)
	}
	if err := writer.SetSecretSet("ANTHROPIC_API_KEY", "dev"); err != nil {
		t.Fatalf("writer SetSecretSet(): %v", err)
	}
	if err := writer.Lock("passphrase"); err != nil {
		t.Fatalf("writer Lock(): %v", err)
	}
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		t.Fatalf("store Load(): %v", err)
	}

	server := &Server{stackName: "test", vaultStore: store}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when vault locked", code)
	}
	// Locked vault degrades to explicit references only: the ${var:} site is
	// still reported, the set member is not.
	if len(body["GITHUB_TOKEN"]) != 1 {
		t.Fatalf("GITHUB_TOKEN consumers = %v, want 1", body["GITHUB_TOKEN"])
	}
	if len(body["ANTHROPIC_API_KEY"]) != 0 {
		t.Fatalf("ANTHROPIC_API_KEY consumers = %v, want none while locked", body["ANTHROPIC_API_KEY"])
	}
}

func TestHandleVariableUsage_NoSecretsSetsBlock(t *testing.T) {
	writeStackWithState(t, "test", usageStackYAML)
	server := &Server{stackName: "test", vaultStore: newSetVault(t, "ANTHROPIC_API_KEY")}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(body["ANTHROPIC_API_KEY"]) != 0 {
		t.Fatalf("ANTHROPIC_API_KEY consumers = %v, want none without secrets.sets", body["ANTHROPIC_API_KEY"])
	}
}

// The usage index is derived from the stack file, not the vault, so a locked
// vault must not turn the endpoint into a 423 or hide the data.
func TestHandleVariableUsage_SafeWhenVaultLocked(t *testing.T) {
	writeStackWithState(t, "test", usageStackYAML)

	vaultDir := t.TempDir()
	writer := vault.NewStore(vaultDir)
	if err := writer.Load(); err != nil {
		t.Fatalf("writer Load(): %v", err)
	}
	if err := writer.Set("GITHUB_TOKEN", "super-secret"); err != nil {
		t.Fatalf("writer Set(): %v", err)
	}
	if err := writer.Lock("passphrase"); err != nil {
		t.Fatalf("writer Lock(): %v", err)
	}

	// A fresh instance over the encrypted dir loads in the locked state (no
	// passphrase supplied) — this is what the running server would hold.
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		t.Fatalf("store Load(): %v", err)
	}
	if !store.IsLocked() {
		t.Fatal("store should be locked")
	}

	server := &Server{stackName: "test", vaultStore: store}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when vault locked", code)
	}
	if len(body["GITHUB_TOKEN"]) != 1 {
		t.Fatalf("GITHUB_TOKEN consumers = %v, want 1", body["GITHUB_TOKEN"])
	}
	// No secret values may appear anywhere in the response.
	if raw, _ := json.Marshal(body); strings.Contains(string(raw), "super-secret") {
		t.Fatal("response leaked a secret value")
	}
}

// ---------------------------------------------------------------------------
// Scoped secrets.sets
// ---------------------------------------------------------------------------

const scopedSetStackYAML = `name: test
secrets:
  sets:
    - name: dev
      servers:
        - github
mcp-servers:
  - name: github
    url: https://api.example.com
  - name: playwright
    url: https://pw.example.com
`

func TestHandleVariableUsage_ScopedSetNamesItsTargets(t *testing.T) {
	writeStackWithState(t, "test", scopedSetStackYAML)
	store := newSetVault(t, "API_KEY")
	server := &Server{stackName: "test", vaultStore: store}

	code, body := getUsage(t, server)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	consumers := body["API_KEY"]
	if len(consumers) != 1 {
		t.Fatalf("API_KEY consumers = %v, want exactly the one scoped target", consumers)
	}
	c := consumers[0]
	if c.Kind != config.RefKindSecretsSet {
		t.Errorf("kind = %q, want secrets-set", c.Kind)
	}
	// Name must stay the set name: the inspector's unassign confirmation
	// compares it against the variable's own set.
	if c.Name != "dev" {
		t.Errorf("name = %q, want the set name 'dev'", c.Name)
	}
	if c.Target != "github" || c.TargetKind != config.RefKindMCPServer {
		t.Errorf("target = %q/%q, want github/mcp-server", c.Target, c.TargetKind)
	}
}

func TestHandleVariableUsage_UnscopedSetHasNoTarget(t *testing.T) {
	writeStackWithState(t, "test", setInjectionStackYAML)
	store := newSetVault(t, "API_KEY")
	server := &Server{stackName: "test", vaultStore: store}

	_, body := getUsage(t, server)
	consumers := body["API_KEY"]
	if len(consumers) != 1 {
		t.Fatalf("API_KEY consumers = %v, want 1", consumers)
	}
	if consumers[0].Target != "" || consumers[0].TargetKind != "" {
		t.Errorf("unscoped consumer carries a target: %+v", consumers[0])
	}
}

func TestSetConsumersFor(t *testing.T) {
	spec := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "a"}, {Name: "b"}},
		Resources:  []config.Resource{{Name: "r"}},
	}

	t.Run("unscoped yields one untargeted consumer", func(t *testing.T) {
		got := setConsumersFor(config.SecretSetRef{Name: "dev"}, spec)
		if len(got) != 1 || got[0].Target != "" {
			t.Fatalf("got %+v, want a single untargeted consumer", got)
		}
	})

	t.Run("scoped yields one consumer per named workload", func(t *testing.T) {
		got := setConsumersFor(config.SecretSetRef{
			Name: "dev", Servers: []string{"a", "b"}, Resources: []string{"r"},
		}, spec)
		if len(got) != 3 {
			t.Fatalf("got %d consumers, want 3", len(got))
		}
		targets := map[string]config.ReferenceKind{}
		for _, c := range got {
			targets[c.Target] = c.TargetKind
		}
		if targets["a"] != config.RefKindMCPServer || targets["r"] != config.RefKindResource {
			t.Errorf("targets = %v", targets)
		}
	})

	t.Run("scope naming nothing real yields nothing", func(t *testing.T) {
		got := setConsumersFor(config.SecretSetRef{
			Name: "dev", Servers: []string{"ghost"},
		}, spec)
		if len(got) != 0 {
			t.Fatalf("got %+v, want no consumers for a scope that matches no workload", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Drift
// ---------------------------------------------------------------------------

const driftStackYAML = `name: test
mcp-servers:
  - name: github
    url: https://api.example.com
    auth:
      type: bearer
      token: "${var:MISSING_KEY}"
    env:
      PRESENT: "${var:PRESENT_KEY}"
      DEFAULTED: "${var:OPTIONAL_KEY:-fallback}"
`

func getDrift(t *testing.T, server *Server) (int, []driftEntry) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/var/drift", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var body []driftEntry
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, body
}

func TestHandleVariableDrift(t *testing.T) {
	t.Run("reports only keys the store cannot satisfy", func(t *testing.T) {
		writeStackWithState(t, "test", driftStackYAML)
		store := newSetVault(t, "PRESENT_KEY")
		server := &Server{stackName: "test", vaultStore: store}

		code, body := getDrift(t, server)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if len(body) != 1 {
			t.Fatalf("drift = %+v, want exactly MISSING_KEY", body)
		}
		if body[0].Key != "MISSING_KEY" {
			t.Fatalf("drift key = %q, want MISSING_KEY", body[0].Key)
		}
		if len(body[0].Consumers) != 1 || body[0].Consumers[0].Field != "auth.token" {
			t.Errorf("consumers = %+v, want the auth.token site", body[0].Consumers)
		}
	})

	t.Run("a defaulted reference is never drift", func(t *testing.T) {
		writeStackWithState(t, "test", driftStackYAML)
		store := newSetVault(t, "PRESENT_KEY", "MISSING_KEY")
		server := &Server{stackName: "test", vaultStore: store}

		_, body := getDrift(t, server)
		for _, e := range body {
			if e.Key == "OPTIONAL_KEY" {
				t.Error("${var:KEY:-default} reported as drift; it resolves to its default")
			}
		}
		if len(body) != 0 {
			t.Errorf("drift = %+v, want none once every hard reference is stored", body)
		}
	})

	t.Run("no stack yields an empty list", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		server := &Server{stackName: "ghost"}
		code, body := getDrift(t, server)
		if code != http.StatusOK || len(body) != 0 {
			t.Fatalf("status = %d, drift = %+v", code, body)
		}
	})
}

func TestBuildVariableDrift_LockedOrAbsentStore(t *testing.T) {
	spec := &config.Stack{
		UnresolvedRefs: []string{"ANY"},
		References:     config.ReferenceIndex{"ANY": {{Kind: config.RefKindStack, Field: "name"}}},
	}

	if got := buildVariableDrift(spec, nil); len(got) != 0 {
		t.Errorf("nil store drift = %+v, want empty (membership is uncheckable)", got)
	}

	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := store.Set("ANY", "v"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := store.Lock("passphrase-long-enough"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if got := buildVariableDrift(spec, store); len(got) != 0 {
		t.Errorf("locked store drift = %+v, want empty rather than everything", got)
	}
}

// TestHandleVariableDrift_SatisfiedByEnvironment pins the subtlest case: the
// deploy-time resolver reads the store first and the process environment
// second, so a key the environment supplies is not a deploy failure and must
// not be reported as drift even though no stored variable holds it.
func TestHandleVariableDrift_SatisfiedByEnvironment(t *testing.T) {
	t.Setenv("ENV_ONLY_KEY", "from-environment")
	writeStackWithState(t, "test", `name: test
mcp-servers:
  - name: github
    url: https://api.example.com
    env:
      A: "${var:ENV_ONLY_KEY}"
      B: "${var:NEITHER_KEY}"
`)
	store := newSetVault(t) // empty store
	server := &Server{stackName: "test", vaultStore: store}

	_, body := getDrift(t, server)
	keys := make([]string, 0, len(body))
	for _, e := range body {
		keys = append(keys, e.Key)
	}
	if len(keys) != 1 || keys[0] != "NEITHER_KEY" {
		t.Fatalf("drift = %v, want only NEITHER_KEY (ENV_ONLY_KEY resolves from the environment)", keys)
	}
}

// TestVariableDriftRoute_NotMirroredOnDeprecatedPath pins the canonical-only
// decision: the deprecated /api/vault surface is frozen, so "drift" must fall
// through to the {key} lookup there rather than serving the new endpoint.
func TestVariableDriftRoute_NotMirroredOnDeprecatedPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := &Server{stackName: "ghost"}

	req := httptest.NewRequest(http.MethodGet, "/api/vault/drift", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK && strings.HasPrefix(w.Body.String(), "[") {
		t.Error("/api/vault/drift served the drift list; new endpoints are canonical-only")
	}
}
