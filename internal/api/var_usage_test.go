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
