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

func TestReload_ExcludesFailedServerAndRecoversAsAdded(t *testing.T) {
	path, loaded := writeSecretStack(t, secretStackBody("echo", false))
	gw := mcp.NewGateway()
	stub := &stubResolver{failures: map[string]error{"echo": errString("secret fake/alpha: provider exited 1")}}
	h := NewHandler(path, loaded, gw, nil, 0, 0, nil, nil)
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver { return stub })
	h.SeedResolved(nil, nil)
	var started []config.MCPServer
	h.SetRegisterServerFunc(func(_ context.Context, server config.MCPServer, _ []ReplicaRuntime, _ string) error {
		started = append(started, server)
		return nil
	})
	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || !strings.Contains(strings.Join(result.Errors, " "), "provider exited 1") {
		t.Fatalf("result = %#v", result)
	}
	if len(started) != 0 {
		t.Fatalf("failed server was started: %#v", started)
	}
	if !h.HasUnresolved() {
		t.Fatal("failure was not retained")
	}
	failed := false
	for _, st := range gw.Status() {
		if st.Name == "echo" && st.RegistrationFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatal("registration failure was not recorded")
	}

	stub.failures = nil
	stub.byRef = map[string]string{"secret:fake/alpha": "recovered-value"}
	result, err = h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.Added, ","), "mcp-server:echo") {
		t.Fatalf("added = %#v errors=%#v", result.Added, result.Errors)
	}
	if len(started) != 1 || started[0].Env["TOKEN"] != "recovered-value" {
		t.Fatalf("started = %#v", started)
	}
	if h.HasUnresolved() {
		t.Fatal("recovered server stayed unresolved")
	}
	if h.CurrentConfig().MCPServers[0].Env["TOKEN"] != "${secret:fake/alpha}" {
		t.Fatal("currentCfg was resolved")
	}
}

func TestReload_ClearsUnresolvedWhenReferencesLeave(t *testing.T) {
	path, loaded := writeSecretStack(t, secretStackBody("echo", false))
	h := NewHandler(path, loaded, mcp.NewGateway(), nil, 0, 0, nil, nil)
	calls := 0
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver {
		calls++
		return &stubResolver{}
	})
	h.SeedResolved(nil, map[string]error{"echo": errString("secret fake/alpha: provider exited 1")})
	body := "version: \"1\"\nname: demo\nmcp-servers:\n  - name: echo\n    command: [\"true\"]\n    env:\n      TOKEN: literal\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var started int
	h.SetRegisterServerFunc(func(context.Context, config.MCPServer, []ReplicaRuntime, string) error {
		started++
		return nil
	})
	if _, err := h.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.HasUnresolved() {
		t.Fatal("unresolved survived a stack with no secret references")
	}
	if calls != 0 {
		t.Fatalf("resolver ran for a stack with no references: %d", calls)
	}
	if _, still := h.RetrySecrets(context.Background()); h.HasUnresolved() || len(still) != 0 {
		t.Fatal("retry kept a stale entry")
	}
	if started != 1 {
		t.Fatalf("env edit started the server %d times, want 1", started)
	}
}

func TestRetrySecrets_DropsGoneParkedAndRefless(t *testing.T) {
	path, loaded := writeSecretStack(t, secretStackBody("echo", true))
	h := NewHandler(path, loaded, mcp.NewGateway(), nil, 0, 0, nil, nil)
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver {
		t.Fatal("resolver ran for a dropped workload")
		return nil
	})
	h.SeedResolved(nil, map[string]error{
		"gone":    errString("secret fake/alpha: provider exited 1"),
		"echo":    errString("secret fake/alpha: provider exited 1"),
		"refless": errString("secret fake/alpha: provider exited 1"),
	})
	h.currentCfg.MCPServers = append(h.currentCfg.MCPServers, config.MCPServer{Name: "refless", Command: []string{"true"}})
	var started int
	h.SetRegisterServerFunc(func(context.Context, config.MCPServer, []ReplicaRuntime, string) error {
		started++
		return nil
	})
	recovered, still := h.RetrySecrets(context.Background())
	if len(recovered) != 0 || len(still) != 0 || h.HasUnresolved() || started != 0 {
		t.Fatalf("recovered=%v still=%v unresolved=%v started=%d", recovered, still, h.HasUnresolved(), started)
	}
}

func TestRetrySecrets_RecoversAndPublishesResolvedServer(t *testing.T) {
	path, loaded := writeSecretStack(t, secretStackBody("echo", false))
	h := NewHandler(path, loaded, mcp.NewGateway(), nil, 0, 0, nil, nil)
	other := loaded.Clone()
	other.MCPServers = []config.MCPServer{{Name: "other", Command: []string{"true"}}}
	h.SeedResolvedStack(other)
	h.SetSecretResolverFactory(func(*config.Stack) secrets.StackResolver {
		return &stubResolver{byRef: map[string]string{"secret:fake/alpha": "now-ready"}}
	})
	h.SeedResolved(nil, map[string]error{"echo": errString("secret fake/alpha: provider exited 1")})
	var started config.MCPServer
	h.SetRegisterServerFunc(func(_ context.Context, server config.MCPServer, _ []ReplicaRuntime, _ string) error {
		started = server
		return nil
	})
	var published *config.Stack
	h.SetOnResolvedApplied(func(stack *config.Stack) { published = stack })
	recovered, still := h.RetrySecrets(context.Background())
	if len(recovered) != 1 || recovered[0] != "echo" || len(still) != 0 {
		t.Fatalf("recovered=%v still=%v", recovered, still)
	}
	if started.Env["TOKEN"] != "now-ready" {
		t.Fatalf("started with %q", started.Env["TOKEN"])
	}
	if published == nil || len(published.MCPServers) != 2 {
		t.Fatalf("published = %#v", published)
	}
	found := false
	for _, srv := range published.MCPServers {
		if srv.Name == "echo" && srv.Env["TOKEN"] == "now-ready" {
			found = true
		}
		if srv.Name == "other" && srv.Env["TOKEN"] != "" {
			t.Fatal("published stack lost the other server or rewrote it")
		}
	}
	if !found {
		t.Fatal("recovered server was not merged into the resolved stack")
	}
}

func writeSecretStack(t *testing.T, body string) (string, *config.Stack) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, loaded
}

func secretStackBody(name string, parked bool) string {
	enabled := ""
	if parked {
		enabled = "    enabled: false\n"
	}
	return "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [\"echo\", \"{{path}}\"]\nmcp-servers:\n  - name: " + name + "\n" + enabled + "    command: [\"true\"]\n    env:\n      TOKEN: ${secret:fake/alpha}\n"
}

type errString string

func (e errString) Error() string { return string(e) }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
