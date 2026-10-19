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
runs:
  enabled: true
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
	traces := getAuth(t, base+"/api/traces")
	if strings.Contains(traces, secret) {
		t.Fatal("traces leaked the secret")
	}
	runsBody := waitForRun(t, base, "echo")
	if strings.Contains(runsBody, secret) {
		t.Fatal("run record leaked the secret")
	}
	before := serverPID(t, base, "echo")
	quietOut, err := runGridctlEnv(ctx, t, bin, env, "reload", "--format", "json", "secret-demo")
	if err != nil {
		t.Fatalf("no-change reload: %v\n%s", err, quietOut)
	}
	if !strings.Contains(quietOut, "no changes detected") {
		t.Fatalf("no-change reload recreated the server:\n%s", quietOut)
	}
	if got := serverPID(t, base, "echo"); got != before || got == 0 {
		t.Fatalf("pid changed on no-change reload: before %d after %d", before, got)
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
	rotatedSum := sha256.Sum256([]byte(rotated))
	rotatedCall := postTool(t, base, "echo__fingerprint")
	if !strings.Contains(rotatedCall, hex.EncodeToString(rotatedSum[:])) {
		t.Fatalf("server did not see the rotated value: %s", rotatedCall)
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
	if !strings.Contains(reloadOut, "echo") {
		t.Fatalf("reload did not recover:\n%s", reloadOut)
	}
	failBase := "http://127.0.0.1:" + strconv.Itoa(failPort)
	waitReady(t, failBase)
	recoveredCall := postTool(t, failBase, "echo__fingerprint")
	if !strings.Contains(recoveredCall, want) {
		t.Fatalf("recovered server did not see the value: %s", recoveredCall)
	}
	servers := mcpServers(t, failBase)
	echoSrv, ok := servers["echo"]
	if !ok || echoSrv.RegistrationFailed || !echoSrv.Initialized {
		t.Fatalf("recovered server = %#v", echoSrv)
	}
	other, ok := servers["other"]
	if !ok || !other.Initialized || other.RegistrationFailed {
		t.Fatalf("other server = %#v", other)
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
	env := append(os.Environ(),
		"HOME="+home,
		"GRIDCTL_HOME="+home,
		"GRIDCTL_VAULT_PASSPHRASE=should-not-pass",
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"UNRELATED=nope",
		"OP_CONNECT_HOST=https://connect.example",
	)
	out, err := runGridctlEnv(ctx, t, bin, env, "var", "explain", "secret:op/vault/item/field", "--file", stackPath, "--format", "json")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"resolved": true`) || strings.Contains(out, "op-secret-value") {
		t.Fatalf("explain document: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "op-report"))
	if err != nil {
		t.Fatalf("stub op did not run: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "argv=read --no-newline op://vault/item/field") {
		t.Fatalf("argv = %s", text)
	}
	if !strings.Contains(text, "OP_SERVICE_ACCOUNT_TOKEN=service-account-token") || !strings.Contains(text, "OP_CONNECT_HOST=https://connect.example") {
		t.Fatalf("allowlisted env missing:\n%s", text)
	}
	for _, line := range []string{"UNRELATED=nope", "GRIDCTL_HOME=" + home, "GRIDCTL_VAULT_PASSPHRASE=should-not-pass"} {
		if strings.Contains(text, line) {
			t.Fatalf("child received %s:\n%s", line, text)
		}
	}
	if !strings.Contains(text, "UNRELATED=<unset>") || !strings.Contains(text, "GRIDCTL_HOME=<unset>") || !strings.Contains(text, "GRIDCTL_VAULT_PASSPHRASE=<unset>") {
		t.Fatalf("dropped keys were not absent:\n%s", text)
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

func TestSecretProviders_CadenceRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	script := filepath.Join(root, "tests/integration/fixtures/secret-provider/provider.sh")
	stackPath := filepath.Join(dir, "stack.yaml")
	secret := "provider-secret-value-9f3c"
	body := fmt.Sprintf(`version: "1"
name: secret-cadence
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
`, script, echo)
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "secret-cadence")
	})
	env := append(os.Environ(), "HOME="+home, "GRIDCTL_HOME="+home, "SECRET_PROVIDER_MARKER="+marker)
	out, err := runGridctlEnv(ctx, t, bin, env, "apply", stackPath, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "provider exited 1") {
		t.Fatalf("missing failure line:\n%s", out)
	}
	if err := os.WriteFile(marker, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	deadline := time.Now().Add(45 * time.Second)
	var echoSrv mcpServerStatus
	var ok bool
	for time.Now().Before(deadline) {
		echoSrv, ok = mcpServers(t, base)["echo"]
		if ok && echoSrv.Initialized && !echoSrv.RegistrationFailed {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !ok || !echoSrv.Initialized || echoSrv.RegistrationFailed {
		t.Fatalf("cadence did not recover: %#v", echoSrv)
	}
	sum := sha256.Sum256([]byte(secret))
	call := postTool(t, base, "echo__fingerprint")
	if !strings.Contains(call, hex.EncodeToString(sum[:])) {
		t.Fatalf("recovered call: %s", call)
	}
}

func TestSecretProviders_GatewayToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	token := "gateway-token-value-9f3c"
	if err := os.WriteFile(marker, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "tests/integration/fixtures/secret-provider/provider.sh")
	stackPath := filepath.Join(dir, "stack.yaml")
	body := fmt.Sprintf(`version: "1"
name: secret-gw
secrets:
  providers:
    - name: fake
      type: exec
      command: [%q, "{{path}}"]
      timeout: 10s
gateway:
  auth:
    type: bearer
    token: ${secret:fake/gw}
mcp-servers:
  - name: echo
    command: [%q]
`, script, echo)
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "secret-gw")
	})
	env := append(os.Environ(), "HOME="+home, "GRIDCTL_HOME="+home, "SECRET_PROVIDER_MARKER="+marker)
	out, err := runGridctlEnv(ctx, t, bin, env, "apply", stackPath, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	if status := authStatus(t, base+"/api/mcp-servers", ""); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", status)
	}
	if status := authStatus(t, base+"/api/mcp-servers", token); status != http.StatusOK {
		t.Fatalf("resolved token status = %d", status)
	}
	statusOut, err := runGridctlEnv(ctx, t, bin, env, "status", "secret-gw", "--json")
	if err != nil {
		t.Fatalf("status via state file: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, "secret-gw") {
		t.Fatalf("status: %s", statusOut)
	}
	if err := os.WriteFile(marker, []byte("rotated-gateway-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadOut, reloadErr := runGridctlEnv(ctx, t, bin, env, "reload", "--format", "json", "secret-gw")
	if reloadErr == nil || !strings.Contains(reloadOut, "restart_required") || !strings.Contains(reloadOut, "gateway.auth.token") {
		t.Fatalf("reload err=%v\n%s", reloadErr, reloadOut)
	}
	if status := authStatus(t, base+"/api/mcp-servers", token); status != http.StatusOK {
		t.Fatalf("listener dropped the startup token: %d", status)
	}
	if status := authStatus(t, base+"/api/mcp-servers", "rotated-gateway-token"); status != http.StatusUnauthorized {
		t.Fatalf("listener accepted the rotated token: %d", status)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	failPath := filepath.Join(dir, "fail.yaml")
	failBody := strings.Replace(body, "name: secret-gw", "name: secret-gw-fail", 1)
	if err := os.WriteFile(failPath, []byte(failBody), 0o644); err != nil {
		t.Fatal(err)
	}
	failOut, failErr := runGridctlEnv(ctx, t, bin, env, "apply", failPath, "--port", strconv.Itoa(freePort(t)))
	if failErr == nil || !strings.Contains(failOut, "secret fake/gw: provider exited 1") {
		t.Fatalf("failed gateway apply err=%v\n%s", failErr, failOut)
	}
}

func authStatus(t *testing.T, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode
}

func waitForRun(t *testing.T, base, server string) string {
	t.Helper()
	var body string
	deadline := time.Now().Add(8 * time.Second)
	for {
		body = getAuth(t, base+"/api/runs")
		if strings.Contains(body, server) && !strings.Contains(body, `"records": null`) && !strings.Contains(body, `"records":[]`) {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("run record for %s was not written:\n%s", server, body)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

type mcpServerStatus struct {
	Name               string `json:"name"`
	Initialized        bool   `json:"initialized"`
	RegistrationFailed bool   `json:"registrationFailed"`
	Replicas           []struct {
		PID int `json:"pid"`
	} `json:"replicas"`
}

func mcpServers(t *testing.T, base string) map[string]mcpServerStatus {
	t.Helper()
	body := getAuth(t, base+"/api/mcp-servers")
	var servers []mcpServerStatus
	if err := json.Unmarshal([]byte(body), &servers); err != nil {
		t.Fatalf("mcp-servers: %v\n%s", err, body)
	}
	out := make(map[string]mcpServerStatus, len(servers))
	for _, srv := range servers {
		out[srv.Name] = srv
	}
	return out
}

func serverPID(t *testing.T, base, name string) int {
	t.Helper()
	srv, ok := mcpServers(t, base)[name]
	if !ok || len(srv.Replicas) == 0 || srv.Replicas[0].PID == 0 {
		t.Fatalf("server %s has no pid", name)
	}
	return srv.Replicas[0].PID
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
