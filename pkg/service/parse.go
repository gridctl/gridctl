package service

import (
	"strconv"
	"strings"
)

// ParseSystemctlShow parses `systemctl show -p ...` key=value lines.
// running requires ActiveState=active and SubState=running. A unit the
// manager does not know is not-loaded.
func ParseSystemctlShow(output string) RuntimeState {
	fields := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		fields[key] = value
	}
	state := RuntimeState{State: "unknown", Detail: fields["ActiveState"]}
	switch fields["ActiveState"] {
	case "active":
		if fields["SubState"] == "running" {
			state.State = "running"
		} else {
			state.State = "inactive"
		}
	case "failed":
		state.State = "failed"
	case "inactive":
		state.State = "inactive"
	case "":
		if strings.Contains(strings.ToLower(output), "not found") || fields["LoadState"] == "not-found" {
			state.State = "not-loaded"
		}
	}
	if fields["LoadState"] == "not-found" || fields["UnitFileState"] == "not-found" {
		state.State = "not-loaded"
	}
	switch fields["UnitFileState"] {
	case "enabled", "enabled-runtime":
		state.Enabled = true
	}
	if pid, err := strconv.Atoi(fields["MainPID"]); err == nil && pid > 0 {
		state.PID = pid
	}
	return state
}

// ParseLaunchctlPrint parses `launchctl print` loosely. Only `state = `
// and `pid = ` are read. Anything else is unknown rather than an error,
// because the text format is not stable across macOS versions.
func ParseLaunchctlPrint(output string) RuntimeState {
	state := RuntimeState{State: "unknown"}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "could not find service") || strings.Contains(lower, "no such process") {
		state.State = "not-loaded"
		return state
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "state = "); ok {
			switch strings.TrimSpace(rest) {
			case "running":
				state.State = "running"
				state.Enabled = true
			case "spawn scheduled", "waiting":
				// Present in the domain but not running.
				state.State = "inactive"
				state.Enabled = true
			default:
				if state.State == "unknown" {
					state.State = "inactive"
					state.Enabled = true
				}
			}
		}
		if rest, ok := strings.CutPrefix(trimmed, "pid = "); ok {
			if pid, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && pid > 0 {
				state.PID = pid
			}
		}
	}
	if state.State == "running" || state.PID > 0 {
		state.Enabled = true
	}
	return state
}

// ParseLinger reads `Linger=yes` or `Linger=no` from loginctl. Anything
// else is unknown.
func ParseLinger(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key == "Linger" {
			switch value {
			case "yes", "no":
				return value
			}
		}
	}
	return "unknown"
}


