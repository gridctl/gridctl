//go:build integration && !windows

package integration

import (
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

	"github.com/gridctl/gridctl/pkg/state"
)

func TestGatewayAuth_RealRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	build := func(source, name string) string {
		bin := filepath.Join(dir, name)
		cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, ".")
		cmd.Dir = source
		if err := cmd.Run(); err != nil {
			t.Fatalf("race build failed: %v", err)
		}
		return bin
	}
	bin := build(filepath.Join(root, "cmd/gridctl"), "gridctl")
	backendBin := build(filepath.Join(root, "examples/_mock-servers/mock-mcp-server"), "backend")
	for _, key := range []string{"HOME", "GRIDCTL_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "DOCKER_CONFIG"} {
		t.Setenv(key, filepath.Join(dir, key))
	}
	t.Setenv("GRIDCTL_AUTH_FIXTURE", rand.Text())
	oldToken := os.Getenv("GRIDCTL_AUTH_FIXTURE")
	childEnv := []string{"PATH=" + os.Getenv("PATH"), "GRIDCTL_AUTH_FIXTURE=" + oldToken}
	for _, key := range []string{"HOME", "GRIDCTL_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "DOCKER_CONFIG"} {
		childEnv = append(childEnv, key+"="+os.Getenv(key))
	}
	backendPort := freePort(t)
	backend := exec.CommandContext(ctx, backendBin, "-port", strconv.Itoa(backendPort))
	backend.Env = childEnv
	if err := backend.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Process.Kill(); _ = backend.Wait() })
	waitForPort(t, ctx, backendPort)
	name := "auth-" + strings.ToLower(rand.Text()[:8])
	image := "gridctl-" + name + ":fixture"
	network := "gridctl-" + name
	container := "gridctl-" + name + "-sentinel"
	docker := func(args ...string) []byte {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = childEnv
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("fixture Docker operation failed: %v", err)
		}
		return out
	}
	imageBuild := exec.CommandContext(ctx, "docker", "build", "--pull=false", "-t", image, "-")
	imageBuild.Env = childEnv
	imageBuild.Stdin = strings.NewReader("FROM alpine:latest\nCMD [\"sleep\", \"3600\"]\n")
	if err := imageBuild.Run(); err != nil {
		t.Fatalf("fixture image build: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		for _, args := range [][]string{{"rm", "-f", container}, {"network", "rm", network}, {"image", "rm", image}} {
			cmd := exec.CommandContext(cleanupCtx, "docker", args...)
			cmd.Env = childEnv
			_ = cmd.Run()
		}
	})
	docker("network", "create", "--label", "gridctl.managed=true", "--label", "gridctl.stack="+name, network)
	docker("run", "-d", "--name", container, "--network", network, "--label", "gridctl.managed=true", "--label", "gridctl.stack="+name, "--label", "gridctl.resource=sentinel", image)
	stackPath := filepath.Join(dir, "stack.yaml")
	base := fmt.Sprintf("name: %s\ngateway:\n  auth:\n    type: bearer\n    token: ${GRIDCTL_AUTH_FIXTURE}\nmcp-servers:\n  - name: backend\n    url: http://127.0.0.1:%d/mcp\n    transport: http\n", name, backendPort)
	base += fmt.Sprintf("network:\n  name: %s\nresources:\n  - name: sentinel\n    image: %s\n", network, image)
	if err := os.WriteFile(stackPath, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	waitLog := func(fragment string) {
		for {
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), fragment) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("expected daemon event not observed")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, path, token, body, session string) *http.Response {
		req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json, text/event-stream")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
			if method == "GET" {
				req.Header.Set("Last-Event-ID", "0")
			}
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("fixture HTTP request failed: %v", err)
		}
		return res
	}
	start := func(activeToken string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, bin, "apply", stackPath, "--daemon-child", "--watch", "--port", strconv.Itoa(port))
		cmd.Env = childEnv
		cmd.Stderr = logFile
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
			res := request("POST", "/api/reload", activeToken, "", "")
			_ = res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("reload handler never became ready")
			case <-time.After(50 * time.Millisecond):
			}
		}
		return cmd
	}
	stop := func(cmd *exec.Cmd) {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err != nil {
			logs, _ := os.ReadFile(logPath)
			detail := strings.ReplaceAll(string(logs), oldToken, "[redacted]")
			if token := os.Getenv("GRIDCTL_AUTH_FIXTURE_NEXT"); token != "" {
				detail = strings.ReplaceAll(detail, token, "[redacted]")
			}
			t.Fatalf("gateway did not shut down cleanly: %v\n%s", err, detail)
		}
	}
	daemon := start(oldToken)
	waitLog("watching for config changes")
	otherName := name + "-other"
	otherPath := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(otherPath, []byte("name: "+otherName+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	other := exec.CommandContext(ctx, bin, "apply", otherPath, "--daemon-child", "--port", strconv.Itoa(freePort(t)))
	other.Env = childEnv
	other.Stderr = logFile
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if other.ProcessState == nil {
			_ = other.Process.Kill()
			_ = other.Wait()
		}
	})
	for {
		ready := exec.CommandContext(ctx, bin, "reload", otherName, "--format", "json")
		ready.Env = childEnv
		if ready.Run() == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second daemon never became ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
	containerID := string(docker("inspect", "--format", "{{.Id}}:{{.State.Running}}", container))
	networkID := string(docker("network", "inspect", "--format", "{{.Id}}", network))
	res := request("POST", "/mcp", oldToken, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, "")
	session := res.Header.Get("Mcp-Session-Id")
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if session == "" {
		t.Fatal("no process-owned session established")
	}
	stream := request("GET", "/mcp", oldToken, "", session)
	if stream.StatusCode != 200 {
		t.Fatal("stateful stream not established")
	}
	streamEnded := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, stream.Body); _ = stream.Body.Close(); close(streamEnded) }()
	newToken := rand.Text()
	t.Setenv("GRIDCTL_AUTH_FIXTURE_NEXT", newToken)
	candidate := strings.Replace(base, "${GRIDCTL_AUTH_FIXTURE}", "${GRIDCTL_AUTH_FIXTURE_NEXT}", 1)
	// The running process cannot see a changed invoking-shell environment.
	// A literal generated inside the isolated fixture is observable on reload.
	candidate = strings.Replace(candidate, "${GRIDCTL_AUTH_FIXTURE_NEXT}", newToken, 1)
	if err := os.WriteFile(stackPath, []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	waitLog("reload failed")
	waitLog("code=restart_required")
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logs), oldToken) || strings.Contains(string(logs), newToken) {
		t.Fatal("daemon diagnostics disclosed a credential")
	}
	for attempt := range 3 {
		if attempt == 2 {
			mixed := strings.Replace(candidate, image, "example.invalid/must-not-start:fixture", 1) + "groups:\n  rejected:\n    servers: [backend]\n"
			if err := os.WriteFile(stackPath, []byte(mixed), 0600); err != nil {
				t.Fatal(err)
			}
		}
		direct := request("POST", "/api/reload", oldToken, "", "")
		directBody, _ := io.ReadAll(direct.Body)
		_ = direct.Body.Close()
		if !strings.Contains(string(directBody), "restart_required") {
			detail := strings.ReplaceAll(strings.ReplaceAll(string(directBody), oldToken, "[redacted]"), newToken, "[redacted]")
			t.Fatalf("direct reload HTTP %d: %s", direct.StatusCode, detail)
		}
		args := []string{"reload", name, "--format", "json"}
		if attempt == 2 {
			args = []string{"reload", "--format", "json"}
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = childEnv
		out, err := cmd.Output()
		if err == nil {
			t.Fatal("CLI accepted static-security change")
		}
		var result struct {
			Stack   string `json:"stack"`
			Code    string `json:"code"`
			Success bool   `json:"success"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(out)))
		found, ordinary := false, false
		for decoder.More() {
			if decoder.Decode(&result) != nil {
				break
			}
			if result.Stack == name && !result.Success && result.Code == "restart_required" {
				found = true
			}
			if result.Stack == otherName && result.Success {
				ordinary = true
			}
		}
		if !found || (attempt == 2 && !ordinary) {
			detail := string(out)
			if exit, ok := err.(*exec.ExitError); ok {
				detail += string(exit.Stderr)
			}
			detail = strings.ReplaceAll(strings.ReplaceAll(detail, oldToken, "[redacted]"), newToken, "[redacted]")
			t.Fatalf("missing structured CLI refusal: %s", detail)
		}
		if strings.Contains(string(out), newToken) || strings.Contains(string(out), oldToken) {
			t.Fatal("CLI disclosed a credential")
		}
		if string(docker("inspect", "--format", "{{.Id}}:{{.State.Running}}", container)) != containerID || string(docker("network", "inspect", "--format", "{{.Id}}", network)) != networkID {
			t.Fatal("rejection changed fixture runtime resources")
		}
		active, err := state.Load(name)
		if err != nil || active.AuthToken != oldToken {
			t.Fatal("rejection changed daemon credential state")
		}
		group := request("GET", "/groups/rejected/mcp", oldToken, "", "")
		_ = group.Body.Close()
		if group.StatusCode != 404 {
			t.Fatal("rejected group policy became active")
		}
	}
	if err := os.WriteFile(stackPath, []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	res = request("GET", "/api/status", oldToken, "", "")
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("old credential stopped working before restart")
	}
	res = request("GET", "/api/status", newToken, "", "")
	_ = res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("candidate activated without restart")
	}
	select {
	case <-streamEnded:
		t.Fatal("stateful stream ended before restart")
	default:
	}
	stop(daemon)
	select {
	case <-streamEnded:
	case <-time.After(3 * time.Second):
		t.Fatal("old process stream survived shutdown")
	}
	if err := backend.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("downstream process was destroyed")
	}
	daemon = start(newToken)
	if string(docker("inspect", "--format", "{{.Id}}:{{.State.Running}}", container)) != containerID {
		t.Fatal("restart destroyed the downstream resource")
	}
	res = request("GET", "/api/status", newToken, "", "")
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("new credential not activated by restart")
	}
	res = request("GET", "/api/status", oldToken, "", "")
	_ = res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("old credential accepted after restart")
	}
	res = request("POST", "/mcp", newToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, session)
	_ = res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal("old process-owned session survived restart")
	}
	stop(daemon)
	stop(other)
	stackless := exec.CommandContext(ctx, bin, "serve", "--daemon-child", "--port", strconv.Itoa(port))
	stackless.Env = childEnv
	stackless.Stderr = logFile
	if err := stackless.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stackless.ProcessState == nil {
			_ = stackless.Process.Kill()
			_ = stackless.Wait()
		}
	})
	waitForPort(t, ctx, port)
	// Wait for composition to expose initialization before making candidates.
	for {
		probe := request("POST", "/api/reload", "", "", "")
		status := probe.StatusCode
		_ = probe.Body.Close()
		if status != http.StatusServiceUnavailable {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("stackless handler never became ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
	for _, secured := range []bool{true, false} {
		yaml := "name: initial\n"
		if secured {
			yaml += "gateway:\n  auth:\n    type: bearer\n    token: ${GRIDCTL_AUTH_FIXTURE}\n"
		}
		body, err := json.Marshal(map[string]string{"name": "initial", "yaml": yaml})
		if err != nil {
			t.Fatal(err)
		}
		saved := request("POST", "/api/stacks", "", string(body), "")
		_ = saved.Body.Close()
		if saved.StatusCode != 200 {
			t.Fatal("candidate save failed")
		}
		initialized := request("POST", "/api/stack/initialize", "", `{"name":"initial"}`, "")
		var result struct {
			Code    string `json:"code"`
			Success bool   `json:"success"`
		}
		if err := json.NewDecoder(initialized.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		_ = initialized.Body.Close()
		if secured {
			if result.Code != "restart_required" || result.Success {
				t.Fatal("stackless listener accepted new security")
			}
		} else if !result.Success {
			t.Fatal("security-equivalent initialization did not recover")
		}
	}
	stop(stackless)
}
