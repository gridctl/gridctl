//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const vaultSessionPass = "vault-session-passphrase" // #nosec G101 -- test fixture

// vaultSessionEnv is the child environment: the parent's, with both
// passphrase variables and the home variables removed, then home set.
func vaultSessionEnv(home string, extra ...string) []string {
	skip := map[string]bool{
		"HOME": true, "GRIDCTL_HOME": true,
		"GRIDCTL_VAULT_PASSPHRASE": true, "GRIDCTL_VAULT_PASSPHRASE_FILE": true,
		"GRIDCTL_GATEWAY_TOKEN": true,
	}
	env := make([]string, 0, len(os.Environ())+3+len(extra))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if skip[key] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+home, "GRIDCTL_HOME="+home, "NO_COLOR=1")
	return append(env, extra...)
}

type cliResult struct {
	stdout, stderr string
	code           int
	elapsed        time.Duration
}

func runCLI(ctx context.Context, t *testing.T, bin string, env []string, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	res := cliResult{stdout: stdout.String(), stderr: stderr.String(), elapsed: time.Since(start)}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, res.stderr)
		}
		res.code = exit.ExitCode()
	}
	return res
}

type varStatusBody struct {
	Locked       bool   `json:"locked"`
	Encrypted    bool   `json:"encrypted"`
	UnlockSource string `json:"unlock_source"`
}

func getVarStatus(t *testing.T, port int) varStatusBody {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/var/status", port))
	if err != nil {
		t.Fatalf("GET /api/var/status: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/var/status = %d: %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte(vaultSessionPass)) {
		t.Fatal("status body contains the passphrase")
	}
	var body varStatusBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return body
}

func daemonPIDFromState(t *testing.T, home, name string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".gridctl", "state", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(vaultSessionPass)) {
		t.Fatal("state file contains the passphrase")
	}
	var st struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st.PID
}

func writeVaultStack(t *testing.T, dir, name, mock string) string {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	body := fmt.Sprintf("version: \"1\"\nname: %s\nmcp-servers:\n  - name: echo\n    command: [%q]\n    env:\n      DEMO_TOKEN: \"${var:DEMO_TOKEN}\"\n", name, mock)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// lockedVaultHome stores DEMO_TOKEN and encrypts the store with the test
// passphrase through the CLI.
func lockedVaultHome(ctx context.Context, t *testing.T, bin, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if r := runCLI(ctx, t, bin, vaultSessionEnv(home), nil, "var", "set", "DEMO_TOKEN", "--value", "stored-token"); r.code != 0 {
		t.Fatalf("var set exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if r := runCLI(ctx, t, bin, vaultSessionEnv(home, "GRIDCTL_VAULT_PASSPHRASE="+vaultSessionPass), nil, "var", "lock"); r.code != 0 {
		t.Fatalf("var lock exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "vault", "secrets.enc")); err != nil {
		t.Fatalf("store not encrypted: %v", err)
	}
}

// TestVaultSession_PassphraseStdinHandOff starts a daemon with the
// passphrase piped to apply --passphrase-stdin and both passphrase
// variables absent, then checks the child's store, environment, argv, and
// log. No container runtime is required.
func TestVaultSession_PassphraseStdinHandOff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Fatal("mock stdio server was not built")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd", "gridctl"), filepath.Join(dir, "gridctl"))
	home := filepath.Join(dir, "home")
	lockedVaultHome(ctx, t, bin, home)
	env := vaultSessionEnv(home)

	const name = "vault-session"
	stackPath := writeVaultStack(t, dir, name, mockStdioBin)
	port := freePort(t)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = runCLI(cleanupCtx, t, bin, env, nil, "destroy", stackPath)
	})

	t.Run("locked store without a terminal", func(t *testing.T) {
		r := runCLI(ctx, t, bin, env, nil, "apply", stackPath, "--port", strconv.Itoa(port), "--quiet")
		if r.code != 1 {
			t.Fatalf("exit = %d, want 1\n%s\n%s", r.code, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stderr, "variable store is locked") || !strings.Contains(r.stderr, "\n  Or run gridctl apply in a terminal to be prompted") {
			t.Fatalf("stderr = %s", r.stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "state", name+".json")); !os.IsNotExist(err) {
			t.Fatalf("a daemon started: %v", err)
		}
	})

	t.Run("empty pipe", func(t *testing.T) {
		r := runCLI(ctx, t, bin, env, strings.NewReader(""), "apply", stackPath, "--port", strconv.Itoa(port), "--passphrase-stdin", "--quiet")
		if r.code == 0 || !strings.Contains(r.stderr, "stdin") {
			t.Fatalf("exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
		}
	})

	t.Run("pipe held open", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pr.Close()
		defer pw.Close()
		r := runCLI(ctx, t, bin, env, pr, "apply", stackPath, "--port", strconv.Itoa(port), "--passphrase-stdin", "--quiet")
		if r.code == 0 || !strings.Contains(r.stderr, "no passphrase arrived on stdin within 10s") {
			t.Fatalf("exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
		}
		if r.elapsed > 20*time.Second {
			t.Fatalf("apply took %s", r.elapsed)
		}
	})

	r := runCLI(ctx, t, bin, env, strings.NewReader(vaultSessionPass+"\n"), "apply", stackPath, "--port", strconv.Itoa(port), "--passphrase-stdin", "--quiet")
	if r.code != 0 {
		t.Fatalf("apply exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, out := range []string{r.stdout, r.stderr} {
		if strings.Contains(out, vaultSessionPass) {
			t.Fatal("apply output contains the passphrase")
		}
	}

	readyClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := readyClient.Get(fmt.Sprintf("http://127.0.0.1:%d/ready", port))
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ready = %d", resp.StatusCode)
	}

	st := getVarStatus(t, port)
	if st.Locked || !st.Encrypted || st.UnlockSource != "stdin" {
		t.Fatalf("var status = %+v, want locked:false encrypted:true unlock_source:stdin", st)
	}

	pid := daemonPIDFromState(t, home, name)
	if runtime.GOOS == "linux" {
		environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(environ, []byte("GRIDCTL_VAULT_PASSPHRASE")) || bytes.Contains(environ, []byte(vaultSessionPass)) {
			t.Fatal("daemon child environment carries a passphrase variable")
		}
	}
	cmdline, err := daemonChildCommand(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if cmdline == "" || !strings.Contains(cmdline, "--passphrase-stdin") {
		t.Fatalf("daemon child argv = %q, want --passphrase-stdin", cmdline)
	}
	if strings.Contains(cmdline, vaultSessionPass) {
		t.Fatal("daemon child argv contains the passphrase")
	}

	logRaw, err := os.ReadFile(filepath.Join(home, ".gridctl", "logs", name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logRaw, []byte(vaultSessionPass)) {
		t.Fatal("daemon log contains the passphrase")
	}

	if r := runCLI(ctx, t, bin, env, nil, "destroy", stackPath); r.code != 0 {
		t.Fatalf("destroy exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

// TestVaultSession_ServePassphraseStdin starts the stackless daemon with the
// passphrase piped to serve --passphrase-stdin.
func TestVaultSession_ServePassphraseStdin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd", "gridctl"), filepath.Join(dir, "gridctl"))
	home := filepath.Join(dir, "home")
	lockedVaultHome(ctx, t, bin, home)
	env := vaultSessionEnv(home)
	port := freePort(t)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = runCLI(cleanupCtx, t, bin, env, nil, "stop")
	})

	r := runCLI(ctx, t, bin, env, strings.NewReader(vaultSessionPass+"\n"), "serve", "--port", strconv.Itoa(port), "--passphrase-stdin")
	if r.code != 0 {
		t.Fatalf("serve exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	st := getVarStatus(t, port)
	if st.Locked || !st.Encrypted || st.UnlockSource != "stdin" {
		t.Fatalf("var status = %+v", st)
	}
	if r := runCLI(ctx, t, bin, env, nil, "stop"); r.code != 0 {
		t.Fatalf("stop exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

// statusVariables reads the per-gateway variables objects from
// gridctl status --json, keyed by stack name.
func statusVariables(ctx context.Context, t *testing.T, bin string, env []string) map[string]*varStatusBody {
	t.Helper()
	r := runCLI(ctx, t, bin, env, nil, "status", "--json")
	if r.code != 0 {
		t.Fatalf("status exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	var report struct {
		Gateways []struct {
			Name      string         `json:"name"`
			Variables *varStatusBody `json:"variables"`
		} `json:"gateways"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &report); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, r.stdout)
	}
	out := map[string]*varStatusBody{}
	for _, g := range report.Gateways {
		out[g.Name] = g.Variables
	}
	return out
}

// TestVaultSession_RelockAndUnlockAcrossGateways runs two daemons for one
// home, ends their sessions with POST /api/var/relock and gridctl var lock,
// and starts them again with gridctl var unlock. No container runtime is
// required for the daemons.
func TestVaultSession_RelockAndUnlockAcrossGateways(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Fatal("mock stdio server was not built")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd", "gridctl"), filepath.Join(dir, "gridctl"))
	home := filepath.Join(dir, "home")
	lockedVaultHome(ctx, t, bin, home)
	env := vaultSessionEnv(home)
	encPath := filepath.Join(home, ".gridctl", "vault", "secrets.enc")

	names := []string{"session-a", "session-b"}
	ports := map[string]int{}
	stacks := map[string]string{}
	for _, name := range names {
		stacks[name] = writeVaultStack(t, dir, name, mockStdioBin)
		ports[name] = freePort(t)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, name := range names {
			_ = runCLI(cleanupCtx, t, bin, env, nil, "destroy", stacks[name])
		}
	})
	for _, name := range names {
		r := runCLI(ctx, t, bin, env, strings.NewReader(vaultSessionPass+"\n"), "apply", stacks[name], "--port", strconv.Itoa(ports[name]), "--passphrase-stdin", "--quiet")
		if r.code != 0 {
			t.Fatalf("apply %s exit = %d\n%s\n%s", name, r.code, r.stdout, r.stderr)
		}
		if st := getVarStatus(t, ports[name]); st.Locked || st.UnlockSource != "stdin" {
			t.Fatalf("%s status = %+v", name, st)
		}
	}
	encBefore, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEncUnchanged := func(step string) {
		t.Helper()
		after, err := os.ReadFile(encPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encBefore, after) {
			t.Fatalf("secrets.enc changed after %s", step)
		}
	}
	noPassphrase := func(step string, r cliResult) {
		t.Helper()
		if strings.Contains(r.stdout+r.stderr, vaultSessionPass) {
			t.Fatalf("%s output contains the passphrase", step)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}

	// POST /api/var/relock on one daemon ends its session in memory only.
	portA := ports["session-a"]
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/api/var/relock", portA), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != `{"status":"locked"}` {
		t.Fatalf("relock = %d %s", resp.StatusCode, raw)
	}
	resp, err = client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/var", portA))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("GET /api/var after relock = %d, want 423", resp.StatusCode)
	}
	assertEncUnchanged("POST /api/var/relock")

	// var lock on an encrypted store prompts for nothing, rewrites nothing,
	// and locks both gateways.
	r := runCLI(ctx, t, bin, env, nil, "var", "lock")
	noPassphrase("var lock", r)
	if r.code != 0 {
		t.Fatalf("var lock exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, name := range names {
		if !strings.Contains(r.stdout, fmt.Sprintf("%s (%d)", name, ports[name])) {
			t.Fatalf("var lock output missing %s:\n%s", name, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "Locked in 2 running gateway(s)") {
		t.Fatalf("var lock output = %q", r.stdout)
	}
	assertEncUnchanged("var lock")
	vars := statusVariables(ctx, t, bin, env)
	for _, name := range names {
		if v := vars[name]; v == nil || !v.Locked || !v.Encrypted {
			t.Fatalf("status variables for %s = %+v, want locked", name, v)
		}
	}
	if table := runCLI(ctx, t, bin, env, nil, "status", "--plain"); !strings.Contains(table.stdout, "locked") {
		t.Fatalf("status table:\n%s", table.stdout)
	}

	// var unlock posts the verified passphrase to both gateways.
	withPass := vaultSessionEnv(home, "GRIDCTL_VAULT_PASSPHRASE="+vaultSessionPass)
	r = runCLI(ctx, t, bin, withPass, nil, "var", "unlock")
	noPassphrase("var unlock", r)
	if r.code != 0 {
		t.Fatalf("var unlock exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, name := range names {
		if !strings.Contains(r.stdout, fmt.Sprintf("%s (%d): unlocked", name, ports[name])) {
			t.Fatalf("var unlock output missing %s:\n%s", name, r.stdout)
		}
		if st := getVarStatus(t, ports[name]); st.Locked || st.UnlockSource != "cli" {
			t.Fatalf("%s status after unlock = %+v", name, st)
		}
	}
	vars = statusVariables(ctx, t, bin, env)
	for _, name := range names {
		if v := vars[name]; v == nil || v.Locked || v.UnlockSource != "cli" {
			t.Fatalf("status variables for %s = %+v, want unlocked (cli)", name, v)
		}
	}
	table := runCLI(ctx, t, bin, env, nil, "status", "--plain")
	if strings.Count(table.stdout, "unlocked (cli)") != 2 {
		t.Fatalf("status table:\n%s", table.stdout)
	}

	r = runCLI(ctx, t, bin, withPass, nil, "var", "unlock", "--format", "json")
	noPassphrase("var unlock --format json", r)
	if r.code != 0 {
		t.Fatalf("var unlock --format json exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	var doc struct {
		SchemaVersion int  `json:"schema_version"`
		Verified      bool `json:"verified"`
		Gateways      []struct {
			Stack  string `json:"stack"`
			Port   int    `json:"port"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"gateways"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("var unlock JSON: %v\n%s", err, r.stdout)
	}
	if doc.SchemaVersion != 1 || !doc.Verified || len(doc.Gateways) != 2 {
		t.Fatalf("var unlock JSON = %+v", doc)
	}
	for _, g := range doc.Gateways {
		if g.Port != ports[g.Stack] || g.Status != "already_unlocked" || g.Error != "" {
			t.Fatalf("gateway entry = %+v", g)
		}
	}

	if r := runCLI(ctx, t, bin, env, nil, "doctor", "--json"); !strings.Contains(r.stdout, "unlocked in session-a (cli), session-b (cli)") {
		t.Fatalf("doctor vault message missing gateway sessions:\n%s", r.stdout)
	}
	assertEncUnchanged("var unlock")

	for _, name := range names {
		if r := runCLI(ctx, t, bin, env, nil, "destroy", stacks[name]); r.code != 0 {
			t.Fatalf("destroy %s exit = %d\n%s\n%s", name, r.code, r.stdout, r.stderr)
		}
	}

	// With no gateway running, the session verbs report it and succeed.
	r = runCLI(ctx, t, bin, withPass, nil, "var", "unlock")
	if r.code != 0 || !strings.Contains(r.stdout, "No running gateway. Passphrase verified") {
		t.Fatalf("var unlock without gateways exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	r = runCLI(ctx, t, bin, env, nil, "var", "lock")
	if r.code != 0 || !strings.Contains(r.stdout, "no running gateway") {
		t.Fatalf("var lock without gateways exit = %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	assertEncUnchanged("var lock without gateways")
}
