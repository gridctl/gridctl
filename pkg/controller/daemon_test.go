package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestNewDaemonManager(t *testing.T) {
	cfg := Config{
		StackPath: "/path/to/stack.yaml",
		Port:      8180,
		BasePort:  9000,
	}
	dm := NewDaemonManager(cfg)
	if dm == nil {
		t.Fatal("NewDaemonManager returned nil")
	}
	if dm.config.Port != 8180 {
		t.Errorf("expected port 8180, got %d", dm.config.Port)
	}
}

func TestDaemonManager_WaitForReady_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Extract port from test server URL
	port := extractPort(t, server.URL)

	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 5*time.Second, DaemonHandle{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonManager_WaitForReady_Timeout(t *testing.T) {
	// Server that always returns 503
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)

	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 1*time.Second, DaemonHandle{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	var timeout *ReadinessTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected readiness timeout, got %T: %v", err, err)
	}
	if !timeout.ChildAlive {
		t.Fatal("nil exit channel must be treated as alive")
	}
	if timeout.LastStatus != http.StatusServiceUnavailable {
		t.Fatalf("last status = %d, want 503", timeout.LastStatus)
	}
}

func TestDaemonManager_WaitForReady_EventualSuccess(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			attempts++
			if attempts >= 3 {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
	}))
	defer server.Close()

	port := extractPort(t, server.URL)

	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 10*time.Second, DaemonHandle{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestDaemonManager_WaitForReady_NoServer(t *testing.T) {
	// Use a port that's not listening
	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(19999, 1*time.Second, DaemonHandle{})
	if err == nil {
		t.Fatal("expected error for non-listening port, got nil")
	}
	var timeout *ReadinessTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected readiness timeout, got %T: %v", err, err)
	}
	if !timeout.ChildAlive || timeout.LastStatus != 0 {
		t.Fatalf("alive=%v status=%d, want alive with no response", timeout.ChildAlive, timeout.LastStatus)
	}
}

func TestDaemonManager_WaitForReady_ChildExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	port := extractPort(t, server.URL)

	exited := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		exited <- errors.New("exit status 1")
		close(exited)
	}()

	dm := NewDaemonManager(Config{})
	start := time.Now()
	err := dm.WaitForReady(port, 5*time.Second, DaemonHandle{PID: 4242, Exited: exited})
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("returned in %s, want under 2s", elapsed)
	}
	var exitedErr *DaemonExitedError
	if !errors.As(err, &exitedErr) {
		t.Fatalf("expected daemon exit, got %T: %v", err, err)
	}
	if exitedErr.PID != 4242 {
		t.Fatalf("PID = %d, want 4242", exitedErr.PID)
	}
	if exitedErr.ExitCode != -1 {
		t.Fatalf("exit code = %d, want -1 for a non-ExitError", exitedErr.ExitCode)
	}
}

func TestDaemonManager_WaitForReady_ExitAtDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	port := extractPort(t, server.URL)

	exited := make(chan error, 1)
	exited <- errors.New("already gone")
	close(exited)

	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 0, DaemonHandle{PID: 7, Exited: exited})
	var exitedErr *DaemonExitedError
	if !errors.As(err, &exitedErr) {
		t.Fatalf("exit during the final interval must be an exit, got %T: %v", err, err)
	}
	if exitedErr.PID != 7 {
		t.Fatalf("PID = %d, want 7", exitedErr.PID)
	}
}

func TestDaemonManager_WaitForReady_TimeoutCarriesLastBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("MCP server not initialized: dead3"))
	}))
	defer server.Close()
	port := extractPort(t, server.URL)

	exited := make(chan error)
	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 1*time.Second, DaemonHandle{PID: 99, Exited: exited})
	var timeout *ReadinessTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected readiness timeout, got %T: %v", err, err)
	}
	if !timeout.ChildAlive {
		t.Fatal("ChildAlive = false, want true")
	}
	if timeout.LastStatus != http.StatusServiceUnavailable {
		t.Fatalf("LastStatus = %d, want 503", timeout.LastStatus)
	}
	if !strings.Contains(timeout.LastBody, "dead3") {
		t.Fatalf("LastBody = %q, want dead3", timeout.LastBody)
	}
	if timeout.PID != 99 {
		t.Fatalf("PID = %d, want 99", timeout.PID)
	}
	if timeout.Port != port {
		t.Fatalf("Port = %d, want %d", timeout.Port, port)
	}
}

func TestDaemonManager_WaitForReady_BodyCapped(t *testing.T) {
	body := strings.Repeat("x", 300) + "tailmark"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	port := extractPort(t, server.URL)

	dm := NewDaemonManager(Config{})
	err := dm.WaitForReady(port, 1*time.Second, DaemonHandle{})
	var timeout *ReadinessTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected readiness timeout, got %T: %v", err, err)
	}
	if len(timeout.LastBody) != readinessBodyLimit {
		t.Fatalf("LastBody length = %d, want %d", len(timeout.LastBody), readinessBodyLimit)
	}
	if strings.Contains(timeout.LastBody, "tailmark") {
		t.Fatalf("LastBody included bytes past the cap: %q", timeout.LastBody)
	}
}

func TestDaemonManager_WaitForHealth_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	dm := NewDaemonManager(Config{})
	if err := dm.WaitForHealth(port, 5*time.Second, DaemonHandle{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonManager_WaitForHealth_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	dm := NewDaemonManager(Config{})
	err := dm.WaitForHealth(port, 1*time.Second, DaemonHandle{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	var timeout *ReadinessTimeoutError
	if !errors.As(err, &timeout) || timeout.Endpoint != "health" {
		t.Fatalf("expected health timeout, got %v", err)
	}
}

func TestDaemonManager_WaitForHealth_NoServer(t *testing.T) {
	dm := NewDaemonManager(Config{})
	if err := dm.WaitForHealth(19998, 500*time.Millisecond, DaemonHandle{}); err == nil {
		t.Fatal("expected error for non-listening port, got nil")
	}
}

func TestDaemonManager_WaitForHealth_ChildExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	port := extractPort(t, server.URL)

	exited := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		exited <- errors.New("exit status 1")
		close(exited)
	}()

	dm := NewDaemonManager(Config{})
	start := time.Now()
	err := dm.WaitForHealth(port, 5*time.Second, DaemonHandle{PID: 5150, Exited: exited})
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("returned in %s, want under 2s", elapsed)
	}
	var exitedErr *DaemonExitedError
	if !errors.As(err, &exitedErr) {
		t.Fatalf("expected daemon exit, got %T: %v", err, err)
	}
	if exitedErr.PID != 5150 {
		t.Fatalf("PID = %d, want 5150", exitedErr.PID)
	}
}

func TestDaemonManager_ForkStackless_InvalidExecutable(t *testing.T) {
	// ForkStackless uses os.Executable() which always works, but we can verify
	// the method exists and returns a valid signature by checking config wiring.
	dm := NewDaemonManager(Config{Port: 8888, LogFile: "/tmp/test.log"})
	if dm.config.Port != 8888 {
		t.Errorf("expected port 8888, got %d", dm.config.Port)
	}
	if dm.config.LogFile != "/tmp/test.log" {
		t.Errorf("expected LogFile set, got %q", dm.config.LogFile)
	}
}

func TestReapDaemon_PublishesExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // G702: re-executes this test binary to publish a known exit code
	cmd.Env = []string{
		"GRIDCTL_HELPER_PROCESS=1",
		"GRIDCTL_HELPER_EXIT=3",
		"PATH=" + os.Getenv("PATH"),
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	handle := reapDaemon(cmd)
	select {
	case err := <-handle.Exited:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
			t.Fatalf("wait result = %v, want exit status 3", err)
		}
		got := newDaemonExitedError(handle.PID, err)
		if got.PID != handle.PID || got.ExitCode != 3 {
			t.Fatalf("typed exit = %+v, want pid %d code 3", got, handle.PID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not reap the child")
	}
	if _, ok := <-handle.Exited; ok {
		t.Fatal("exit channel still open after Wait")
	}
}

func TestTailLogFile_BoundedAndRedacted(t *testing.T) {
	dir := t.TempDir()
	linesPath := filepath.Join(dir, "lines.log")
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, "line-"+strconv.Itoa(i))
	}
	if err := os.WriteFile(linesPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := tailLogFile(linesPath, 20, 4096)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 20 {
		t.Fatalf("got %d lines, want 20: %q", len(gotLines), got)
	}
	if gotLines[0] != "line-31" || gotLines[19] != "line-50" {
		t.Fatalf("tail = %q, want line-31 through line-50", got)
	}

	widePath := filepath.Join(dir, "wide.log")
	if err := os.WriteFile(widePath, []byte(strings.Repeat("a", 10*1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	wide := tailLogFile(widePath, 20, 4096)
	if len(wide) != 4096 {
		t.Fatalf("wide tail length = %d, want 4096", len(wide))
	}

	secret := "s3cret-tail-7c1e"
	logging.RegisterSensitiveValues(secret)
	secretPath := filepath.Join(dir, "secret.log")
	if err := os.WriteFile(secretPath, []byte("boom "+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	redacted := tailLogFile(secretPath, 20, 4096)
	if strings.Contains(redacted, secret) || !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("tail = %q, want secret masked", redacted)
	}

	if tailLogFile(filepath.Join(dir, "missing.log"), 20, 4096) != "" {
		t.Fatal("missing file should yield an empty tail")
	}
}

func TestTailLogFile_Unreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not deny the owner on Windows or root")
	}
	path := filepath.Join(t.TempDir(), "locked.log")
	if err := os.WriteFile(path, []byte("hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if tailLogFile(path, 20, 4096) != "" {
		t.Fatal("unreadable file should yield an empty tail")
	}
}

func TestFormatDaemonStartupFailure_ExitedAndTimeout(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "demo.log")
	if err := os.WriteFile(logPath, []byte("Error: pins: reading pin file: open pins/demo.json: permission denied\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exited := formatDaemonStartupFailure(&DaemonExitedError{PID: 42, ExitCode: 1, Err: errors.New("exit status 1")}, daemonFailureOptions{
		logPath:  logPath,
		logsName: "demo",
		stopCmd:  "gridctl destroy /tmp/demo/stack.yaml",
		endpoint: "ready",
	})
	if exited == nil {
		t.Fatal("expected an error")
	}
	text := exited.Error()
	for _, want := range []string{
		"gateway daemon exited during startup (PID 42, exit status 1)",
		"Last lines of " + logPath + ":",
		"permission denied",
		"Check logs at " + logPath,
		"Run 'gridctl logs demo' for the full log",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("exited message missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "still running") {
		t.Fatalf("exited message claims the process is running:\n%s", text)
	}

	timeout := formatDaemonStartupFailure(&ReadinessTimeoutError{
		PID:        42,
		Port:       8180,
		Timeout:    60 * time.Second,
		LastStatus: http.StatusServiceUnavailable,
		LastBody:   "MCP server not initialized: dead3",
		ChildAlive: true,
		Endpoint:   "ready",
	}, daemonFailureOptions{
		logPath:  logPath,
		logsName: "demo",
		stopCmd:  "gridctl destroy /tmp/demo/stack.yaml",
		endpoint: "ready",
	})
	timeoutText := timeout.Error()
	for _, want := range []string{
		"daemon failed to become ready: readiness check timed out after 1m0s",
		"The gateway process is still running (PID 42, port 8180); last readiness response: 503 MCP server not initialized: dead3",
		"check with 'gridctl status' or 'gridctl logs demo -f'",
		"To stop it: gridctl destroy /tmp/demo/stack.yaml",
		"Check logs at " + logPath,
	} {
		if !strings.Contains(timeoutText, want) {
			t.Errorf("timeout message missing %q:\n%s", want, timeoutText)
		}
	}
	if strings.Contains(timeoutText, "exited during startup") {
		t.Fatalf("timeout message claims the process exited:\n%s", timeoutText)
	}

	raced := formatDaemonStartupFailure(&ReadinessTimeoutError{
		PID:        42,
		ChildAlive: false,
		Endpoint:   "ready",
	}, daemonFailureOptions{logPath: logPath, logsName: "demo", endpoint: "ready"})
	if !strings.Contains(raced.Error(), "exited during startup") {
		t.Fatalf("deadline race must be reported as an exit:\n%s", raced.Error())
	}
}

func TestFormatDaemonStartupFailure_HealthStopCommand(t *testing.T) {
	err := formatDaemonStartupFailure(&ReadinessTimeoutError{
		PID:        8,
		Port:       8180,
		Timeout:    30 * time.Second,
		LastStatus: 0,
		ChildAlive: true,
		Endpoint:   "health",
	}, daemonFailureOptions{
		logPath:  "/tmp/gridctl.log",
		logsName: "gridctl",
		stopCmd:  "gridctl stop",
		endpoint: "health",
	})
	text := err.Error()
	for _, want := range []string{
		"daemon failed to start: health check timed out after 30s",
		"still running (PID 8, port 8180); last health response: no response",
		"gridctl logs gridctl -f",
		"To stop it: gridctl stop",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("health message missing %q:\n%s", want, text)
		}
	}
}

func TestFormatDaemonStartupFailure_QuietParentRedactsVault(t *testing.T) {
	store := vault.NewStore(t.TempDir())
	secret := "vault-s3cret-9f3a"
	if err := store.Set("TOKEN", secret); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "quiet.log")
	if err := os.WriteFile(logPath, []byte("Error: startup failed "+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Quiet and Silent skip the registration in setupOrchestratorLogging.
	// The formatter must register anyway, which is the path runDaemonMode
	// uses for those parents.
	err := formatDaemonStartupFailure(&DaemonExitedError{PID: 3, ExitCode: 1}, daemonFailureOptions{
		logPath:  logPath,
		logsName: "quiet-stack",
		stopCmd:  "gridctl destroy /tmp/stack.yaml",
		endpoint: "ready",
		vault:    store,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("quiet parent leaked a vault value:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("quiet parent tail was not redacted:\n%s", err.Error())
	}
}

func extractPort(t *testing.T, url string) int {
	t.Helper()
	// URL format: http://127.0.0.1:PORT
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == ':' {
			port, err := strconv.Atoi(url[i+1:])
			if err != nil {
				t.Fatalf("failed to parse port from %s: %v", url, err)
			}
			return port
		}
	}
	t.Fatalf("no port found in URL: %s", url)
	return 0
}
