package main

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestServe_ExitsOnSIGTERM forks a real `gridctl serve --daemon-child` and
// asserts that SIGTERM causes the process to exit within the graceful
// shutdown window. This guards against regressions where ctx-bound
// goroutines (health monitor, autoscaler, agent IDE watcher, file watcher)
// hold the process alive after the signal is received.
func TestServe_ExitsOnSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon mode and SIGTERM semantics are POSIX-specific")
	}

	// Build the binary before redirecting HOME so `go build` resolves its
	// real module cache rather than poisoning the test's tempdir with
	// read-only mod-cache files that t.TempDir() RemoveAll can't clean.
	binPath := filepath.Join(t.TempDir(), "gridctl")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Stderr = &bytes.Buffer{}
	if err := build.Run(); err != nil {
		t.Fatalf("building gridctl: %v\n%s", err, build.Stderr.(*bytes.Buffer).String())
	}

	t.Setenv("HOME", t.TempDir())

	port := freePort(t)

	var out bytes.Buffer
	cmd := exec.Command(binPath, "serve", "--daemon-child", "--port", strconv.Itoa(port))
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting daemon: %v", err)
	}

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	if !waitHealth(port, 10*time.Second) {
		t.Fatalf("daemon never reached healthy state on :%d\n%s", port, out.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-waitDone:
		// Process exited — that's the expected outcome. We don't assert
		// on the exit error since cmd.Wait() returns non-nil for any
		// non-zero exit including the SIGTERM-driven graceful shutdown
		// path on some platforms.
	case <-time.After(20 * time.Second):
		t.Fatalf("daemon did not exit within 20s of SIGTERM\n%s", out.String())
	}

	// State file must be gone after the daemon exits — proves the
	// defer in runStacklessDaemonChild fired. If the file lingers, an
	// orphan daemon would still be discoverable by `gridctl stop`, but
	// this assertion specifically guards the happy-path lifecycle.
	statePath := filepath.Join(os.Getenv("HOME"), ".gridctl", "state", "gridctl.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected state file to be absent after exit, got err=%v", err)
	}
}

// freePort finds an unused TCP port by binding to :0 and closing the
// listener. There is an unavoidable race between close and the daemon's
// bind, but it is acceptable for an integration test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitHealth polls /health until it returns 200 or the deadline expires.
func waitHealth(port int, timeout time.Duration) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			ok := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if ok {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
