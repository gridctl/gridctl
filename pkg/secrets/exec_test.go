package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
)

func TestExec_ArgvTrimAndEnvFilter(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "show.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$1\"\nprintf 'GRID=%s\\n' \"${GRIDCTL_VAULT_PASSPHRASE-}\"\nprintf 'OP=%s\\n' \"${OP_SERVICE_ACCOUNT_TOKEN-}\"\nprintf 'KEEP=%s\\n' \"${KEEP_ME-}\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "vault-secret")
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "op-secret")
	t.Setenv("OP_CONNECT_TOKEN", "connect-secret")
	t.Setenv("KEEP_ME", "visible")
	provider := NewExec(config.SecretProvider{
		Name:    "fake",
		Type:    "exec",
		Command: []string{script, "path={{path}}"},
	}, os.Environ())
	got, err := provider.Resolve(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "path=alpha") || strings.Contains(got, "vault-secret") || strings.Contains(got, "op-secret") || strings.Contains(got, "connect-secret") {
		t.Fatalf("output = %q", got)
	}
	if !strings.Contains(got, "KEEP=visible") {
		t.Fatalf("filtered a non-reserved variable: %q", got)
	}
}

func TestExec_EmptyTimeoutAndTooLarge(t *testing.T) {
	provider := NewExec(config.SecretProvider{
		Name:    "fake",
		Type:    "exec",
		Command: []string{"sh", "-c", "printf ''"},
		Timeout: "1s",
	}, nil)
	_, err := provider.Resolve(context.Background(), "alpha")
	var secretErr *Error
	if !errors.As(err, &secretErr) || secretErr.Category != "empty value" || strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %v", err)
	}

	big := NewExec(config.SecretProvider{
		Name:    "fake",
		Type:    "exec",
		Command: []string{"sh", "-c", "head -c 1048577 /dev/zero"},
		Timeout: "10s",
	}, nil)
	_, err = big.Resolve(context.Background(), "alpha")
	if !errors.As(err, &secretErr) || secretErr.Category != "output too large" {
		t.Fatalf("err = %v", err)
	}
}

func TestExec_Timeout(t *testing.T) {
	provider := NewExec(config.SecretProvider{
		Name:    "fake",
		Type:    "exec",
		Command: []string{"sh", "-c", "sleep 5"},
		Timeout: "1s",
	}, nil)
	_, err := provider.Resolve(context.Background(), "alpha")
	var secretErr *Error
	if !errors.As(err, &secretErr) || !strings.HasPrefix(secretErr.Category, "timeout after") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout should unwrap to DeadlineExceeded")
	}
}

func TestExec_NotFound(t *testing.T) {
	provider := NewExec(config.SecretProvider{
		Name:    "fake",
		Type:    "exec",
		Command: []string{"gridctl-missing-provider-bin", "{{path}}"},
	}, nil)
	_, err := provider.Resolve(context.Background(), "alpha")
	var secretErr *Error
	if !errors.As(err, &secretErr) || !strings.Contains(secretErr.Category, "not found on PATH") {
		t.Fatalf("err = %v", err)
	}
}

func TestOnePassword_PathForms(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"vault/item/field", true},
		{"vault/item/section/field", true},
		{"vault/item", false},
		{"a/b/c/d/e", false},
		{"vault//field", false},
		{"op://vault/item/field", false},
	}
	for _, tc := range cases {
		err := validateOPPath(tc.path)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.path, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected invalid path", tc.path)
		}
	}
}

func TestOnePassword_TokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode and symlink checks are POSIX")
	}
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("service-account-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readTokenFile(token)
	if err != nil || got != "service-account-token" {
		t.Fatalf("token = %q err=%v", got, err)
	}
	logging.RegisterSensitiveValues(got)
	if !strings.Contains(logging.RedactString("saw service-account-token here"), "[REDACTED]") {
		t.Fatal("token file content was not registered")
	}

	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(loose); err == nil || !strings.Contains(err.Error(), "token file mode") {
		t.Fatalf("mode err = %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(token, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(link); err == nil || !strings.Contains(err.Error(), "token file symlink") {
		t.Fatalf("symlink err = %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	nested := filepath.Join(home, "op-token")
	if err := os.WriteFile(nested, []byte("from-home\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = readTokenFile("~/op-token")
	if err != nil || got != "from-home" {
		t.Fatalf("tilde token = %q err=%v", got, err)
	}
}

func TestOnePassword_Allowlist(t *testing.T) {
	env := filterOPEnv([]string{
		"PATH=/usr/bin",
		"HOME=/home/me",
		"OP_ACCOUNT=team",
		"OP_CONNECT_TOKEN=keep",
		"SECRET=nope",
		"GRIDCTL_VAULT_PASSPHRASE=nope",
	})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SECRET=") || strings.Contains(joined, "GRIDCTL_") {
		t.Fatalf("allowlist leaked: %q", joined)
	}
	if !strings.Contains(joined, "OP_CONNECT_TOKEN=keep") || !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatalf("allowlist dropped required keys: %q", joined)
	}
}

func TestResolver_ExcludesFailedWorkload(t *testing.T) {
	dir := t.TempDir()
	okScript := filepath.Join(dir, "ok.sh")
	badScript := filepath.Join(dir, "bad.sh")
	if err := os.WriteFile(okScript, []byte("#!/bin/sh\nprintf 'good-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badScript, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := &config.Stack{
		Name: "demo",
		Secrets: SecretsForTest(
			config.SecretProvider{Name: "good", Type: "exec", Command: []string{okScript, "{{path}}"}},
			config.SecretProvider{Name: "bad", Type: "exec", Command: []string{badScript, "{{path}}"}},
		),
		MCPServers: []config.MCPServer{
			{Name: "ok", Env: map[string]string{"TOKEN": "${secret:good/alpha}"}},
			{Name: "broken", Env: map[string]string{"TOKEN": "${secret:bad/alpha}"}},
		},
	}
	expandForTest(stack)
	resolver := NewResolver(stack.Secrets.Providers, nil)
	resolved, failures, values, err := resolver.ResolveStack(context.Background(), stack)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.MCPServers) != 1 || resolved.MCPServers[0].Name != "ok" {
		t.Fatalf("servers = %#v", resolved.MCPServers)
	}
	if resolved.MCPServers[0].Env["TOKEN"] != "good-value" {
		t.Fatalf("substituted = %q", resolved.MCPServers[0].Env["TOKEN"])
	}
	if stack.MCPServers[0].Env["TOKEN"] != "${secret:good/alpha}" {
		t.Fatal("literal stack was mutated")
	}
	if failures["broken"] == nil || !strings.Contains(failures["broken"].Error(), "provider exited 1") {
		t.Fatalf("failures = %#v", failures)
	}
	if len(values) != 1 || values[0] != "good-value" {
		t.Fatalf("values = %#v", values)
	}
}

func SecretsForTest(providers ...config.SecretProvider) *config.Secrets {
	return &config.Secrets{Providers: providers}
}

func expandForTest(stack *config.Stack) {
	// Index secret refs the same way the loader does, without importing loader internals.
	stack.References = config.ReferenceIndex{}
	stack.SecretRefs = nil
	for _, srv := range stack.MCPServers {
		for key, value := range srv.Env {
			if !strings.Contains(value, "${secret:") {
				continue
			}
			refKey := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
			stack.SecretRefs = append(stack.SecretRefs, config.SecretRef{Provider: strings.Split(strings.TrimPrefix(refKey, "secret:"), "/")[0], Path: strings.SplitN(strings.TrimPrefix(refKey, "secret:"), "/", 2)[1], Key: refKey})
			stack.References[refKey] = []config.Consumer{{Kind: config.RefKindMCPServer, Name: srv.Name, Field: "env." + key}}
		}
	}
	if stack.Gateway != nil && stack.Gateway.Auth != nil && strings.Contains(stack.Gateway.Auth.Token, "${secret:") {
		refKey := strings.TrimSuffix(strings.TrimPrefix(stack.Gateway.Auth.Token, "${"), "}")
		provider, path, _ := config.ParseSecretRefKey(refKey)
		stack.SecretRefs = append(stack.SecretRefs, config.SecretRef{Provider: provider, Path: path, Key: refKey})
		stack.References[refKey] = []config.Consumer{{Kind: config.RefKindGateway, Field: "auth.token"}}
	}
}

func TestResolver_StackFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "bad.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := &config.Stack{
		Name:    "demo",
		Secrets: &config.Secrets{Providers: []config.SecretProvider{{Name: "fake", Type: "exec", Command: []string{script, "{{path}}"}}}},
		Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{Token: "${secret:fake/gw}"}},
	}
	expandForTest(stack)
	_, failures, _, err := NewResolver(stack.Secrets.Providers, nil).ResolveStack(context.Background(), stack)
	if err == nil || failures["stack"] == nil {
		t.Fatalf("err=%v failures=%#v", err, failures)
	}
	if strings.Contains(err.Error(), "stdout") {
		t.Fatalf("error leaked output: %v", err)
	}
}

func TestResolver_CacheOneProcess(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	script := filepath.Join(dir, "once.sh")
	body := "#!/bin/sh\necho x >> \"" + count + "\"\nprintf 'value\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver([]config.SecretProvider{{Name: "fake", Type: "exec", Command: []string{script, "{{path}}"}}}, nil)
	ref := Reference{Provider: "fake", Path: "alpha"}
	var wgErr error
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			_, err := resolver.Resolve(context.Background(), ref)
			if err != nil {
				wgErr = err
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if wgErr != nil {
		t.Fatal(wgErr)
	}
	data, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "x") != 1 {
		t.Fatalf("subprocess count = %q", data)
	}
	_ = time.Second
}
