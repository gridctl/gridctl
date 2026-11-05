package controller

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	// readinessPollInterval and readinessRequestTimeout are the historical
	// wait cadence. Do not change them: apply and serve budgets assume this
	// loop, and a faster poll would change when a late exit is observed.
	readinessPollInterval   = 250 * time.Millisecond
	readinessRequestTimeout = 500 * time.Millisecond
	// readinessBodyLimit caps the non-200 body quoted in a timeout message.
	// The live /ready body is a short server name; the cap is for a peer
	// that writes more.
	readinessBodyLimit = 256
)

// appendLogLevelArg forwards a non-default global --log-level to a forked
// daemon child so its log file honors the requested verbosity.
func appendLogLevelArg(args []string, lvl slog.Level) []string {
	if lvl == slog.LevelInfo {
		return args
	}
	return append(args, "--log-level", strings.ToLower(lvl.String()))
}

// DaemonManager handles daemon lifecycle: forking child processes and
// waiting for readiness.
type DaemonManager struct {
	config Config
}

// NewDaemonManager creates a DaemonManager.
func NewDaemonManager(cfg Config) *DaemonManager {
	return &DaemonManager{config: cfg}
}

// DaemonHandle is the parent's view of a forked daemon child.
// Exited receives the single cmd.Wait result and is then closed. A nil
// error means the child exited 0, which is still a startup failure.
// The parent must not call Wait again: a second Wait fails, and an
// unreaped zombie passes a PID liveness check, so this channel is the
// only exit signal.
type DaemonHandle struct {
	PID    int
	Exited <-chan error
}

// DaemonExitedError is returned when the child exits before the endpoint
// returns 200. ExitCode is 0 when Wait returns nil, the process exit code
// when Wait returns *exec.ExitError, and -1 when the wait error has no code.
type DaemonExitedError struct {
	PID      int
	ExitCode int
	Err      error
}

func (e *DaemonExitedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil && e.ExitCode < 0 {
		return fmt.Sprintf("daemon exited during startup (pid %d, exit status %s): %v", e.PID, formatExitStatus(e.ExitCode), e.Err)
	}
	return fmt.Sprintf("daemon exited during startup (pid %d, exit status %s)", e.PID, formatExitStatus(e.ExitCode))
}

// Unwrap returns the Wait result so callers can inspect *exec.ExitError.
func (e *DaemonExitedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ReadinessTimeoutError is returned when the wait budget expires and the
// child has not exited. LastStatus and LastBody come from the most recent
// non-200 response and are zero values when none arrived. ChildAlive is
// the non-blocking exit check at the deadline. WaitForReady and
// WaitForHealth convert a deadline that races an exit into
// DaemonExitedError, so a timeout they return has ChildAlive set.
type ReadinessTimeoutError struct {
	PID        int
	Port       int
	Timeout    time.Duration
	LastStatus int
	LastBody   string
	ChildAlive bool
	// Endpoint is "ready" or "health", selecting the Error text.
	Endpoint string
}

func (e *ReadinessTimeoutError) Error() string {
	if e == nil {
		return ""
	}
	if e.Endpoint == "health" {
		return fmt.Sprintf("health check timed out after %v", e.Timeout)
	}
	return fmt.Sprintf("readiness check timed out after %v", e.Timeout)
}

// Fork starts a daemon child process that runs the MCP gateway in the background.
// The returned handle is the only waiter for the child.
func (d *DaemonManager) Fork(stack *config.Stack) (DaemonHandle, error) {
	exe, err := os.Executable()
	if err != nil {
		return DaemonHandle{}, fmt.Errorf("getting executable: %w", err)
	}

	if err := state.EnsureLogDir(); err != nil {
		return DaemonHandle{}, fmt.Errorf("creating log directory: %w", err)
	}

	logPath, err := state.LogPath(stack.Name)
	if err != nil {
		return DaemonHandle{}, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return DaemonHandle{}, fmt.Errorf("opening log file: %w", err)
	}

	args := []string{"apply", d.config.StackPath,
		"--daemon-child",
		"--port", strconv.Itoa(d.config.Port),
		"--base-port", strconv.Itoa(d.config.BasePort)}
	if d.config.NoExpand {
		args = append(args, "--no-expand")
	}
	if d.config.Watch {
		args = append(args, "--watch")
	}
	if d.config.NoOverride {
		args = append(args, "--no-override")
	} else if d.config.OverridePath != "" {
		args = append(args, "--override", d.config.OverridePath)
		if !d.config.OverrideRequired {
			args = append(args, "--override-optional")
		}
	}
	if d.config.LogFile != "" {
		args = append(args, "--log-file", d.config.LogFile)
	}
	args = appendLogLevelArg(args, effectiveLogLevel(d.config))
	cmd := exec.Command(exe, args...)

	configureDaemonProcess(cmd)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()

	return startDaemon(cmd, logFile)
}

// ForkStackless starts a stackless daemon child that runs only the HTTP API
// and web UI (no stack, no containers). The returned handle is the only
// waiter for the child.
func (d *DaemonManager) ForkStackless() (DaemonHandle, error) {
	exe, err := os.Executable()
	if err != nil {
		return DaemonHandle{}, fmt.Errorf("getting executable: %w", err)
	}

	if err := state.EnsureLogDir(); err != nil {
		return DaemonHandle{}, fmt.Errorf("creating log directory: %w", err)
	}

	logPath, err := state.LogPath("gridctl")
	if err != nil {
		return DaemonHandle{}, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return DaemonHandle{}, fmt.Errorf("opening log file: %w", err)
	}

	args := []string{"serve",
		"--daemon-child",
		"--port", strconv.Itoa(d.config.Port),
	}
	if d.config.LogFile != "" {
		args = append(args, "--log-file", d.config.LogFile)
	}
	args = appendLogLevelArg(args, effectiveLogLevel(d.config))
	cmd := exec.Command(exe, args...)

	configureDaemonProcess(cmd)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()

	return startDaemon(cmd, logFile)
}

// startDaemon starts cmd with stdout and stderr already pointed at logFile.
// The parent closes its copy of the log file; the child keeps its own
// descriptor, so Wait has no pipes to drain. Exactly one goroutine calls
// Wait.
func startDaemon(cmd *exec.Cmd, logFile *os.File) (DaemonHandle, error) {
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return DaemonHandle{}, fmt.Errorf("starting daemon: %w", err)
	}
	logFile.Close()
	return reapDaemon(cmd), nil
}

// reapDaemon publishes cmd.Wait on a buffered channel and closes it.
// The goroutine holds no lock and no file. After a successful readiness
// wait the parent process exits and takes the goroutine with it; a
// long-lived parent (the pack launcher) leaves the goroutine parked in
// Wait for the daemon's lifetime.
func reapDaemon(cmd *exec.Cmd) DaemonHandle {
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		close(exited)
	}()
	return DaemonHandle{PID: cmd.Process.Pid, Exited: exited}
}

// WaitForReady polls the /ready endpoint until it returns 200, the child
// exits, or timeout. The /ready endpoint only succeeds when all MCP servers
// are initialized, unlike /health which succeeds immediately when the HTTP
// server starts. A nil or never-closed Exited channel is treated as alive.
func (d *DaemonManager) WaitForReady(port int, timeout time.Duration, handle DaemonHandle) error {
	return d.waitForHTTP(port, timeout, handle, "/ready", "ready")
}

// WaitForHealth polls the /health endpoint until it returns 200, the child
// exits, or timeout. Used for stackless mode where /ready always returns 503.
func (d *DaemonManager) WaitForHealth(port int, timeout time.Duration, handle DaemonHandle) error {
	return d.waitForHTTP(port, timeout, handle, "/health", "health")
}

func (d *DaemonManager) waitForHTTP(port int, timeout time.Duration, handle DaemonHandle, path, endpoint string) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: readinessRequestTimeout}
	url := fmt.Sprintf("http://localhost:%d%s", port, path)

	var lastStatus int
	var lastBody string

	for time.Now().Before(deadline) {
		if err, ok := consumeExit(handle.Exited); ok {
			return newDaemonExitedError(handle.PID, err)
		}
		resp, err := client.Get(url)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, readinessBodyLimit))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastStatus = resp.StatusCode
			// A short read still names the blocking server when the prefix arrived.
			if readErr == nil || len(body) > 0 {
				lastBody = string(body)
			}
		}
		// An exit during this interval, including one that straddles the
		// deadline, is an exit, not a timeout. The child is not signaled.
		if err, ok := waitExit(handle.Exited, readinessPollInterval); ok {
			return newDaemonExitedError(handle.PID, err)
		}
	}
	if err, ok := consumeExit(handle.Exited); ok {
		return newDaemonExitedError(handle.PID, err)
	}
	return &ReadinessTimeoutError{
		PID:        handle.PID,
		Port:       port,
		Timeout:    timeout,
		LastStatus: lastStatus,
		LastBody:   lastBody,
		ChildAlive: true,
		Endpoint:   endpoint,
	}
}

// consumeExit is a non-blocking receive. The boolean is false when the
// channel is nil, not ready, or already closed and empty. A received nil
// error is still an exit (the child exited 0).
func consumeExit(exited <-chan error) (error, bool) {
	if exited == nil {
		return nil, false
	}
	select {
	case err, ok := <-exited:
		if !ok {
			return nil, false
		}
		return err, true
	default:
		return nil, false
	}
}

// waitExit waits up to d for the child to exit. A nil or already-closed
// channel waits out the interval so the poll loop cannot spin.
func waitExit(exited <-chan error, d time.Duration) (error, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	if exited == nil {
		<-timer.C
		return nil, false
	}
	select {
	case err, ok := <-exited:
		if !ok {
			<-timer.C
			return nil, false
		}
		return err, true
	case <-timer.C:
		return nil, false
	}
}

func newDaemonExitedError(pid int, err error) *DaemonExitedError {
	code := 0
	if err != nil {
		code = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return &DaemonExitedError{PID: pid, ExitCode: code, Err: err}
}

func formatExitStatus(code int) string {
	if code < 0 {
		return "unknown"
	}
	return strconv.Itoa(code)
}
