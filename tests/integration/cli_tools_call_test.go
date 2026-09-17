//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/state"
)

type recSink struct{ rec *runs.Recorder }

func (s recSink) Begin(ctx context.Context, name string) mcp.RunAttempt {
	return s.rec.Begin(ctx, name)
}

func TestCLIToolsCall_HTTPAndProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	backendPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", backendPort))
	waitForPort(t, ctx, backendPort)

	var downstreamCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if bytes.Contains(body, []byte(`"method":"tools/call"`)) {
			downstreamCalls.Add(1)
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, fmt.Sprintf("http://127.0.0.1:%d%s", backendPort, r.URL.Path), bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
	}))
	t.Cleanup(proxy.Close)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:      "echo",
		Transport: mcp.TransportHTTP,
		Endpoint:  proxy.URL + "/mcp",
	}); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	token := "test-token"
	srv := api.NewServer(gw, nil)
	srv.SetAuth("bearer", token, "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := &http.Server{Handler: srv.Handler()}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() {
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = httpSrv.Shutdown(shutdownCtx)
	})
	port := ln.Addr().(*net.TCPAddr).Port

	t.Run("rest_call_and_discover", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"echo__echo","arguments":{"message":"hello"}}`)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/tools/call", port), body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d %s", resp.StatusCode, raw)
		}
		var env struct {
			Outcome mcp.CallOutcome     `json:"outcome"`
			Result  *mcp.ToolCallResult `json:"result"`
			Error   *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.Outcome.Reason != runs.ReasonOK || env.Result == nil || env.Error != nil {
			t.Fatalf("env = %+v", env)
		}

		disc, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/tools/discover?query=mcp", port), nil)
		if err != nil {
			t.Fatal(err)
		}
		disc.Header.Set("Authorization", "Bearer "+token)
		dresp, err := http.DefaultClient.Do(disc)
		if err != nil {
			t.Fatal(err)
		}
		defer dresp.Body.Close()
		if dresp.StatusCode != http.StatusOK {
			t.Fatalf("discover %d", dresp.StatusCode)
		}
	})

	t.Run("auth_rejected", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/tools/call", port), strings.NewReader(`{"name":"echo__echo"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Gridctl-Auth-Rejected") != "1" {
			t.Fatalf("status=%d rejected=%s", resp.StatusCode, resp.Header.Get("Gridctl-Auth-Rejected"))
		}
	})

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/gridctl")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0700); err != nil {
		t.Fatal(err)
	}
	st := &state.DaemonState{
		StackName: "demo",
		PID:       os.Getpid(),
		Port:      port,
		AuthToken: token,
		AuthType:  "bearer",
		Home:      home,
	}
	t.Setenv(state.HomeEnv, home)
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	runCLI := func(args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return stdout.String(), stderr.String(), code
	}

	t.Run("cli_success", func(t *testing.T) {
		stdout, stderr, code := runCLI("call", "echo__echo", `{"message":"hello"}`, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if strings.Count(stdout, `"schema_version"`) != 1 {
			t.Fatalf("stdout=%s", stdout)
		}
	})

	t.Run("cli_invalid_json_exit_1", func(t *testing.T) {
		stdout, _, code := runCLI("call", "echo__echo", `[]`, "--format", "json")
		if code != 1 {
			t.Fatalf("exit %d stdout=%s", code, stdout)
		}
	})

	t.Run("cli_offline_help", func(t *testing.T) {
		stdout, _, code := runCLI("call", "--help")
		if code != 0 || !strings.Contains(stdout, "Invoke one canonical tool") {
			t.Fatalf("help exit %d %s", code, stdout)
		}
	})
	t.Run("cli_omitted_and_file_args", func(t *testing.T) {
		stdout, stderr, code := runCLI("call", "echo__echo", "--format", "json")
		if code != 0 {
			t.Fatalf("omitted args exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "args.json")
		if err := os.WriteFile(path, []byte(`{"message":"from-file"}`), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code = runCLI("call", "echo__echo", "@"+path, "--format", "json")
		if code != 0 || !strings.Contains(stdout, "from-file") && !strings.Contains(stdout, "Echo") {
			t.Fatalf("file args exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
	})
	t.Run("cli_targeted_help", func(t *testing.T) {
		before := downstreamCalls.Load()
		stdout, stderr, code := runCLI("call", "echo__echo", "--help")
		if code != 0 {
			t.Fatalf("leaf help exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if !strings.Contains(stdout, "echo__echo") {
			t.Fatalf("leaf help = %s", stdout)
		}
		stdout, stderr, code = runCLI("call", "echo", "--help")
		if code != 0 || !strings.Contains(stdout, "echo__echo") {
			t.Fatalf("server help exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("help dispatched a tool")
		}
	})
	t.Run("cli_tools_search", func(t *testing.T) {
		before := downstreamCalls.Load()
		stdout, stderr, code := runCLI("tools", "search", "mcp", "--format", "json")
		if code != 0 {
			t.Fatalf("search exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if !strings.Contains(stdout, "echo__echo") {
			t.Fatalf("search = %s", stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("search dispatched a tool")
		}
	})
	t.Run("cli_explicit_home", func(t *testing.T) {
		other := t.TempDir()
		cmd := exec.CommandContext(ctx, bin, "--home", home, "call", "echo__echo", `{"message":"home"}`, "--format", "json")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + other, "GRIDCTL_HOME=" + other, "NO_COLOR=1"}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("explicit --home failed: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
		}
	})
	t.Run("cli_scope_and_as", func(t *testing.T) {
		gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
			Default: "deny",
			Profiles: map[string]mcp.ClientProfileSpec{
				"allowed": {Servers: []string{"echo"}},
			},
		}))
		t.Cleanup(func() { gw.SetClientAccessPolicy(nil) })
		before := downstreamCalls.Load()
		stdout, _, code := runCLI("call", "echo__echo", `{"message":"no"}`, "--format", "json")
		if code != 1 || !strings.Contains(stdout, "client_scope") {
			t.Fatalf("deny cli: exit %d stdout=%s", code, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("scope denial invoked downstream")
		}
		stdout, stderr, code := runCLI("call", "echo__echo", `{"message":"yes"}`, "--as", "allowed", "--format", "json")
		if code != 0 {
			t.Fatalf("--as allowed: exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
	})
	t.Run("cli_rate_limit", func(t *testing.T) {
		pol := limits.NewPolicy(&config.LimitsConfig{RateLimits: []config.RateLimit{{Tool: "echo__echo", CallsPerMinute: 1, Burst: 1}}}, nil)
		gw.SetCallGates(pol.Gates())
		t.Cleanup(func() { gw.SetCallGates(nil) })
		if _, _, code := runCLI("call", "echo__echo", `{"message":"one"}`, "--format", "json"); code != 0 {
			t.Fatalf("first rate-limited call failed: %d", code)
		}
		before := downstreamCalls.Load()
		stdout, _, code := runCLI("call", "echo__echo", `{"message":"two"}`, "--format", "json")
		if code != 1 || !strings.Contains(stdout, "gate_denied") {
			t.Fatalf("second call: exit %d stdout=%s", code, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("rate limit invoked downstream")
		}
	})
	t.Run("cli_unknown_and_stale", func(t *testing.T) {
		before := downstreamCalls.Load()
		stdout, _, code := runCLI("call", "echo__missing", "--format", "json")
		if code != 1 || !strings.Contains(stdout, "unknown_tool") {
			t.Fatalf("unknown: exit %d stdout=%s", code, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("unknown tool invoked downstream")
		}
		gw.Router().RemoveClient("echo")
		gw.Router().RefreshTools()
		t.Cleanup(func() {
			if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
				Name: "echo", Transport: mcp.TransportHTTP, Endpoint: proxy.URL + "/mcp",
			}); err != nil {
				t.Errorf("re-register: %v", err)
			}
		})
		stdout, _, code = runCLI("call", "echo__echo", "--format", "json")
		if code != 1 || !strings.Contains(stdout, "unknown_tool") {
			t.Fatalf("stale: exit %d stdout=%s", code, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("stale catalog invoked downstream")
		}
	})
	t.Run("cli_recorder", func(t *testing.T) {
		dir := t.TempDir()
		rec, err := runs.NewRecorder(runs.Config{
			Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
			QueueSize: 8, SyncInterval: 20 * time.Millisecond, ShutdownDrain: time.Second,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(rec.Close)
		gw.SetRunSink(recSink{rec: rec})
		t.Cleanup(func() { gw.SetRunSink(nil) })
		if _, _, code := runCLI("call", "echo__echo", `{"message":"rec"}`, "--format", "json"); code != 0 {
			t.Fatalf("recorded call failed: %d", code)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			res, err := runs.Query(ctx, dir, runs.Filter{}, 10, nil, rec.WipeEpoch())
			if err == nil && len(res.Records) >= 1 {
				raw, _ := json.Marshal(res.Records[0])
				if strings.Contains(string(raw), `"message":"rec"`) {
					t.Fatalf("payload persisted: %s", raw)
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("run record not written")
	})
	t.Run("cli_unhealthy_recorder", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		rec, err := runs.NewRecorder(runs.Config{
			Enabled: true, Dir: bad, MaxBytes: 1 << 20, MaxAge: time.Hour,
			QueueSize: 4, SyncInterval: time.Second, ShutdownDrain: time.Second,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(rec.Close)
		gw.SetRunSink(recSink{rec: rec})
		t.Cleanup(func() { gw.SetRunSink(nil) })
		stdout, stderr, code := runCLI("call", "echo__echo", `{"message":"degraded"}`, "--format", "json")
		if code != 0 {
			t.Fatalf("unhealthy recorder changed the result: exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
	})
	t.Run("cli_schema_pin_block", func(t *testing.T) {
		set := gw.Router().GetReplicaSet("echo")
		if set == nil || len(set.Replicas()) == 0 {
			t.Fatal("echo replica missing")
		}
		tools := set.Replicas()[0].Client().Tools()
		store := pins.NewWithPath(t.TempDir(), "cli-call")
		adapter := pins.NewGatewayAdapter(store)
		if _, err := adapter.VerifyOrPin("echo", tools); err != nil {
			t.Fatal(err)
		}
		gw.SetSchemaVerifier(adapter, "block")
		gw.Router().RemoveClient("echo")
		driftPort := freePort(t)
		startMockServerEnv(t, driftPort, "MOCK_ECHO_DESC=changed for pin block")
		waitForPort(t, ctx, driftPort)
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name: "echo", Transport: mcp.TransportHTTP, Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", driftPort),
		}); err != nil {
			t.Fatalf("register drifted: %v", err)
		}
		t.Cleanup(func() {
			gw.UnblockServer("echo")
			gw.SetSchemaVerifier(nil, "")
			gw.Router().RemoveClient("echo")
			if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
				Name: "echo", Transport: mcp.TransportHTTP, Endpoint: proxy.URL + "/mcp",
			}); err != nil {
				t.Errorf("restore echo: %v", err)
			}
		})
		before := downstreamCalls.Load()
		stdout, _, code := runCLI("call", "echo__echo", `{"message":"pin"}`, "--format", "json")
		if code != 1 || !strings.Contains(stdout, "schema_pin") {
			t.Fatalf("pin block: exit %d stdout=%s", code, stdout)
		}
		if downstreamCalls.Load() != before {
			t.Fatal("pin block invoked downstream")
		}
	})
	t.Run("cli_ambiguous_stacks", func(t *testing.T) {
		other := *st
		other.StackName = "other"
		if err := state.Save(&other); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := state.Delete("other"); err != nil {
				t.Error(err)
			}
		})
		stdout, stderr, code := runCLI("call", "echo__echo", "--format", "json")
		if code != 1 {
			t.Fatalf("ambiguous exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout+stderr, "multiple stacks") && !strings.Contains(stdout, "daemon_unavailable") {
			t.Fatalf("ambiguous output stdout=%s stderr=%s", stdout, stderr)
		}
	})
	t.Run("cli_stopped_daemon", func(t *testing.T) {
		empty := t.TempDir()
		cmd := exec.CommandContext(ctx, bin, "call", "echo__echo", "--format", "json")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + empty, "GRIDCTL_HOME=" + empty, "NO_COLOR=1"}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			code = err.(*exec.ExitError).ExitCode()
		}
		if code != 1 {
			t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		if strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "passphrase") {
			t.Fatal("prompted for vault")
		}
	})

	t.Run("cli_wrong_auth", func(t *testing.T) {
		st.AuthToken = "stale"
		if err := state.Save(st); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := runCLI("call", "echo__echo", `{"message":"x"}`, "--format", "json")
		if code != 1 {
			t.Fatalf("exit %d %s", code, stdout)
		}
		if !strings.Contains(stdout, "gateway_auth_rejected") {
			t.Fatalf("stdout=%s", stdout)
		}
	})
}

func TestCLIToolsCall_ForegroundDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	backendPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", backendPort))
	waitForPort(t, ctx, backendPort)

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/gridctl")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	stack := fmt.Sprintf("version: \"1\"\nname: cli-fg\ngateway:\n  auth:\n    type: bearer\n    token: fg-token\nmcp-servers:\n  - name: echo\n    url: http://127.0.0.1:%d/mcp\n    transport: http\n", backendPort)
	if err := os.WriteFile(stackPath, []byte(stack), 0600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	daemon := exec.CommandContext(ctx, bin, "apply", stackPath, "--foreground", "--port", fmt.Sprintf("%d", port))
	daemon.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GRIDCTL_HOME=" + dir, "NO_COLOR=1"}
	daemon.Stdout, daemon.Stderr = logFile, logFile
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = daemon.Process.Signal(syscall.SIGTERM)
		_, _ = daemon.Process.Wait()
	})
	waitForPort(t, ctx, port)
	t.Setenv(state.HomeEnv, dir)
	st := &state.DaemonState{
		StackName: "cli-fg",
		PID:       daemon.Process.Pid,
		Port:      port,
		AuthToken: "fg-token",
		AuthType:  "bearer",
		Home:      dir,
	}
	if err := state.Save(st); err != nil {
		logs, _ := os.ReadFile(logPath)
		t.Fatalf("save state: %v\n%s", err, logs)
	}
	readyDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(readyDeadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/tools/discover?name=echo__echo", port), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer fg-token")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd := exec.CommandContext(ctx, bin, "call", "echo__echo", `{"message":"fg"}`, "--format", "json")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GRIDCTL_HOME=" + dir, "NO_COLOR=1"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		logs, _ := os.ReadFile(logPath)
		t.Fatalf("foreground call: %v stderr=%s stdout=%s\ndaemon:\n%s", err, stderr.String(), stdout.String(), logs)
	}
	if !strings.Contains(stdout.String(), `"ok"`) && !strings.Contains(stdout.String(), "Echo") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}
