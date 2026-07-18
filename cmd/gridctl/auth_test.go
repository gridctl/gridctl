package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// startFakeAuthAPI runs a fake daemon auth API and records a matching
// running-stack state file. Returns the port.
func startFakeAuthAPI(t *testing.T, mux *http.ServeMux) int {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	st := &state.DaemonState{StackName: "test", StackFile: "/x.yaml", PID: os.Getpid(), Port: port, StartedAt: time.Now()}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}
	return port
}

func authServersHandler(infos []authServerInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(infos)
	}
}

func TestAuthResolveDaemonNoStack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	authStack = ""
	if _, err := authResolveDaemon(); err == nil {
		t.Fatal("expected an error with no running stacks")
	}
}

func TestRunAuthStatusNeedsAuthExitCode(t *testing.T) {
	authStack = ""
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/servers", authServersHandler([]authServerInfo{
		{Server: "github", Status: "authorized", Issuer: "https://as.example.com"},
		{Server: "notion", Status: "needs_auth"},
	}))
	startFakeAuthAPI(t, mux)

	var stdout, stderr bytes.Buffer
	plain := true
	authStatusPlain = &plain
	exit := runAuthStatus(&stdout, &stderr, "", "table")

	if exit != authExitNeedsAuth {
		t.Fatalf("exit = %d, want %d", exit, authExitNeedsAuth)
	}
	out := stdout.String()
	if !strings.Contains(out, "needs auth (run 'gridctl auth login notion')") {
		t.Errorf("table must name the login command, got:\n%s", out)
	}
	if !strings.Contains(out, "github") || !strings.Contains(out, "authorized") {
		t.Errorf("table missing authorized row:\n%s", out)
	}
}

func TestRunAuthStatusJSON(t *testing.T) {
	authStack = ""
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/servers", authServersHandler([]authServerInfo{
		{Server: "github", Status: "authorized"},
	}))
	startFakeAuthAPI(t, mux)

	var stdout, stderr bytes.Buffer
	plain := false
	authStatusPlain = &plain
	exit := runAuthStatus(&stdout, &stderr, "", "json")

	if exit != authExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr.String())
	}
	var doc authStatusDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if doc.SchemaVersion != authJSONSchemaVersion || doc.NeedsAuth || len(doc.Servers) != 1 {
		t.Errorf("unexpected doc: %+v", doc)
	}
}

func TestRunAuthStatusUnknownServer(t *testing.T) {
	authStack = ""
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/servers", authServersHandler(nil))
	startFakeAuthAPI(t, mux)

	var stdout, stderr bytes.Buffer
	plain := true
	authStatusPlain = &plain
	if exit := runAuthStatus(&stdout, &stderr, "ghost", "table"); exit != authExitInfrastructure {
		t.Fatalf("exit = %d, want %d", exit, authExitInfrastructure)
	}
}

func TestRunAuthLoginNoBrowser(t *testing.T) {
	authStack = ""
	authLoginBrowser = true
	authLoginManual = false
	authLoginFormat = "text"
	authLoginTimeout = 5 * time.Second
	t.Cleanup(func() { authLoginBrowser = false })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/servers/notion/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorize_url": "https://as.example.com/authorize?state=abc",
			"state":         "abc",
		})
	})
	mux.HandleFunc("GET /api/servers/notion/auth/wait", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "abc" {
			http.Error(w, "wrong state", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "authorized"})
	})
	expiry := time.Now().Add(time.Hour)
	mux.HandleFunc("/api/auth/servers", authServersHandler([]authServerInfo{
		{Server: "notion", Status: "authorized", Issuer: "https://as.example.com", Scopes: []string{"read"}, Expiry: &expiry},
	}))
	startFakeAuthAPI(t, mux)

	var stdout, stderr bytes.Buffer
	if err := runAuthLogin(&stdout, &stderr, "notion"); err != nil {
		t.Fatalf("runAuthLogin: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "https://as.example.com/authorize?state=abc") {
		t.Errorf("no-browser mode must print the URL:\n%s", out)
	}
	if !strings.Contains(out, "Authorized notion.") || !strings.Contains(out, "Issuer: https://as.example.com") {
		t.Errorf("missing success summary:\n%s", out)
	}
}

func TestRunAuthLoginFailurePropagates(t *testing.T) {
	authStack = ""
	authLoginBrowser = true
	authLoginManual = false
	t.Cleanup(func() { authLoginBrowser = false })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/servers/notion/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "authorization server does not support dynamic client registration; set auth.client_id (and auth.client_secret if issued) in stack.yaml",
		})
	})
	startFakeAuthAPI(t, mux)

	var stdout, stderr bytes.Buffer
	err := runAuthLogin(&stdout, &stderr, "notion")
	if err == nil || !strings.Contains(err.Error(), "auth.client_id") {
		t.Fatalf("expected DCR-fallback error to propagate, got %v", err)
	}
}

func TestPrintAuthHints(t *testing.T) {
	authStack = ""
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/servers", authServersHandler([]authServerInfo{
		{Server: "notion", Status: "needs_auth"},
		{Server: "github", Status: "authorized"},
	}))
	port := startFakeAuthAPI(t, mux)

	var out bytes.Buffer
	printAuthHints(port, &out)

	if !strings.Contains(out.String(), "gridctl auth login notion") {
		t.Errorf("hint must name the login command:\n%s", out.String())
	}
	if strings.Contains(out.String(), "github") {
		t.Errorf("authorized servers must not produce hints:\n%s", out.String())
	}
}
