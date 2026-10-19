//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSecretProviders_ExecApplyReloadAndRedaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	echo := buildRaceBinary(t, ctx, filepath.Join(root, "tests/integration/fixtures/secret-echo"), filepath.Join(dir, "secret-echo"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	secret := "provider-secret-value-9f3c"
	if err := os.WriteFile(marker, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "tests/integration/fixtures/secret-provider/provider.sh")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	body := fmt.Sprintf(`version: "1"
name: secret-demo
secrets:
  providers:
    - name: fake
      type: exec
      command: [%q, "{{path}}"]
      timeout: 10s
mcp-servers:
  - name: echo
    command: [%q]
    env:
      SECRET_VALUE: ${secret:fake/alpha}
  - name: other
    command: [%q]
`, script, echo, echo)
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "secret-demo")
	})
	env := append(os.Environ(), "HOME="+home, "GRIDCTL_HOME="+home, "SECRET_PROVIDER_MARKER="+marker)
	out, err := runGridctlEnv(ctx, t, bin, env, "apply", stackPath, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	sum := sha256.Sum256([]byte(secret))
	want := hex.EncodeToString(sum[:])
	call := postTool(t, base, "echo__fingerprint")
	if !strings.Contains(call, want) {
		t.Fatalf("tool result missing hash: %s", call)
	}
	if strings.Contains(call, secret) {
		t.Fatal("tool result included the secret")
	}
	logs := getAuth(t, base+"/api/logs")
	if strings.Contains(logs, secret) {
		t.Fatalf("logs leaked the secret: %s", logs)
	}
	if !strings.Contains(logs, "[REDACTED]") {
		t.Fatalf("logs did not redact stderr: %s", logs)
	}
	for _, path := range []string{"/api/traces", "/api/runs"} {
		body := getAuth(t, base+path)
		if strings.Contains(body, secret) {
			t.Fatalf("%s leaked the secret", path)
		}
	}
	exportOut, err := runGridctlEnv(ctx, t, bin, env, "export", stackPath)
	if err != nil {
		t.Fatalf("export: %v\n%s", err, exportOut)
	}
	if !strings.Contains(exportOut, "${secret:fake/alpha}") || !strings.Contains(exportOut, "providers:") {
		t.Fatalf("export dropped the reference:\n%s", exportOut)
	}
	rotated := "provider-secret-value-rotated"
	if err := os.WriteFile(marker, []byte(rotated+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planOut, planErr := runGridctlEnv(ctx, t, bin, env, "plan", stackPath)
	if planErr != nil && !strings.Contains(planOut, "No changes") && !strings.Contains(planOut, "no changes") {
		t.Fatalf("plan: %v\n%s", planErr, planOut)
	}
	if strings.Contains(planOut, secret) || strings.Contains(planOut, rotated) {
		t.Fatalf("plan printed a secret value:\n%s", planOut)
	}
	rotateOut, err := runGridctlEnv(ctx, t, bin, env, "reload", "--format", "json", "secret-demo")
	if err != nil {
		t.Fatalf("rotate reload: %v\n%s", err, rotateOut)
	}
	if !strings.Contains(rotateOut, "secret rotated") {
		t.Fatalf("reload did not report rotation:\n%s", rotateOut)
	}
	if err := os.WriteFile(marker, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	failPath := filepath.Join(dir, "fail.yaml")
	failBody := strings.Replace(body, "name: secret-demo", "name: secret-fail", 1)
	if err := os.WriteFile(failPath, []byte(failBody), 0o644); err != nil {
		t.Fatal(err)
	}
	failPort := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "secret-fail")
	})
	failOut, failErr := runGridctlEnv(ctx, t, bin, env, "apply", failPath, "--port", strconv.Itoa(failPort))
	if failErr != nil {
		t.Fatalf("failing apply exited: %v\n%s", failErr, failOut)
	}
	if !strings.Contains(failOut, "echo: secret fake/alpha: provider exited 1") {
		t.Fatalf("missing failure line:\n%s", failOut)
	}
	statusOut, err := runGridctlEnv(ctx, t, bin, env, "status", "secret-fail", "--json")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, `"registrationFailed": true`) || !strings.Contains(statusOut, "provider exited 1") {
		t.Fatalf("status missing failure:\n%s", statusOut)
	}
	if err := os.WriteFile(marker, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadOut, err := runGridctlEnv(ctx, t, bin, env, "reload", "secret-fail")
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, reloadOut)
	}
	if !strings.Contains(reloadOut, "secret rotated") && !strings.Contains(reloadOut, "echo") {
		t.Fatalf("reload did not recover:\n%s", reloadOut)
	}
}

func TestSecretProviders_StubOp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	op := buildRaceBinary(t, ctx, filepath.Join(root, "tests/integration/fixtures/stub-op"), filepath.Join(dir, "op"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("service-account-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	body := fmt.Sprintf(`version: "1"
name: op-demo
secrets:
  providers:
    - name: op
      type: onepassword
      token_file: %q
mcp-servers:
  - name: demo
    command: ["true"]
    env:
      TOKEN: ${secret:op/vault/item/field}
`, token)
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+home, "GRIDCTL_HOME="+home, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "UNRELATED=nope", "OP_CONNECT_HOST=https://connect.example")
	out, err := runGridctlEnv(ctx, t, bin, env, "var", "explain", "secret:op/vault/item/field", "--file", stackPath, "--format", "json")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"resolved": true`) || strings.Contains(out, "op-secret-value") {
		t.Fatalf("explain document: %s", out)
	}
	_ = op
}

func runGridctlEnv(ctx context.Context, t *testing.T, bin string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String() + stderr.String(), err
}

func postTool(t *testing.T, base, name string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"arguments":{}}`, name)
	req, err := http.NewRequest(http.MethodPost, base+"/api/tools/call", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("call %s: %s", resp.Status, data)
	}
	return string(data)
}

func getAuth(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	_ = json.Unmarshal(data, &probe)
	return string(data)
}
