//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestA2AAdapter_DaemonRestartAndReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, "./cmd/gridctl")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("race build failed: %v\n%s", err, output)
	}
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AAdapterFixture(t, version)
			home := t.TempDir()
			token := rand.Text()
			// An empty PATH and nonexistent daemon endpoints demonstrate that an
			// A2A-only apply does not discover or require a container runtime.
			env := []string{"PATH=" + home, "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1",
				"DOCKER_HOST=unix://" + home + "/absent.sock", "CONTAINER_HOST=unix://" + home + "/absent.sock",
				"A2A_TEST_TOKEN=" + token}
			stackPath := filepath.Join(home, "stack.yaml")
			stack := func(timeout, extra string) string {
				return fmt.Sprintf("version: \"1\"\nname: a2a-restart\nexperimental:\n  a2a: true\ngateway:\n  auth:\n    type: bearer\n    token: ${A2A_TEST_TOKEN}\n  security:\n    schema_pinning:\n      enabled: false\nmcp-servers:\n  - name: agent\n    pin_schemas: false\n    a2a:\n      card: %s/card\n      dialect: %q\n      timeout: %s\n%s", fixture.server.URL, version, timeout, extra)
			}
			writeStack := func(body string) {
				t.Helper()
				if err := os.WriteFile(stackPath, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeStack(stack("5m", ""))
			port := freePort(t)
			logPath := filepath.Join(home, "daemon.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			client := &http.Client{Timeout: 10 * time.Second}
			request := func(method, path string, input any) (int, []byte) {
				t.Helper()
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				response, err := client.Do(req)
				if err != nil {
					t.Fatal("daemon HTTP request failed")
				}
				defer response.Body.Close()
				result, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
				if err != nil {
					t.Fatal(err)
				}
				return response.StatusCode, result
			}
			start := func() *exec.Cmd {
				t.Helper()
				cmd := exec.CommandContext(ctx, bin, "apply", stackPath, "--daemon-child", "--port", strconv.Itoa(port), "--verbose")
				cmd.Env, cmd.Stdout, cmd.Stderr = env, logFile, logFile
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				})
				waitForPort(t, ctx, port)
				for {
					status, _ := request("POST", "/api/reload", nil)
					if status == http.StatusOK {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("reload never became ready")
					case <-time.After(25 * time.Millisecond):
					}
				}
				return cmd
			}
			stop := func(cmd *exec.Cmd) {
				t.Helper()
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				if err := cmd.Wait(); err != nil {
					t.Fatalf("daemon did not shut down cleanly: %v", err)
				}
			}
			call := func(tool string, args map[string]any) map[string]any {
				t.Helper()
				_, body := request("POST", "/api/tools/call", map[string]any{"name": "agent__" + tool, "arguments": args})
				var response struct {
					Result *mcp.ToolCallResult `json:"result"`
				}
				if json.Unmarshal(body, &response) != nil {
					t.Fatal("invalid daemon call envelope")
				}
				return a2aAdapterEnvelope(t, response.Result)
			}
			daemon := start()
			first := call("send", map[string]any{"message": "first", "return_immediately": true})
			if first["task_handle"] == nil {
				t.Fatal("A2A-only daemon did not issue authority")
			}
			writeStack(stack("5m", "# unrelated authored edit\n"))
			if status, _ := request("POST", "/api/reload", nil); status != http.StatusOK {
				t.Fatal("unrelated reload failed")
			}
			if got := call("task_get", map[string]any{"task_handle": first["task_handle"]}); got["task_handle"] != first["task_handle"] {
				t.Fatal("unrelated reload retired live authority")
			}
			writeStack(stack("4m", ""))
			if status, _ := request("POST", "/api/reload", nil); status != http.StatusOK {
				t.Fatal("adapter replacement reload failed")
			}
			before := fixture.calls.Load()
			if got := call("task_get", map[string]any{"task_handle": first["task_handle"]}); got["error"] != "capability_unavailable" || fixture.calls.Load() != before {
				t.Fatal("replacement reused old authority")
			}
			second := call("send", map[string]any{"message": "replacement"})
			if second["task_handle"] == nil {
				t.Fatal("identity-preserving replacement did not retain card trust")
			}
			stop(daemon)
			daemon = start()
			before = fixture.calls.Load()
			if got := call("task_get", map[string]any{"task_handle": second["task_handle"]}); got["error"] != "capability_unavailable" || fixture.calls.Load() != before {
				t.Fatal("restart restored capability authority")
			}
			third := call("send", map[string]any{"message": "restart"})
			if third["task_handle"] == nil {
				t.Fatal("restart did not retain card trust")
			}
			fixture.revision.Add(1)
			before = fixture.calls.Load()
			if got := call("task_get", map[string]any{"task_handle": third["task_handle"]}); got["error"] != "card_approval_required" || fixture.calls.Load() != before {
				t.Fatal("disabled legacy pinning bypassed mandatory card trust")
			}
			status, body := request("GET", "/api/pins/agent/diff", nil)
			var diff struct {
				Hash string `json:"live_server_hash"`
			}
			if status != http.StatusOK || json.Unmarshal(body, &diff) != nil || diff.Hash == "" {
				t.Fatal("pending card unavailable with legacy pinning disabled")
			}
			if status, _ := request("POST", "/api/pins/agent/approve", map[string]any{}); status != http.StatusBadRequest {
				t.Fatal("unbound approval accepted")
			}
			if status, _ := request("POST", "/api/pins/agent/approve", map[string]any{"expected_server_hash": diff.Hash}); status != http.StatusOK {
				t.Fatal("hash-bound approval unavailable with legacy pinning disabled")
			}
			if got := call("task_get", map[string]any{"task_handle": third["task_handle"]}); got["error"] != "capability_unavailable" {
				t.Fatal("approval revived pre-drift capability")
			}
			if got := call("send", map[string]any{"message": "approved"}); got["task_handle"] == nil {
				t.Fatal("approved card not callable")
			}
			stop(daemon)
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{token, first["task_handle"].(string), first["context_handle"].(string), second["task_handle"].(string), third["task_handle"].(string)} {
				if strings.Contains(string(logs), secret) {
					t.Fatal("verbose daemon logs disclosed credentials or authority")
				}
			}
		})
	}
}
