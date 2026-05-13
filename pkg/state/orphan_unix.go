//go:build !windows

package state

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FindOrphan looks for an orphan gridctl daemon listening on port — a
// process that owns the port but has no managed state file (because
// e.g. an earlier shutdown deleted state mid-way, or the daemon was
// started by a pre-fix binary).
//
// Returns (pid, true, nil) only when both signals agree: the /health
// endpoint responds 200 AND pgrep -f finds exactly one matching
// gridctl --daemon-child process for that port. Any ambiguity
// (multiple matches, no /health response, no matching process)
// returns ok=false so the caller can fall through to the legacy
// behavior rather than acting on guesswork.
//
// The pgrep pattern requires --daemon-child, which the parent
// gridctl stop process never sets, so this can never match the
// caller itself.
func FindOrphan(port int) (int, bool, error) {
	if !probeHealth(port) {
		return 0, false, nil
	}
	return runPgrep(port)
}

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

func runPgrep(port int) (int, bool, error) {
	pattern := fmt.Sprintf("gridctl.*--daemon-child.*--port %d", port)
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		// pgrep exit code 1 means "no matches" — not an error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("pgrep: %w", err)
	}

	var pids []int
	for _, line := range strings.Fields(strings.TrimSpace(string(out))) {
		pid, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) != 1 {
		return 0, false, nil
	}
	return pids[0], true, nil
}
