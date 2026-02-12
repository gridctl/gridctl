package controller

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
)

// DaemonManager handles daemon lifecycle: forking child processes and
// waiting for readiness.
type DaemonManager struct {
	config Config
}

// NewDaemonManager creates a DaemonManager.
func NewDaemonManager(cfg Config) *DaemonManager {
	return &DaemonManager{config: cfg}
}

// Fork starts a daemon child process that runs the MCP gateway in the background.
// Returns the child PID.
func (d *DaemonManager) Fork(stack *config.Stack) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("getting executable: %w", err)
	}

	if err := state.EnsureLogDir(); err != nil {
		return 0, fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(state.LogPath(stack.Name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}

	args := []string{"deploy", d.config.StackPath,
		"--daemon-child",
		"--port", strconv.Itoa(d.config.Port),
		"--base-port", strconv.Itoa(d.config.BasePort)}
	if d.config.NoExpand {
		args = append(args, "--no-expand")
	}
	if d.config.Watch {
		args = append(args, "--watch")
	}
	cmd := exec.Command(exe, args...)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("starting daemon: %w", err)
	}

	// Close log file in parent — child has its own file descriptor
	logFile.Close()

	// Don't wait — let it run in background
	return cmd.Process.Pid, nil
}

// WaitForReady polls the /ready endpoint until it returns 200 or timeout.
// The /ready endpoint only succeeds when all MCP servers are initialized,
// unlike /health which succeeds immediately when the HTTP server starts.
func (d *DaemonManager) WaitForReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://localhost:%d/ready", port)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			statusOK := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if statusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("readiness check timed out after %v", timeout)
}
