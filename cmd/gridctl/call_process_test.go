package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/state"
)

func buildCallBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gridctl")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runCallBin(t *testing.T, bin, home string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func writeCallState(t *testing.T, home string, port int, token, header, authType string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0700); err != nil {
		t.Fatal(err)
	}
	st := &state.DaemonState{
		StackName:  "demo",
		PID:        os.Getpid(),
		Port:       port,
		AuthToken:  token,
		AuthHeader: header,
		AuthType:   authType,
		Home:       home,
	}
	t.Setenv(state.HomeEnv, home)
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}
}

func TestCallProcess_JSONFlagOrderAndConflicts(t *testing.T) {
	bin := buildCallBinary(t)
	home := t.TempDir()
	stdout, stderr, code := runCallBin(t, bin, home, "call", "echo__echo", "--bad", "--json")
	if code != 1 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, `"schema_version"`) || !strings.Contains(stdout, "invalid_flag") {
		t.Fatalf("expected JSON error, stdout=%s stderr=%s", stdout, stderr)
	}
	stdout, stderr, code = runCallBin(t, bin, home, "call", "echo__echo", "--json", "--bad")
	if code != 1 || !strings.Contains(stdout, "invalid_flag") {
		t.Fatalf("json then bad: exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stdout, stderr, code = runCallBin(t, bin, home, "call", "echo__echo", "--json", "--format", "invalid")
	if code != 1 || !strings.Contains(stdout, `"schema_version"`) {
		t.Fatalf("format conflict: exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stdout, stderr, code = runCallBin(t, bin, home, "tools", "search", "x", "--bad", "--json")
	if code != 1 || !strings.Contains(stdout, `"schema_version"`) {
		t.Fatalf("tools search: exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestCallProcess_EmptyArgsDoNotDispatch(t *testing.T) {
	bin := buildCallBinary(t)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	home := t.TempDir()
	writeCallState(t, home, port, "tok", "", "bearer")
	stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "   ", "--json")
	if code != 1 || posts.Load() != 0 {
		t.Fatalf("whitespace args dispatched posts=%d exit=%d stdout=%s", posts.Load(), code, stdout)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runCallBin(t, bin, home, "call", "echo__echo", "@"+path, "--json")
	if code != 1 || posts.Load() != 0 {
		t.Fatalf("empty file dispatched posts=%d exit=%d stdout=%s", posts.Load(), code, stdout)
	}
}

func TestCallProcess_TimeoutUnknownNoRetry(t *testing.T) {
	bin := buildCallBinary(t)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"late"}]},"error":null}`)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	home := t.TempDir()
	writeCallState(t, home, port, "tok", "", "bearer")
	stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", `{"message":"x"}`, "--json", "--timeout", "50ms")
	if code != 1 {
		t.Fatalf("exit %d stdout=%s", code, stdout)
	}
	if posts.Load() != 1 {
		t.Fatalf("retries posts=%d", posts.Load())
	}
	if !strings.Contains(stdout, `"deadline_exceeded"`) || !strings.Contains(stdout, `"unknown"`) {
		t.Fatalf("stdout=%s", stdout)
	}
	if strings.Contains(stdout, `"not_started"`) {
		t.Fatalf("timeout reported as not started: %s", stdout)
	}
}

func TestCallProcess_MalformedAndPlainResponses(t *testing.T) {
	bin := buildCallBinary(t)
	t.Run("empty json object", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)
		home := t.TempDir()
		writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
		stdout, _, code := runCallBin(t, bin, home, "tools", "search", "x", "--json")
		if code != 1 || strings.Contains(stdout, `"schema_version": 0`) {
			t.Fatalf("empty catalog success: exit %d stdout=%s", code, stdout)
		}
		if !strings.Contains(stdout, "malformed_response") || !strings.Contains(stdout, `"unknown"`) {
			t.Fatalf("stdout=%s", stdout)
		}
	})
	t.Run("trailing document", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"ok"}]},"error":null} {}`)
		}))
		t.Cleanup(srv.Close)
		home := t.TempDir()
		writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
		stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "--json")
		if code != 1 || !strings.Contains(stdout, "malformed_response") {
			t.Fatalf("exit %d stdout=%s", code, stdout)
		}
	})
	t.Run("numeric meta", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"ok"}],"_meta":{"n":9007199254740993}},"error":null}`)
		}))
		t.Cleanup(srv.Close)
		home := t.TempDir()
		writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
		stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "--json")
		if code != 0 {
			t.Fatalf("exit %d stdout=%s", code, stdout)
		}
		if !strings.Contains(stdout, "9007199254740993") {
			t.Fatalf("lost precision: %s", stdout)
		}
	})
	t.Run("client label sent raw", func(t *testing.T) {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var req struct {
				Client string `json:"client"`
			}
			_ = json.Unmarshal(raw, &req)
			got = req.Client
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"schema_version":1,"client":"cursor-ide","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"ok"}]},"error":null}`)
		}))
		t.Cleanup(srv.Close)
		home := t.TempDir()
		writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
		stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "--as", "cursor ide", "--json")
		if code != 0 {
			t.Fatalf("exit %d stdout=%s", code, stdout)
		}
		if got != "cursor ide" {
			t.Fatalf("sent client %q", got)
		}
		if !strings.Contains(stdout, `"client": "cursor-ide"`) {
			t.Fatalf("effective client missing: %s", stdout)
		}
	})
}

func TestCallProcess_HelpTargetsAndOffline(t *testing.T) {
	bin := buildCallBinary(t)
	home := t.TempDir()
	stdout, stderr, code := runCallBin(t, bin, home, "call", "--format", "json", "--help")
	if code != 0 || !strings.Contains(stdout, "Invoke one canonical tool") {
		t.Fatalf("offline help exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "daemon_unavailable") {
		t.Fatalf("zero-target help contacted a daemon: %s%s", stdout, stderr)
	}

	var query atomic.Value
	query.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query.Store(r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","tools":[{"name":"echo__echo","description":"Echo","inputSchema":{"type":"object","properties":{"message":{"type":"string","enum":["a","b"]}}}}],"total_visible":1,"matched":1,"returned":1,"truncated":false}`)
	}))
	t.Cleanup(srv.Close)
	home = t.TempDir()
	writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
	stdout, stderr, code = runCallBin(t, bin, home, "call", "--format", "json", "echo__echo", "--help")
	if code != 0 {
		t.Fatalf("leaf help exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	q, _ := query.Load().(string)
	if strings.Contains(q, "server=json") || !strings.Contains(q, "name=echo__echo") {
		t.Fatalf("query=%s", q)
	}

	query.Store("")
	stdout, stderr, code = runCallBin(t, bin, home, "call", "--timeout", "5s", "echo", "--help")
	if code != 0 {
		t.Fatalf("server help exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	q, _ = query.Load().(string)
	if !strings.Contains(q, "server=echo") {
		t.Fatalf("query=%s", q)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "404 page not found")
	}))
	t.Cleanup(plain.Close)
	home = t.TempDir()
	writeCallState(t, home, plain.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
	stdout, _, code = runCallBin(t, bin, home, "call", "echo__echo", "--help", "--json")
	if code != 1 || !strings.Contains(stdout, "missing_endpoint") {
		t.Fatalf("plain 404: exit %d stdout=%s", code, stdout)
	}

	typed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__hidden","outcome":{"disposition":"routing_failed","stage":"routing","reason":"not_found","completion":"not_started"},"result":null,"error":{"code":"not_found","message":"tool or server not found"}}`)
	}))
	t.Cleanup(typed.Close)
	home = t.TempDir()
	writeCallState(t, home, typed.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
	stdout, _, code = runCallBin(t, bin, home, "call", "echo__hidden", "--help", "--json")
	if code != 1 || !strings.Contains(stdout, "not_found") || strings.Contains(stdout, "missing_endpoint") {
		t.Fatalf("typed 404: exit %d stdout=%s", code, stdout)
	}
}

func TestCallProcess_CompletedToolErrorExitTwo(t *testing.T) {
	bin := buildCallBinary(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"tool_error","stage":"downstream","reason":"tool_error","completion":"complete"},"result":{"content":[{"type":"text","text":"nope"}],"isError":true},"error":{"code":"tool_error","message":"tool returned an error"}}`)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "tok", "", "bearer")
	stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "--json")
	if code != 2 {
		t.Fatalf("exit %d stdout=%s", code, stdout)
	}
}

func TestCallProcess_CustomHeaderCredential(t *testing.T) {
	bin := buildCallBinary(t)
	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"ok"}]},"error":null}`)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeCallState(t, home, srv.Listener.Addr().(*net.TCPAddr).Port, "secret", "X-Api-Key", "apikey")
	stdout, _, code := runCallBin(t, bin, home, "call", "echo__echo", "--json")
	if code != 0 {
		t.Fatalf("exit %d stdout=%s", code, stdout)
	}
	if header != "secret" {
		t.Fatalf("header = %q", header)
	}
}

func TestCallEnvelopeCompletedToolErrorRequiresComplete(t *testing.T) {
	env := cliCallEnvelope{Outcome: mcp.CallOutcome{Disposition: "tool_error", Stage: "downstream", Reason: "tool_error", Completion: mcp.CompletionUnknown}, Result: &mcp.ToolCallResult{IsError: true}}
	if callEnvelopeCompletedToolError(env) {
		t.Fatal("unknown completion must not select exit two")
	}
}
