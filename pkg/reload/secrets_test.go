package reload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/secrets"
)

type stubResolver struct {
	byRef    map[string]string
	failures map[string]error
	err      error
	calls    int
}

func (s *stubResolver) ResolveStack(_ context.Context, stack *config.Stack) (*config.Stack, map[string]error, []string, error) {
	s.calls++
	if s.err != nil {
		return stack, s.failures, nil, s.err
	}
	clone := stack.Clone()
	config.SubstituteSecretRefs(clone, s.byRef)
	var values []string
	for _, ref := range stack.SecretRefs {
		if v, ok := s.byRef[ref.Key]; ok {
			values = append(values, v)
		}
	}
	if s.failures != nil {
		clone.MCPServers = filterServers(clone.MCPServers, s.failures)
	}
	return clone, s.failures, values, nil
}

func (s *stubResolver) Values() map[string]string { return s.byRef }

func TestReload_SecretRotationAndLiteralHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [\"echo\", \"{{path}}\"]\nmcp-servers:\n  - name: netbox\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:fake/alpha}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubResolver{byRef: map[string]string{"secret:fake/alpha": "same-value"}}
	h := NewHandler(path, loaded, mcp.NewGateway(), nil, 0, 0, nil, nil)
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver { return stub })
	h.SeedResolved(map[string]string{"secret:fake/alpha": "same-value"}, nil)
	var hooked *config.Stack
	h.SetOnConfigApplied(func(stack *config.Stack) { hooked = stack })
	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Message != "no changes detected" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Modified) != 0 {
		t.Fatalf("recreated on unchanged value: %#v", result.Modified)
	}

	stub.byRef = map[string]string{"secret:fake/alpha": "rotated-value"}
	result, err = h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(result.Modified, ",")
	if !strings.Contains(joined, "mcp-server:netbox (secret rotated)") {
		t.Fatalf("modified = %#v errors=%#v", result.Modified, result.Errors)
	}
	if hooked == nil || hooked.MCPServers[0].Env["TOKEN"] != "${secret:fake/alpha}" {
		t.Fatalf("hook saw resolved stack: %#v", hooked)
	}
	if h.CurrentConfig().MCPServers[0].Env["TOKEN"] != "${secret:fake/alpha}" {
		t.Fatal("currentCfg was resolved")
	}
}

func TestReload_GatewayTokenRotationRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [\"echo\", \"{{path}}\"]\ngateway:\n  auth:\n    type: bearer\n    token: ${secret:fake/gw}\nmcp-servers: []\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	startup := loaded.Clone()
	config.SubstituteSecretRefs(startup, map[string]string{"secret:fake/gw": "startup-token"})
	stub := &stubResolver{byRef: map[string]string{"secret:fake/gw": "rotated-token"}}
	h := NewHandler(path, loaded, mcp.NewGateway(), nil, 0, 0, nil, nil)
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver { return stub })
	h.SeedResolved(map[string]string{"secret:fake/gw": "startup-token"}, nil)
	h.SetSecurityPreflight(NewSecurityPreflight(startup, "", false))
	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "restart_required" || !containsString(result.ChangedFields, "gateway.auth.token") {
		t.Fatalf("result = %#v", result)
	}
	if h.CurrentConfig().Gateway.Auth.Token != "${secret:fake/gw}" {
		t.Fatal("currentCfg token was resolved")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
