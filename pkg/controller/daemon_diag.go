package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/vault"
)

const (
	daemonLogTailLines = 20
	daemonLogTailBytes = 4096
)

// daemonFailureOptions selects the user-facing text for a failed readiness
// or health wait. endpoint is "ready" (apply) or "health" (serve).
type daemonFailureOptions struct {
	logPath  string
	logsName string
	stopCmd  string
	endpoint string
	vault    *vault.Store
}

// formatDaemonStartupFailure builds the two startup-failure messages.
// Vault values are registered before the log is read: a quiet or silent
// parent never reaches the registration in setupOrchestratorLogging, and
// the child's fatal line is written with fmt.Fprintf, outside the redacting
// slog handler. The tail re-emits text already on disk, so it stays bounded.
func formatDaemonStartupFailure(waitErr error, opts daemonFailureOptions) error {
	if waitErr == nil {
		return nil
	}
	if opts.vault != nil {
		logging.RegisterSensitiveValues(opts.vault.Values()...)
	}
	tail := ""
	if opts.logPath != "" {
		tail = tailLogFile(opts.logPath, daemonLogTailLines, daemonLogTailBytes)
	}

	var exited *DaemonExitedError
	var timeout *ReadinessTimeoutError
	switch {
	case errors.As(waitErr, &exited):
		return fmt.Errorf("%s", formatExitedMessage(exited, opts, tail))
	case errors.As(waitErr, &timeout):
		if !timeout.ChildAlive {
			return fmt.Errorf("%s", formatExitedMessage(&DaemonExitedError{
				PID:      timeout.PID,
				ExitCode: -1,
			}, opts, tail))
		}
		return fmt.Errorf("%s", formatTimeoutMessage(timeout, opts))
	default:
		prefix := "daemon failed to become ready"
		if opts.endpoint == "health" {
			prefix = "daemon failed to start"
		}
		return fmt.Errorf("%s: %w\nCheck logs at %s", prefix, waitErr, opts.logPath)
	}
}

func formatExitedMessage(exited *DaemonExitedError, opts daemonFailureOptions, tail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gateway daemon exited during startup (PID %d, exit status %s)", exited.PID, formatExitStatus(exited.ExitCode))
	if tail != "" && opts.logPath != "" {
		fmt.Fprintf(&b, "\nLast lines of %s:\n%s", opts.logPath, tail)
	}
	if opts.logPath != "" {
		fmt.Fprintf(&b, "\nCheck logs at %s", opts.logPath)
	}
	fmt.Fprintf(&b, "\nRun 'gridctl logs %s' for the full log", logsName(opts))
	return b.String()
}

func formatTimeoutMessage(timeout *ReadinessTimeoutError, opts daemonFailureOptions) string {
	var b strings.Builder
	if opts.endpoint == "health" {
		fmt.Fprintf(&b, "daemon failed to start: %s", timeout.Error())
	} else {
		fmt.Fprintf(&b, "daemon failed to become ready: %s", timeout.Error())
	}
	kind := "readiness"
	if opts.endpoint == "health" {
		kind = "health"
	}
	fmt.Fprintf(&b, "\nThe gateway process is still running (PID %d, port %d); last %s response: %s",
		timeout.PID, timeout.Port, kind, formatLastResponse(timeout.LastStatus, timeout.LastBody))
	fmt.Fprintf(&b, "\nIt may still become ready; check with 'gridctl status' or 'gridctl logs %s -f'", logsName(opts))
	fmt.Fprintf(&b, "\nTo stop it: %s", opts.stopCmd)
	if opts.logPath != "" {
		fmt.Fprintf(&b, "\nCheck logs at %s", opts.logPath)
	}
	return b.String()
}

func logsName(opts daemonFailureOptions) string {
	if opts.logsName == "" {
		return "gridctl"
	}
	return opts.logsName
}

func formatLastResponse(status int, body string) string {
	body = logging.RedactString(strings.TrimSpace(body))
	body = strings.ReplaceAll(body, "\r\n", " ")
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.ReplaceAll(body, "\r", " ")
	if status == 0 {
		return "no response"
	}
	if body == "" {
		return fmt.Sprintf("%d", status)
	}
	return fmt.Sprintf("%d %s", status, body)
}

// tailLogFile returns the last maxLines lines of path, capped at maxBytes,
// with each line passed through logging.RedactString. A missing or
// unreadable file yields an empty string so the original failure stays
// visible. The byte cap keeps the newest bytes.
func tailLogFile(path string, maxLines, maxBytes int) string {
	if path == "" || maxLines <= 0 || maxBytes <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	// A startup line is rarely longer than a few hundred bytes. Reading
	// this much from the end recovers the last maxLines before the byte
	// cap, without loading a large log into memory.
	want := maxLines * 512
	if want < maxBytes {
		want = maxBytes
	}
	want++
	size := info.Size()
	offset := int64(0)
	if size > int64(want) {
		offset = size - int64(want)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(want)))
	if err != nil {
		return ""
	}
	if offset > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	for i, line := range lines {
		lines[i] = logging.RedactString(line)
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
		for len(out) > 0 && !utf8.RuneStart(out[0]) {
			out = out[1:]
		}
	}
	return out
}
