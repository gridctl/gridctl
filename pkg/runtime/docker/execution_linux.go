//go:build linux

package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/execution"
)

func observeExecution(ctx context.Context, id string, pid int, e *execution.ExecutionContract) ([]execution.Control, error) {
	if pid <= 0 || len(id) != 64 {
		return nil, fmt.Errorf("execution.runtime: trusted process identity unavailable")
	}
	proc, err := os.OpenRoot(fmt.Sprintf("/proc/%d", pid))
	if err != nil {
		return nil, fmt.Errorf("execution.runtime: daemon workload is not locally observable")
	}
	defer proc.Close()
	cgroup, err := executionRead(ctx, proc, "cgroup")
	if err != nil {
		return nil, err
	}
	var group string
	for _, line := range strings.Split(cgroup, "\n") {
		if strings.HasPrefix(line, "0::/") && strings.Contains(line, id) {
			group = strings.TrimPrefix(line, "0::/")
		}
	}
	if group == "" {
		return nil, fmt.Errorf("execution.resources: local cgroup identity cannot be bound to daemon instance")
	}
	root, err := os.OpenRoot("/sys/fs/cgroup")
	if err != nil {
		return nil, fmt.Errorf("execution.resources: cgroup v2 observation unavailable")
	}
	defer root.Close()
	readLimit := func(name string) (string, error) { return executionRead(ctx, root, filepath.Join(group, name)) }
	controls := []execution.Control{}
	check := func(field, requested, observed string, match bool) error {
		outcome := "observed"
		if !match {
			outcome = "mismatch"
		}
		controls = append(controls, execution.Control{Field: field, Requested: requested, Observed: observed, Outcome: outcome, Source: "kernel-proc-cgroup-v2; engine-instance-bound"})
		if !match {
			return fmt.Errorf("execution.%s: required kernel control mismatch", field)
		}
		return nil
	}
	for _, limit := range []struct{ file, field, wanted string }{
		{"memory.max", "memory_bytes", strconv.FormatInt(e.MemoryBytes, 10)},
		{"memory.swap.max", "swap_bytes", "0"},
		{"pids.max", "pids", strconv.FormatInt(e.PIDs, 10)},
	} {
		value, err := readLimit(limit.file)
		if err != nil {
			return controls, err
		}
		if err := check(limit.field, limit.wanted, "kernel limit comparison", strings.TrimSpace(value) == limit.wanted); err != nil {
			return controls, err
		}
	}
	cpu, err := readLimit("cpu.max")
	if err != nil {
		return controls, err
	}
	parts := strings.Fields(cpu)
	var quota, period int64
	if len(parts) == 2 {
		quota, _ = strconv.ParseInt(parts[0], 10, 64)
		period, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if err := check("cpu_millis", strconv.FormatInt(e.CPUMillis, 10), "kernel quota comparison", period >= 1000 && period <= 1000000 && quota > 0 && quota <= 1000000000 && quota*1000 == e.CPUMillis*period); err != nil {
		return controls, err
	}
	peakObservation := execution.Control{Field: "memory_peak_bytes", Requested: "observation only", Outcome: "unknown", Source: "kernel-cgroup-v2"}
	if peak, readErr := readLimit("memory.peak"); readErr == nil {
		if number, parseErr := strconv.ParseUint(strings.TrimSpace(peak), 10, 64); parseErr == nil {
			peakObservation.Observed, peakObservation.Outcome = strconv.FormatUint(number, 10), "observed"
		}
	}
	controls = append(controls, peakObservation)
	status, err := executionRead(ctx, proc, "status")
	if err != nil {
		return controls, err
	}
	mountinfo, err := executionRead(ctx, proc, "mountinfo")
	if err != nil {
		return controls, err
	}
	allowedData := map[string]bool{}
	scratchSizes, observedScratch := map[string]int64{}, map[string]bool{}
	for _, mount := range e.Mounts {
		allowedData[mount.Target] = mount.ReadOnly != nil && !*mount.ReadOnly
	}
	for _, mount := range e.Tmpfs {
		allowedData[mount.Target] = true
		scratchSizes[mount.Target] = mount.SizeBytes
	}
	rootObserved, mountsMatch := false, true
	for _, line := range strings.Split(strings.TrimSpace(mountinfo), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 7 {
			mountsMatch = false
			continue
		}
		target := strings.ReplaceAll(parts[4], `\040`, " ")
		writable := strings.Contains(","+parts[5]+",", ",rw,")
		if size, scratch := scratchSizes[target]; scratch {
			options := "," + parts[5] + ","
			valid := writable && strings.Contains(options, ",noexec,") && strings.Contains(options, ",nosuid,") && strings.Contains(options, ",nodev,")
			actualSize := int64(0)
			for index, part := range parts {
				if part != "-" || index+3 >= len(parts) {
					continue
				}
				valid = valid && parts[index+1] == "tmpfs"
				for _, option := range strings.Split(parts[index+3], ",") {
					if strings.HasPrefix(option, "size=") {
						actualSize = executionKernelSize(strings.TrimPrefix(option, "size="))
					}
				}
			}
			mountsMatch = mountsMatch && valid && actualSize == size
			observedScratch[target] = true
		}
		if target == "/" {
			rootObserved = true
			mountsMatch = mountsMatch && (!e.ReadOnly || !writable)
			continue
		}
		if !writable {
			continue
		}
		if allowedData[target] {
			continue
		}
		if target == "/proc" || strings.HasPrefix(target, "/proc/") || target == "/dev" || strings.HasPrefix(target, "/dev/") || target == "/etc/hosts" || target == "/etc/hostname" || target == "/etc/resolv.conf" {
			continue
		}
		mountsMatch = false
	}
	if err := check("writable_inventory", "declared bounded nonexecutable scratch, data, and engine virtual/hostname/DNS mounts", "kernel mount inventory comparison", rootObserved && mountsMatch && len(observedScratch) == len(scratchSizes)); err != nil {
		return controls, err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(status, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	// NSpid is kernel status available without ptrace permission against a
	// non-root workload. Paired with private engine PID mode, it establishes
	// that the daemon's init process is PID one in a nested namespace.
	pidLevels := strings.Fields(fields["NSpid"])
	if err := check("pid_namespace", "private", "kernel nested PID identity comparison", len(pidLevels) >= 2 && pidLevels[len(pidLevels)-1] == "1"); err != nil {
		return controls, err
	}
	if err := check("seccomp", "filter enabled", "kernel mode comparison", fields["Seccomp"] == "2"); err != nil {
		return controls, err
	}
	if e.NoNewPrivileges {
		if err := check("no_new_privileges", "true", "kernel flag comparison", fields["NoNewPrivs"] == "1"); err != nil {
			return controls, err
		}
	}
	mask := uint64(0)
	capBits := map[string]uint{"CHOWN": 0, "DAC_OVERRIDE": 1, "FOWNER": 3, "FSETID": 4, "KILL": 5, "SETGID": 6, "SETUID": 7, "SETPCAP": 8, "NET_BIND_SERVICE": 10, "NET_RAW": 13, "SYS_CHROOT": 18, "MKNOD": 27, "AUDIT_WRITE": 29, "SETFCAP": 31}
	for _, capability := range e.DropCapabilities {
		if capability == "ALL" {
			mask = ^uint64(0)
			break
		}
		if bit, ok := capBits[capability]; ok {
			mask |= 1 << bit
		}
	}
	if mask != 0 {
		for _, field := range []string{"CapEff", "CapPrm", "CapInh", "CapBnd", "CapAmb"} {
			bits, parseErr := strconv.ParseUint(fields[field], 16, 64)
			if err := check("capabilities", "declared drops", "kernel capability comparison", parseErr == nil && bits&mask == 0); err != nil {
				return controls, err
			}
		}
	}
	for _, identity := range []struct {
		field, mapping string
		id             uint32
	}{{"Uid", "uid_map", e.UID}, {"Gid", "gid_map", e.GID}} {
		mapping, err := executionRead(ctx, proc, identity.mapping)
		if err != nil {
			return controls, err
		}
		ids := strings.Fields(fields[identity.field])
		match := len(ids) == 4
		for _, value := range ids {
			hostID, parseErr := strconv.ParseUint(value, 10, 32)
			match = match && parseErr == nil && mappedExecutionID(mapping, uint64(identity.id), hostID)
		}
		if err := check(strings.ToLower(identity.field), "nonzero numeric container identity", "kernel namespace mapping comparison", match); err != nil {
			return controls, err
		}
	}
	// Recheck the instance binding after observations to reject an exited or
	// reused PID. No workload-controlled helper participates in admission.
	again, err := executionRead(ctx, proc, "cgroup")
	if err != nil || again != cgroup {
		return controls, fmt.Errorf("execution.runtime: instance changed during observation")
	}
	return controls, nil
}

func mappedExecutionID(mapping string, containerID, hostID uint64) bool {
	for _, line := range strings.Split(mapping, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		inside, e1 := strconv.ParseUint(fields[0], 10, 32)
		outside, e2 := strconv.ParseUint(fields[1], 10, 32)
		length, e3 := strconv.ParseUint(fields[2], 10, 32)
		if e1 == nil && e2 == nil && e3 == nil && containerID >= inside && containerID-inside < length && outside+containerID-inside == hostID {
			return true
		}
	}
	return false
}

func executionKernelSize(value string) int64 {
	multiplier := int64(1)
	for suffix, scale := range map[string]int64{"k": 1024, "m": 1024 * 1024, "g": 1024 * 1024 * 1024} {
		if strings.HasSuffix(value, suffix) {
			value, multiplier = strings.TrimSuffix(value, suffix), scale
			break
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 || number > (1<<50)/multiplier {
		return -1
	}
	return number * multiplier
}

func executionRead(ctx context.Context, root *os.Root, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := root.Open(name)
	if err != nil {
		return "", fmt.Errorf("execution.runtime: trusted kernel field unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return "", fmt.Errorf("execution.runtime: trusted kernel field unreadable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(data), nil
}
