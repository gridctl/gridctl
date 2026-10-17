package reload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestReload_HiddenToolErrorsAndMissingVariableOmitsErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	hidden := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths"]
groups:
  ops:
    tools: ["vault__read_secret"]
`)
	if err := os.WriteFile(path, hidden, 0o600); err != nil {
		t.Fatal(err)
	}
	baseline := &config.Stack{Name: "active"}
	h := NewHandler(path, baseline, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	result, err := h.Reload(context.Background())
	if err != nil || result.Success || result.Code != "invalid_candidate" {
		t.Fatalf("reload = %+v err=%v", result, err)
	}
	if h.CurrentConfig() != baseline {
		t.Fatal("reload changed active config")
	}
	joined := strings.Join(result.Errors, "\n")
	want := "groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called"
	if !strings.Contains(joined, want) {
		t.Fatalf("errors = %#v", result.Errors)
	}

	initHandler := NewHandler("", nil, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	result, err = initHandler.Initialize(context.Background(), path)
	if err != nil || result.Success || result.Code != "invalid_candidate" || !strings.Contains(strings.Join(result.Errors, "\n"), want) {
		t.Fatalf("initialize = %+v err=%v", result, err)
	}

	secret := "resolved-secret-value"
	missing := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    headers:
      X-Tenant: "${vault:MISSING}"
      X-Note: "` + secret + `"
`)
	if err := os.WriteFile(path, missing, 0o600); err != nil {
		t.Fatal(err)
	}
	vaulted := NewHandler(path, baseline, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, emptyCandidateVault{}, nil)
	result, err = vaulted.Reload(context.Background())
	if err != nil || result.Success || result.Code != "invalid_candidate" {
		t.Fatalf("missing variable reload = %+v err=%v", result, err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("missing variable errors = %#v, want empty", result.Errors)
	}
	if strings.Contains(result.Message, secret) || strings.Contains(strings.Join(result.Errors, "\n"), secret) {
		t.Fatal("resolved value leaked")
	}
}

type emptyCandidateVault struct{}

func (emptyCandidateVault) Get(string) (string, bool) { return "", false }
