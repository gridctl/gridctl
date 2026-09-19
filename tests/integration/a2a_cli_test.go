//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestA2AAdapter_CLISubprocess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// An explicit override exercises task build's ./gridctl against these same
	// listeners. Ordinary acceptance builds a fresh race-enabled child binary.
	bin := os.Getenv("GRIDCTL_A2A_FIXTURE_BINARY")
	if bin == "" {
		bin = filepath.Join(dir, "gridctl")
		build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, "./cmd/gridctl")
		build.Dir = root
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("race build failed: %v\n%s", err, output)
		}
	}
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AAdapterFixture(t, version)
			gateway := mcp.NewGateway()
			defer gateway.Close()
			store := pins.NewWithPath(t.TempDir(), "cli")
			if err := gateway.SetCardPinStorage(ctx, store); err != nil {
				t.Fatal(err)
			}
			if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: fixture.server.URL}}); err != nil {
				t.Fatal(err)
			}
			token := rand.Text()
			apiServer := api.NewServer(gateway, nil)
			apiServer.SetAuth("bearer", token, "")
			listener := httptest.NewServer(apiServer.Handler())
			defer listener.Close()
			endpoint, err := url.Parse(listener.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(endpoint.Port())
			if err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			t.Setenv(state.HomeEnv, home)
			if err := state.Save(&state.DaemonState{StackName: "a2a-cli", PID: os.Getpid(), Port: port, AuthType: "bearer", AuthToken: token, Home: home}); err != nil {
				t.Fatal(err)
			}
			call := func(tool string, args map[string]any, wantExit int) map[string]any {
				t.Helper()
				encoded, err := json.Marshal(args)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, "private-args.json")
				if err := os.WriteFile(path, encoded, 0600); err != nil {
					t.Fatal(err)
				}
				command := exec.CommandContext(ctx, bin, "call", "agent__"+tool, "@"+path, "--format", "json", "--stack", "a2a-cli", "--as", "same-label")
				command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				err = command.Run()
				if command.ProcessState == nil {
					t.Fatal("CLI process did not start")
				}
				exit := command.ProcessState.ExitCode()
				if exit != wantExit || err != nil && wantExit == 0 {
					t.Fatalf("CLI exit %d, expected %d", exit, wantExit)
				}
				if strings.Contains(stderr.String(), "gca2a_") || strings.Contains(stderr.String(), token) {
					t.Fatal("CLI diagnostics exposed a bearer secret")
				}
				var response struct {
					Result *mcp.ToolCallResult `json:"result"`
				}
				if json.Unmarshal(stdout.Bytes(), &response) != nil {
					t.Fatal("CLI stdout was not one JSON envelope")
				}
				return a2aAdapterEnvelope(t, response.Result)
			}
			first := call("send", map[string]any{"message": "hello", "return_immediately": true}, 0)
			if first["task_handle"] == nil || first["context_handle"] == nil {
				t.Fatal("CLI stdout lost capability delivery")
			}
			beforeGET, beforeRPC := fixture.gets.Load(), fixture.calls.Load()
			if got := call("task_get", map[string]any{"task_handle": "task-1"}, 2); got["error"] != "capability_unavailable" {
				t.Fatal("CLI label recovered remote work without a capability")
			}
			if fixture.gets.Load() != beforeGET || fixture.calls.Load() != beforeRPC {
				t.Fatal("invalid CLI capability caused outbound I/O")
			}
			if got := call("task_get", map[string]any{"task_handle": first["task_handle"]}, 0); got["task_handle"] != first["task_handle"] || got["context_handle"] != nil {
				t.Fatal("private-file CLI input lost task authority")
			}
			if got := call("task_cancel", map[string]any{"task_handle": first["task_handle"]}, 0); got["state"] != "canceled" {
				t.Fatal("CLI did not deliver remote cancellation observation")
			}
		})
	}
}
