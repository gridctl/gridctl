//go:build !windows

package state

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FindOrphan looks for an orphan gridctl daemon listening on port — a
// process that owns the port but has no managed state file (because
// e.g. an earlier shutdown deleted state mid-way, or the daemon was
// started in a mode that doesn't write state, like 'serve --foreground').
//
// Returns (pid, true, nil) only when all three signals agree:
//  1. The /health endpoint on port responds 200.
//  2. Exactly one process (after excluding our own PID) holds the TCP
//     listen socket for that port.
//  3. That process's executable basename is "gridctl".
//
// Any ambiguity — health probe fails, no listener, multiple listeners
// after self-exclusion, or executable name mismatch — returns ok=false
// so the caller falls through to the legacy behavior rather than acting
// on guesswork.
func FindOrphan(port int) (int, bool, error) {
	if !probeHealthFn(port) {
		return 0, false, nil
	}

	pids, err := listenerForPort(port)
	if err != nil {
		return 0, false, err
	}

	self := os.Getpid()
	var candidates []int
	for _, pid := range pids {
		if pid == self {
			continue
		}
		candidates = append(candidates, pid)
	}
	if len(candidates) != 1 {
		return 0, false, nil
	}

	exe, err := executableForPID(candidates[0])
	if err != nil || exe != "gridctl" {
		return 0, false, nil
	}
	return candidates[0], true, nil
}

// Seams: package-level vars so tests can substitute fakes without
// shelling out. The cmd/gridctl/stop_test.go file uses the same pattern
// for findOrphan.
var (
	probeHealthFn    = probeHealth
	listenerForPort  = lookupListenerPort
	executableForPID = lookupExecutable
)

func probeHealth(port int) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// lookupListenerPort returns the PIDs currently holding a TCP listen
// socket on port. Uses `lsof -nP -iTCP:<port> -sTCP:LISTEN -t`, whose
// -t flag emits one numeric PID per line and nothing else. Exit code 1
// from lsof means "no matches" and is reported as an empty slice rather
// than an error.
func lookupListenerPort(port int) ([]int, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	var pids []int
	for _, line := range strings.Fields(strings.TrimSpace(string(out))) {
		pid, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// lookupExecutable returns the bare executable basename for pid via
// `ps -o comm= -p <pid>`. Linux ps may decorate renamed threads with
// parens; trim them so the returned name compares cleanly to "gridctl".
func lookupExecutable(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(out))
	name = strings.Trim(name, "()")
	// Some ps implementations return the full executable path; the
	// daemon-vs-caller decision only cares about the basename.
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name, nil
}
