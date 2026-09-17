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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestCLIToolsCall_HTTPAndProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	backendPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", backendPort))
	waitForPort(t, ctx, backendPort)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:      "echo",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", backendPort),
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
		if code != 0 || !strings.Contains(stdout, "Invoke a live gateway tool") {
			t.Fatalf("help exit %d %s", code, stdout)
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
