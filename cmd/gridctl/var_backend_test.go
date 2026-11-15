package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/spf13/cobra"
)

const backendPass = "backend-passphrase-long" // #nosec G101 -- test fixture

// fixtureVarHome creates a temp home whose encrypted store holds a fixed
// set of variables and sets, points GRIDCTL_HOME at it, and returns the
// store directory. No passphrase variable is left set.
func fixtureVarHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, "")
	t.Setenv(gatewayTokenEnv, "")
	dir := filepath.Join(home, ".gridctl", "vault")
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []vault.Variable{
		{Key: "API_TOKEN", Value: "tok-abcdef-123456", Type: vault.TypeString, IsSecret: true, Set: "prod", Description: "deploy token"},
		{Key: "FEATURES", Value: `["a","b"]`, Type: vault.TypeList, IsSecret: false, Docs: "https://example.test"},
		{Key: "REGION", Value: "us-east-1", Type: vault.TypeString, IsSecret: false, Set: "prod"},
		{Key: "SHORT", Value: "xy", Type: vault.TypeString, IsSecret: true, Example: "zz", Deprecated: "use API_TOKEN"},
	} {
		if err := store.SetVariable(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateSet("empty"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(backendPass); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startStoreGateway serves the real variable API over an unlocked copy of
// the store in dir and records a running state file for it.
func startStoreGateway(t *testing.T, name, dir string) (*vault.Store, int) {
	t.Helper()
	daemonStore := vault.NewStore(dir)
	if err := daemonStore.Load(); err != nil {
		t.Fatal(err)
	}
	if err := daemonStore.Unlock(backendPass); err != nil {
		t.Fatal(err)
	}
	daemonStore.SetUnlockSource(vault.UnlockSourceStdin)
	srv := api.NewServer(mcp.NewGateway(), nil)
	srv.SetVaultStore(daemonStore)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: name, StackFile: "/tmp/" + name + ".yaml", PID: os.Getpid(), Port: port}); err != nil {
		t.Fatal(err)
	}
	return daemonStore, port
}

var clockStamp = regexp.MustCompile(`\d\d:\d\d:\d\d`)

// varOutput runs fn and returns its stdout and stderr with clock stamps
// removed, failing the test on an error.
func varOutput(t *testing.T, fn func() error) string {
	t.Helper()
	stdout, stderr := captureOutput(t, fn)
	return clockStamp.ReplaceAllString(stdout+"\x00"+stderr, "")
}

// setVarFlags sets package flag variables for the duration of the test.
func setVarFlags(t *testing.T, apply func()) {
	t.Helper()
	prev := []any{varGetPlain, varListFmt, varExportFmt, varExportPlain, varImportFmt, varDeleteForce,
		varSetValue, varSetSetName, varSetSecret, varSetPlaintext, varSetType}
	t.Cleanup(func() {
		varGetPlain, varListFmt, varExportFmt, varExportPlain = prev[0].(bool), prev[1].(string), prev[2].(string), prev[3].(bool)
		varImportFmt, varDeleteForce = prev[4].(string), prev[5].(bool)
		varSetValue, varSetSetName, varSetSecret, varSetPlaintext, varSetType = prev[6].(string), prev[7].(string), prev[8].(bool), prev[9].(bool), prev[10].(string)
	})
	apply()
}

func TestVarBackend_ReadCommandsMatchLocalOutput(t *testing.T) {
	nonTTYStdin(t)
	gatewayDir := fixtureVarHome(t)
	gatewayHome := os.Getenv("GRIDCTL_HOME")
	startStoreGateway(t, "daily", gatewayDir)
	localHome := filepath.Dir(filepath.Dir(fixtureVarHome(t)))

	ctx := context.Background()
	cases := []struct {
		name  string
		flags func()
		run   func() error
	}{
		{"list table", func() { varListFmt = "table" }, func() error { return runVarList(ctx) }},
		{"list json", func() { varListFmt = "json" }, func() error { return runVarList(ctx) }},
		{"get masked", func() { varGetPlain = false }, func() error { return runVarGet(ctx, "API_TOKEN") }},
		{"get plain", func() { varGetPlain = true }, func() error { return runVarGet(ctx, "API_TOKEN") }},
		{"get plaintext variable", func() { varGetPlain = false }, func() error { return runVarGet(ctx, "REGION") }},
		{"export env masked", func() { varExportFmt, varExportPlain = "env", false }, func() error { return runVarExport(ctx) }},
		{"export env plain", func() { varExportFmt, varExportPlain = "env", true }, func() error { return runVarExport(ctx) }},
		{"export json plain", func() { varExportFmt, varExportPlain = "json", true }, func() error { return runVarExport(ctx) }},
		{"sets list", func() {}, func() error { return runVarSetsList(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setVarFlags(t, tc.flags)

			t.Setenv("GRIDCTL_HOME", gatewayHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
			viaGateway := varOutput(t, tc.run)

			t.Setenv("GRIDCTL_HOME", localHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", backendPass)
			viaLocal := varOutput(t, tc.run)

			if viaGateway != viaLocal {
				t.Fatalf("gateway output differs from local\ngateway: %q\nlocal:   %q", viaGateway, viaLocal)
			}
			if viaGateway == "\x00" {
				t.Fatal("no output")
			}
		})
	}
}

func TestVarBackend_MutationsMatchLocal(t *testing.T) {
	nonTTYStdin(t)
	gatewayDir := fixtureVarHome(t)
	gatewayHome := os.Getenv("GRIDCTL_HOME")
	daemonStore, _ := startStoreGateway(t, "daily", gatewayDir)
	localDir := fixtureVarHome(t)
	localHome := filepath.Dir(filepath.Dir(localDir))

	importFile := filepath.Join(t.TempDir(), "vars.env")
	if err := os.WriteFile(importFile, []byte("# @public\nIMPORTED=one\nOTHER=\"two\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	steps := []struct {
		name  string
		flags func()
		run   func() error
	}{
		{"set", func() {
			varSetValue, varSetSetName, varSetType, varSetPlaintext, varSetSecret = "a,b", "new-set", "list", true, false
		}, func() error { return runVarSet(ctx, "ADDED") }},
		{"set replaces metadata", func() {
			varSetValue, varSetSetName, varSetType, varSetPlaintext = "rotated", "", "string", false
		}, func() error { return runVarSet(ctx, "API_TOKEN") }},
		{"delete", func() { varDeleteForce = true }, func() error { return runVarDelete(ctx, "SHORT") }},
		{"import", func() { varImportFmt = "" }, func() error { return runVarImport(ctx, importFile) }},
		{"sets create", func() {}, func() error { return runVarSetsCreate(ctx, "team") }},
		{"sets delete", func() {}, func() error { return runVarSetsDelete(ctx, "prod") }},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			setVarFlags(t, step.flags)

			t.Setenv("GRIDCTL_HOME", gatewayHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
			viaGateway := varOutput(t, step.run)

			t.Setenv("GRIDCTL_HOME", localHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", backendPass)
			viaLocal := varOutput(t, step.run)

			if viaGateway != viaLocal {
				t.Fatalf("gateway output differs from local\ngateway: %q\nlocal:   %q", viaGateway, viaLocal)
			}
		})
	}

	localStore := vault.NewStore(localDir)
	if err := localStore.Load(); err != nil {
		t.Fatal(err)
	}
	if err := localStore.Unlock(backendPass); err != nil {
		t.Fatal(err)
	}
	strip := func(vars []vault.Variable) []vault.Variable {
		for i := range vars {
			vars[i].LastRotated = ""
		}
		return vars
	}
	gotVars, wantVars := strip(daemonStore.List()), strip(localStore.List())
	if fmt.Sprint(gotVars) != fmt.Sprint(wantVars) {
		t.Fatalf("store through gateway = %+v\nlocal = %+v", gotVars, wantVars)
	}
	if got, want := fmt.Sprint(daemonStore.ListSets()), fmt.Sprint(localStore.ListSets()); got != want {
		t.Fatalf("sets through gateway = %s, local = %s", got, want)
	}
	// The gateway wrote the shared file; a fresh reader sees the change.
	reread := vault.NewStore(gatewayDir)
	if err := reread.Load(); err != nil {
		t.Fatal(err)
	}
	if err := reread.Unlock(backendPass); err != nil {
		t.Fatal(err)
	}
	if !reread.Has("ADDED") || reread.Has("SHORT") {
		t.Fatalf("store file not updated: %v", reread.Keys())
	}
}

func TestVarBackend_RunAndScanMatchLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	nonTTYStdin(t)
	gatewayDir := fixtureVarHome(t)
	gatewayHome := os.Getenv("GRIDCTL_HOME")
	startStoreGateway(t, "daily", gatewayDir)
	localHome := filepath.Dir(filepath.Dir(fixtureVarHome(t)))

	scanDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(scanDir, "leak.txt"), []byte("token=tok-abcdef-123456\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prevAll, prevSets, prevOnly, prevNoRedact := varRunAll, varRunSets, varRunOnly, varRunNoRedact
	prevScanFile, prevStaged := varScanFile, varScanStaged
	t.Cleanup(func() {
		varRunAll, varRunSets, varRunOnly, varRunNoRedact = prevAll, prevSets, prevOnly, prevNoRedact
		varScanFile, varScanStaged = prevScanFile, prevStaged
	})
	varRunAll, varRunSets, varRunOnly, varRunNoRedact = false, []string{"prod"}, nil, true
	varScanFile, varScanStaged = "", false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	run := func() error {
		code, err := runVarCommand(cmd, []string{"sh", "-c", `printf '%s|%s|%s' "$API_TOKEN" "$REGION" "${SHORT-unset}"`})
		if err == nil && code != 0 {
			err = fmt.Errorf("run exit %d", code)
		}
		return err
	}
	scan := func() error {
		code, err := runVarScan(cmd, []string{scanDir}, "json")
		if err == nil && code != 1 {
			err = fmt.Errorf("scan exit %d, want 1 (finding)", code)
		}
		return err
	}
	for name, fn := range map[string]func() error{"run": run, "scan": scan} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GRIDCTL_HOME", gatewayHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
			viaGateway := varOutput(t, fn)

			t.Setenv("GRIDCTL_HOME", localHome)
			t.Setenv("GRIDCTL_VAULT_PASSPHRASE", backendPass)
			viaLocal := varOutput(t, fn)

			if viaGateway != viaLocal {
				t.Fatalf("gateway output differs from local\ngateway: %q\nlocal:   %q", viaGateway, viaLocal)
			}
			if name == "run" && !strings.HasPrefix(viaGateway, "tok-abcdef-123456|us-east-1|unset") {
				t.Fatalf("run output = %q", viaGateway)
			}
		})
	}
}

func TestVarBackend_RunFetchesOnlySelectedValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	sessionHome(t)
	g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
	g.respond = func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/var":
			fmt.Fprint(w, `[{"key":"A","type":"string","is_secret":true},{"key":"B","type":"string","is_secret":true}]`)
		case "/api/var/A":
			fmt.Fprint(w, `{"key":"A","value":"alpha","type":"string","is_secret":true}`)
		case "/api/var/B":
			fmt.Fprint(w, `{"key":"B","value":"beta","type":"string","is_secret":true}`)
		default:
			return false
		}
		return true
	}
	prevAll, prevSets, prevOnly, prevNoRedact := varRunAll, varRunSets, varRunOnly, varRunNoRedact
	t.Cleanup(func() { varRunAll, varRunSets, varRunOnly, varRunNoRedact = prevAll, prevSets, prevOnly, prevNoRedact })
	varRunAll, varRunSets, varRunOnly, varRunNoRedact = false, nil, []string{"A"}, true

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	out := varOutput(t, func() error {
		_, err := runVarCommand(cmd, []string{"sh", "-c", `printf '%s' "$A"`})
		return err
	})
	if !strings.HasPrefix(out, "alpha") {
		t.Fatalf("output = %q", out)
	}
	for _, req := range g.seen() {
		if strings.HasPrefix(req, "GET /api/var/B") {
			t.Fatalf("fetched an unselected value: %v", g.seen())
		}
	}
}

func TestVarBackend_ExportSkipsReservedValues(t *testing.T) {
	nonTTYStdin(t)
	sessionHome(t)
	g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
	g.respond = func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/var":
			fmt.Fprint(w, `[{"key":"A","type":"string","is_secret":false},{"key":"GRIDCTL_TOKEN","type":"string","is_secret":true}]`)
		case "/api/var/A":
			fmt.Fprint(w, `{"key":"A","value":"alpha","type":"string","is_secret":false}`)
		case "/api/var/GRIDCTL_TOKEN":
			fmt.Fprint(w, `{"key":"GRIDCTL_TOKEN","value":"reserved","type":"string","is_secret":true}`)
		default:
			return false
		}
		return true
	}
	setVarFlags(t, func() { varExportFmt, varExportPlain = "env", true })

	out := varOutput(t, func() error { return runVarExport(context.Background()) })
	if !strings.Contains(out, "alpha") || strings.Contains(out, "reserved") {
		t.Fatalf("output = %q", out)
	}
	for _, req := range g.seen() {
		if strings.HasPrefix(req, "GET /api/var/GRIDCTL_TOKEN") {
			t.Fatalf("fetched a reserved value: %v", g.seen())
		}
	}
}

func TestOpenVarBackend_ResolutionOrder(t *testing.T) {
	t.Run("plaintext store stays local", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GRIDCTL_HOME", home)
		g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
		b, err := openVarBackend(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := b.(localVarBackend); !ok {
			t.Fatalf("backend = %T", b)
		}
		if len(g.seen()) != 0 {
			t.Fatalf("gateway contacted: %v", g.seen())
		}
	})

	t.Run("environment passphrase wins over a gateway", func(t *testing.T) {
		sessionHome(t)
		g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
		t.Setenv("GRIDCTL_VAULT_PASSPHRASE", sessionPass)
		b, err := openVarBackend(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := b.(localVarBackend); !ok {
			t.Fatalf("backend = %T", b)
		}
		if len(g.seen()) != 0 {
			t.Fatalf("gateway contacted: %v", g.seen())
		}
	})

	t.Run("passphrase file wins over a gateway", func(t *testing.T) {
		sessionHome(t)
		g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
		t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, sessionPass+"\n"))
		b, err := openVarBackend(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := b.(localVarBackend); !ok {
			t.Fatalf("backend = %T", b)
		}
		if len(g.seen()) != 0 {
			t.Fatalf("gateway contacted: %v", g.seen())
		}
	})

	t.Run("wrong environment passphrase does not fall back", func(t *testing.T) {
		sessionHome(t)
		g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
		t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "wrong-passphrase-value")
		if _, err := openVarBackend(context.Background(), true); err == nil {
			t.Fatal("expected an error")
		}
		if len(g.seen()) != 0 {
			t.Fatalf("gateway contacted: %v", g.seen())
		}
	})

	t.Run("lowest-port unlocked gateway", func(t *testing.T) {
		sessionHome(t)
		a := startFakeGateway(t, "a", gatewayVarStatus{Encrypted: true}, nil)
		b := startFakeGateway(t, "b", gatewayVarStatus{Encrypted: true}, nil)
		want := a
		if b.port < a.port {
			want = b
		}
		got, err := openVarBackend(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		gw, ok := got.(*gatewayVarBackend)
		if !ok || gw.st.Port != want.port {
			t.Fatalf("backend = %T %+v, want port %d", got, got, want.port)
		}
	})

	t.Run("no gateway and no terminal", func(t *testing.T) {
		sessionHome(t)
		_, err := openVarBackend(context.Background(), true)
		if !errors.Is(err, errVarStoreLocked) {
			t.Fatalf("err = %v", err)
		}
		want := "vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or GRIDCTL_VAULT_PASSPHRASE_FILE, start the stack with gridctl apply, or run this command in a terminal to be prompted"
		if err.Error() != want {
			t.Fatalf("message = %q", err.Error())
		}
	})
}

// closedPort returns a loopback port with no listener.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestOpenVarBackend_SkipsUnusableGateways(t *testing.T) {
	statusReply := func(code int) func(w http.ResponseWriter, r *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == "/api/var/status" {
				w.Header().Set("Gridctl-Auth-Rejected", "1")
				w.WriteHeader(code)
				return true
			}
			return false
		}
	}
	cases := map[string]func(t *testing.T){
		"401": func(t *testing.T) {
			g := startFakeGateway(t, "auth", gatewayVarStatus{Encrypted: true}, nil)
			g.respond = statusReply(http.StatusUnauthorized)
		},
		"423": func(t *testing.T) {
			g := startFakeGateway(t, "locked423", gatewayVarStatus{Encrypted: true}, nil)
			g.respond = statusReply(http.StatusLocked)
		},
		"locked": func(t *testing.T) {
			startFakeGateway(t, "locked", lockedGatewayStatus(), nil)
		},
		"unreachable": func(t *testing.T) {
			if err := state.Save(&state.DaemonState{StackName: "gone", PID: os.Getpid(), Port: closedPort(t)}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			sessionHome(t)
			setup(t)
			if _, err := openVarBackend(context.Background(), true); !errors.Is(err, errVarStoreLocked) {
				t.Fatalf("err = %v, want the locked error", err)
			}

			// A usable gateway behind the skipped one is still selected.
			next := startFakeGateway(t, "next", gatewayVarStatus{Encrypted: true}, nil)
			got, err := openVarBackend(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			if gw, ok := got.(*gatewayVarBackend); !ok || gw.st.Port != next.port {
				t.Fatalf("backend = %T %+v, want gateway next", got, got)
			}
		})
	}
}

// promptForbidden makes the terminal prompt fail the test if reached.
func promptForbidden(t *testing.T) {
	t.Helper()
	prev := stdinIsTerminal
	stdinIsTerminal = func() bool {
		t.Error("terminal prompt reached")
		return false
	}
	t.Cleanup(func() { stdinIsTerminal = prev })
}

func TestVarBackend_GatewayFailureFailsWithoutPrompt(t *testing.T) {
	ctx := context.Background()
	failing := func(path string) func(w http.ResponseWriter, r *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == path {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"boom"}`)
				return true
			}
			if r.URL.Path == "/api/var" || r.URL.Path == "/api/var/sets" {
				fmt.Fprint(w, `[]`)
				return true
			}
			if strings.HasPrefix(r.URL.Path, "/api/var/") && r.URL.Path != "/api/var/status" {
				fmt.Fprint(w, `{"key":"K","value":"v","type":"string","is_secret":true}`)
				return true
			}
			return false
		}
	}
	commands := []struct {
		name string
		path string
		run  func() error
	}{
		{"status read", "/api/var/status", func() error { return runVarList(ctx) }},
		{"list", "/api/var", func() error { return runVarList(ctx) }},
		{"get", "/api/var/K", func() error { return runVarGet(ctx, "K") }},
		{"set", "/api/var/K", func() error { return runVarSet(ctx, "K") }},
		{"export", "/api/var", func() error { return runVarExport(ctx) }},
		{"sets list", "/api/var/sets", func() error { return runVarSetsList(ctx) }},
		{"sets create", "/api/var/sets", func() error { return runVarSetsCreate(ctx, "team") }},
		{"sets delete", "/api/var/sets/team", func() error { return runVarSetsDelete(ctx, "team") }},
	}
	for _, c := range commands {
		t.Run(c.name, func(t *testing.T) {
			sessionHome(t)
			setVarFlags(t, func() { varSetValue, varSetType, varListFmt = "value", "string", "table" })
			g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
			g.respond = failing(c.path)
			promptForbidden(t)

			var err error
			captureStdout(t, func() { err = c.run() })
			want := "gateway daily: 500 boom"
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestVarBackend_OperationErrorsCarryNoValues(t *testing.T) {
	sessionHome(t)
	g := startFakeGateway(t, "daily", gatewayVarStatus{Encrypted: true}, nil)
	g.respond = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusLocked)
			fmt.Fprint(w, `{"error":"vault is locked"}`)
			return true
		}
		return false
	}
	setVarFlags(t, func() { varSetValue, varSetType = "super-secret-value", "string" })
	var err error
	captureStdout(t, func() { err = runVarSet(context.Background(), "K") })
	if err == nil || err.Error() != "gateway daily: 423 vault is locked" {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatal("error carries the value")
	}
}

func TestVarBackend_GetMissingKeyMatchesLocal(t *testing.T) {
	nonTTYStdin(t)
	dir := fixtureVarHome(t)
	startStoreGateway(t, "daily", dir)
	err := runVarGet(context.Background(), "MISSING")
	if err == nil || err.Error() != `variable "MISSING" not found` {
		t.Fatalf("get err = %v", err)
	}
	setVarFlags(t, func() { varDeleteForce = true })
	err = runVarDelete(context.Background(), "MISSING")
	if err == nil || err.Error() != `variable "MISSING" not found` {
		t.Fatalf("delete err = %v", err)
	}
}

func TestVarScan_WithFileSkipsLockedStoreWithoutGateway(t *testing.T) {
	sessionHome(t)
	promptForbidden(t)
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prevFile, prevStaged := varScanFile, varScanStaged
	t.Cleanup(func() { varScanFile, varScanStaged = prevFile, prevStaged })
	varScanFile, varScanStaged = stackPath, false
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var code int
	var err error
	captureStdout(t, func() { code, err = runVarScan(cmd, []string{t.TempDir()}, "json") })
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
