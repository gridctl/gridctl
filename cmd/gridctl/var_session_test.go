package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/spf13/cobra"
)

const sessionPass = "session-passphrase-long" // #nosec G101 -- test fixture

// fakeGateway is an httptest daemon registered through a temp state file.
type fakeGateway struct {
	mu       sync.Mutex
	name     string
	port     int
	requests []string // "METHOD /path body"
	status   gatewayVarStatus
	// respond overrides the session routes when set.
	respond func(w http.ResponseWriter, r *http.Request) bool
}

func (g *fakeGateway) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

func (g *fakeGateway) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	g.mu.Lock()
	g.requests = append(g.requests, r.Method+" "+r.URL.Path+" "+string(body))
	g.mu.Unlock()
	if g.respond != nil && g.respond(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	g.mu.Lock()
	defer g.mu.Unlock()
	switch r.Method + " " + r.URL.Path {
	case "GET /api/var/status":
		_ = json.NewEncoder(w).Encode(g.status)
	case "POST /api/var/unlock":
		if !g.status.Locked {
			fmt.Fprint(w, `{"status":"already_unlocked"}`)
			return
		}
		g.status.Locked, g.status.UnlockSource = false, vault.UnlockSourceCLI
		fmt.Fprint(w, `{"status":"unlocked"}`)
	case "POST /api/var/relock":
		if g.status.Locked {
			fmt.Fprint(w, `{"status":"already_locked"}`)
			return
		}
		g.status.Locked, g.status.UnlockSource = true, ""
		fmt.Fprint(w, `{"status":"locked"}`)
	default:
		http.NotFound(w, r)
	}
}

// startFakeGateway serves a fake daemon and saves a running state file for
// it. mutate may adjust the state before it is saved.
func startFakeGateway(t *testing.T, name string, status gatewayVarStatus, mutate func(*state.DaemonState)) *fakeGateway {
	t.Helper()
	g := &fakeGateway{name: name, status: status}
	srv := httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	g.port, err = strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	st := &state.DaemonState{StackName: name, StackFile: "/tmp/" + name + ".yaml", PID: os.Getpid(), Port: g.port}
	if mutate != nil {
		mutate(st)
	}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}
	return g
}

// sessionHome creates a temp home with an encrypted store and no
// passphrase variables, and points stdin at a non-terminal.
func sessionHome(t *testing.T) string {
	t.Helper()
	dir := lockedVarStore(t, sessionPass)
	t.Setenv(gatewayTokenEnv, "")
	nonTTYStdin(t)
	return dir
}

func lockedGatewayStatus() gatewayVarStatus { return gatewayVarStatus{Encrypted: true, Locked: true} }

func TestRunningGateways_FiltersStoppedAndSortsByPort(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	for _, st := range []*state.DaemonState{
		{StackName: "b", PID: os.Getpid(), Port: 9200},
		{StackName: "stale", PID: 0, Port: 9100},
		{StackName: "a", PID: os.Getpid(), Port: 9150},
	} {
		if err := state.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	got, err := runningGateways()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].StackName != "a" || got[1].StackName != "b" {
		t.Fatalf("runningGateways = %+v", got)
	}
}

func TestGatewayVarStatusLabel(t *testing.T) {
	for _, tc := range []struct {
		in   gatewayVarStatus
		want string
	}{
		{gatewayVarStatus{}, "plaintext"},
		{gatewayVarStatus{Encrypted: true, Locked: true}, "locked"},
		{gatewayVarStatus{Encrypted: true}, "unlocked"},
		{gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, "unlocked (stdin)"},
		{gatewayVarStatus{Encrypted: true, UnlockSource: "cli"}, "unlocked (cli)"},
		{gatewayVarStatus{Encrypted: true, UnlockSource: "api"}, "unlocked (api)"},
	} {
		if got := tc.in.label(); got != tc.want {
			t.Errorf("label(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunVarUnlockSession_UnlocksEveryGateway(t *testing.T) {
	sessionHome(t)
	daily := startFakeGateway(t, "daily", lockedGatewayStatus(), nil)
	demo := startFakeGateway(t, "policy-demo", gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, nil)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)

	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Passphrase verified",
		fmt.Sprintf("daily (%d): unlocked", daily.port),
		fmt.Sprintf("policy-demo (%d): already unlocked", demo.port),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out+stderr.String(), sessionPass) {
		t.Fatal("output carries the passphrase")
	}
	reqs := daily.seen()
	if len(reqs) != 1 || !strings.HasPrefix(reqs[0], "POST /api/var/unlock ") {
		t.Fatalf("daily requests = %v", reqs)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(reqs[0], "POST /api/var/unlock ")), &body); err != nil {
		t.Fatal(err)
	}
	if body["passphrase"] != sessionPass || body["source"] != "cli" {
		t.Fatalf("unlock body = %v", body)
	}
}

func TestRunVarUnlockSession_JSONShape(t *testing.T) {
	sessionHome(t)
	daily := startFakeGateway(t, "daily", lockedGatewayStatus(), nil)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)

	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "json"); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if doc["schema_version"] != float64(1) || doc["verified"] != true {
		t.Fatalf("doc = %v", doc)
	}
	gws, _ := doc["gateways"].([]any)
	if len(gws) != 1 {
		t.Fatalf("gateways = %v", doc["gateways"])
	}
	gw := gws[0].(map[string]any)
	if gw["stack"] != "daily" || gw["port"] != float64(daily.port) || gw["status"] != "unlocked" {
		t.Fatalf("gateway = %v", gw)
	}
	if _, ok := gw["error"]; ok {
		t.Fatalf("successful gateway carries error: %v", gw)
	}
}

func TestRunVarUnlockSession_FailedGatewayStillAttemptsOthers(t *testing.T) {
	sessionHome(t)
	oauth := startFakeGateway(t, "oauth", lockedGatewayStatus(), func(st *state.DaemonState) {
		st.AuthType = "oauth"
		st.AuthIssuerHost = "issuer.example"
	})
	oauth.respond = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Gridctl-Auth-Rejected", "1")
		w.WriteHeader(http.StatusUnauthorized)
		return true
	}
	broken := startFakeGateway(t, "broken", lockedGatewayStatus(), nil)
	broken.respond = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"Variable store not available"}`)
		return true
	}
	good := startFakeGateway(t, "good", lockedGatewayStatus(), nil)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)

	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	out := stdout.String()
	for _, want := range []string{
		fmt.Sprintf("oauth (%d): failed: unauthorized; set GRIDCTL_GATEWAY_TOKEN", oauth.port),
		fmt.Sprintf("broken (%d): failed: 503 Variable store not available", broken.port),
		fmt.Sprintf("good (%d): unlocked", good.port),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	stdout.Reset()
	good.mu.Lock()
	good.status.Locked = true
	good.mu.Unlock()
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "json"); code != 1 {
		t.Fatalf("json exit = %d, want 1", code)
	}
	var doc varUnlockDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Verified || len(doc.Gateways) != 3 {
		t.Fatalf("doc = %+v", doc)
	}
	failed := 0
	for _, g := range doc.Gateways {
		if g.Status == "failed" {
			failed++
			if g.Error == "" {
				t.Errorf("failed gateway without error: %+v", g)
			}
		}
	}
	if failed != 2 {
		t.Fatalf("failed = %d: %+v", failed, doc.Gateways)
	}
}

func TestRunVarUnlockSession_UnreachableGatewayFails(t *testing.T) {
	sessionHome(t)
	gone := startFakeGateway(t, "gone", lockedGatewayStatus(), nil)
	gone.respond = func(w http.ResponseWriter, r *http.Request) bool {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return true
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)
	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("gone (%d): failed: unreachable", gone.port)) {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunVarUnlockSession_WrongPassphraseContactsNoGateway(t *testing.T) {
	sessionHome(t)
	daily := startFakeGateway(t, "daily", lockedGatewayStatus(), nil)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "not-the-passphrase")

	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "wrong passphrase or corrupted vault") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if reqs := daily.seen(); len(reqs) != 0 {
		t.Fatalf("gateway contacted on a wrong passphrase: %v", reqs)
	}

	stdout.Reset()
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "json"); code != 1 {
		t.Fatalf("json exit = %d, want 1", code)
	}
	var doc varUnlockDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Verified || len(doc.Gateways) != 0 {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestRunVarUnlockSession_NoPassphraseWithoutTerminal(t *testing.T) {
	sessionHome(t)
	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "GRIDCTL_VAULT_PASSPHRASE") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunVarUnlockSession_NoRunningGateway(t *testing.T) {
	sessionHome(t)
	if err := state.Save(&state.DaemonState{StackName: "stale", PID: 0, Port: 9}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)
	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != noGatewayUnlockNotice {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunVarUnlockSession_PlaintextStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	store := vault.NewStore(filepath.Join(home, ".gridctl", "vault"))
	if err := store.Set("K", "v"); err != nil {
		t.Fatal(err)
	}
	daily := startFakeGateway(t, "daily", gatewayVarStatus{}, nil)
	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "not encrypted") {
		t.Fatalf("output = %q", stdout.String())
	}
	if reqs := daily.seen(); len(reqs) != 0 {
		t.Fatalf("plaintext unlock contacted a gateway: %v", reqs)
	}
}

func TestRunVarUnlockSession_UnreadableInputsExitTwo(t *testing.T) {
	t.Run("store", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GRIDCTL_HOME", home)
		dir := filepath.Join(home, ".gridctl", "vault")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 2 {
			t.Fatalf("exit = %d, want 2 (stderr %q)", code, stderr.String())
		}
	})
	t.Run("state directory", func(t *testing.T) {
		sessionHome(t)
		stateDir, err := state.StateDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
			t.Fatal(err)
		}
		_ = os.RemoveAll(stateDir)
		if err := os.WriteFile(stateDir, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)
		var stdout, stderr bytes.Buffer
		if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 2 {
			t.Fatalf("exit = %d, want 2 (stderr %q)", code, stderr.String())
		}
	})
}

func TestRunVarLockSession_EncryptedRelocksEveryGateway(t *testing.T) {
	dir := sessionHome(t)
	daily := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, nil)
	demo := startFakeGateway(t, "policy-demo", lockedGatewayStatus(), nil)
	encPath := filepath.Join(dir, "secrets.enc")
	before, err := os.ReadFile(encPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}

	// No passphrase variable and a non-terminal stdin: a prompt would fail.
	var stdout, stderr bytes.Buffer
	if err := runVarLockSession(context.Background(), &stdout, &stderr); err != nil {
		t.Fatalf("lock: %v (stderr %q)", err, stderr.String())
	}
	low, high := daily, demo
	if high.port < low.port {
		low, high = high, low
	}
	want := fmt.Sprintf("Store is already encrypted. Locked in 2 running gateway(s): %s (%d), %s (%d)", low.name, low.port, high.name, high.port)
	if strings.TrimSpace(stdout.String()) != want {
		t.Fatalf("output = %q, want %q", stdout.String(), want)
	}
	for _, g := range []*fakeGateway{daily, demo} {
		reqs := g.seen()
		if len(reqs) != 1 || reqs[0] != "POST /api/var/relock " {
			t.Fatalf("%s requests = %q", g.name, reqs)
		}
	}
	after, err := os.ReadFile(encPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("var lock rewrote secrets.enc on an encrypted store")
	}
}

func TestRunVarLockSession_EncryptedNoRunningGateway(t *testing.T) {
	sessionHome(t)
	var stdout, stderr bytes.Buffer
	if err := runVarLockSession(context.Background(), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "Store is already encrypted; no running gateway to lock" {
		t.Fatalf("output = %q", got)
	}
}

func TestRunVarLockSession_FailedGatewayExitsNonzero(t *testing.T) {
	sessionHome(t)
	bad := startFakeGateway(t, "bad", gatewayVarStatus{Encrypted: true}, nil)
	bad.respond = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"store is not encrypted"}`)
		return true
	}
	good := startFakeGateway(t, "good", gatewayVarStatus{Encrypted: true}, nil)
	var stdout, stderr bytes.Buffer
	err := runVarLockSession(context.Background(), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "could not lock 1 running gateway") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("Locked in 1 running gateway(s): good (%d)", good.port)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("bad (%d): failed: 409 store is not encrypted", bad.port)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunVarLockSession_PlaintextEncryptsWithoutContactingGateways(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv(vault.PassphraseFileEnv, "")
	dir := filepath.Join(home, ".gridctl", "vault")
	store := vault.NewStore(dir)
	if err := store.Set("K", "v"); err != nil {
		t.Fatal(err)
	}
	daily := startFakeGateway(t, "daily", gatewayVarStatus{}, nil)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)

	var stdout, stderr bytes.Buffer
	if err := runVarLockSession(context.Background(), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.enc")); err != nil {
		t.Fatalf("secrets.enc missing after lock: %v", err)
	}
	reloaded := vault.NewStore(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Unlock(sessionPass); err != nil {
		t.Fatalf("encrypted with an unexpected passphrase: %v", err)
	}
	if reqs := daily.seen(); len(reqs) != 0 {
		t.Fatalf("plaintext lock contacted a gateway: %v", reqs)
	}
}

func TestVaultDoctorMessage(t *testing.T) {
	gw := func(name string, s gatewayVarStatus, err error) gatewayVarState {
		return gatewayVarState{State: state.DaemonState{StackName: name}, Status: s, Err: err}
	}
	if got := vaultDoctorMessage(nil); got != "encrypted at rest and locked; gridctl apply prompts for the passphrase on a terminal" {
		t.Fatalf("no gateway = %q", got)
	}
	got := vaultDoctorMessage([]gatewayVarState{
		gw("daily", gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, nil),
		gw("policy-demo", gatewayVarStatus{Encrypted: true, Locked: true}, nil),
	})
	want := "encrypted at rest; unlocked in daily (stdin); locked in policy-demo; gridctl var unlock unlocks running gateways, gridctl apply prompts on a terminal"
	if got != want {
		t.Fatalf("message = %q\nwant      %q", got, want)
	}
	got = vaultDoctorMessage([]gatewayVarState{
		gw("plain", gatewayVarStatus{Encrypted: true}, nil),
		gw("down", gatewayVarStatus{}, errors.New("status 503")),
	})
	if got != "encrypted at rest; unlocked in plain; state unavailable in down; gridctl var unlock unlocks running gateways, gridctl apply prompts on a terminal" {
		t.Fatalf("message = %q", got)
	}
}

func TestCheckVault_ReportsGatewaySessions(t *testing.T) {
	sessionHome(t)
	startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, nil)
	var checks []doctorCheck
	checkVault(context.Background(), &checks)
	if len(checks) != 1 || checks[0].ID != "vault" || checks[0].Status != doctorStatusInfo {
		t.Fatalf("checks = %+v", checks)
	}
	if !strings.HasPrefix(checks[0].Message, "encrypted at rest; unlocked in daily (stdin);") {
		t.Fatalf("message = %q", checks[0].Message)
	}
}

func TestLockedExplainProblem(t *testing.T) {
	if got := lockedExplainProblem(nil); got != "store is locked; stored metadata and resolution are unknown" {
		t.Fatalf("no gateway = %q", got)
	}
	if got := lockedExplainProblem([]string{"daily"}); got != "store is locked in this process; gateway daily holds it unlocked" {
		t.Fatalf("one gateway = %q", got)
	}
}

func TestStatus_VarsColumnAndJSON(t *testing.T) {
	sessionHome(t)
	startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true, UnlockSource: "cli"}, nil)

	stdout, _ := captureOutput(t, func() error { return runStatus("daily", false, true, true) })
	var report struct {
		Gateways []map[string]any `json:"gateways"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, stdout)
	}
	if len(report.Gateways) != 1 {
		t.Fatalf("gateways = %v", report.Gateways)
	}
	vars, ok := report.Gateways[0]["variables"].(map[string]any)
	if !ok || vars["encrypted"] != true || vars["locked"] != false || vars["unlock_source"] != "cli" {
		t.Fatalf("variables = %v", report.Gateways[0]["variables"])
	}

	table, _ := captureOutput(t, func() error { return runStatus("daily", false, false, true) })
	if !strings.Contains(table, "VARS") || !strings.Contains(table, "unlocked (cli)") {
		t.Fatalf("status table:\n%s", table)
	}
}

func TestStatus_VariablesOmittedWhenUnread(t *testing.T) {
	sessionHome(t)
	down := startFakeGateway(t, "down", gatewayVarStatus{}, nil)
	down.respond = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/api/var/status" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	}
	stdout, _ := captureOutput(t, func() error { return runStatus("down", false, true, true) })
	if strings.Contains(stdout, `"variables"`) {
		t.Fatalf("variables present for an unread gateway:\n%s", stdout)
	}
}

func TestRunVarExplain_LockedNamesUnlockedGateway(t *testing.T) {
	sessionHome(t)
	startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true, UnlockSource: "stdin"}, nil)
	startFakeGateway(t, "policy-demo", lockedGatewayStatus(), nil)
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&out)
	exit, err := runVarExplain(cmd, "TOKEN", "json", stackPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc explainDocument
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if exit != 1 || !doc.Locked {
		t.Fatalf("exit=%d doc=%+v", exit, doc)
	}
	want := "store is locked in this process; gateway daily holds it unlocked"
	if len(doc.Problems) != 1 || doc.Problems[0] != want {
		t.Fatalf("problems = %q, want [%q]", doc.Problems, want)
	}
}
