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
