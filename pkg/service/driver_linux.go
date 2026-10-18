//go:build linux

package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func newHostSupervisor() Supervisor {
	return &systemdSupervisor{}
}

type systemdSupervisor struct{}

func (s *systemdSupervisor) Enable(ctx context.Context, spec Spec) error {
	unit := UnitFileName(spec.Name)
	if _, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	_, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "enable", "--now", unit)
	return err
}

func (s *systemdSupervisor) Disable(ctx context.Context, spec Spec) error {
	unit := UnitFileName(spec.Name)
	_, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "disable", "--now", unit)
	if err != nil && unitNotLoaded(err) {
		return nil
	}
	return err
}

func (s *systemdSupervisor) Stop(ctx context.Context, spec Spec) error {
	unit := UnitFileName(spec.Name)
	_, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "stop", unit)
	return err
}

func (s *systemdSupervisor) Query(ctx context.Context, spec Spec) (RuntimeState, error) {
	unit := UnitFileName(spec.Name)
	out, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "show",
		"-p", "ActiveState,SubState,MainPID,UnitFileState,LoadState", unit)
	if err != nil {
		if unitNotLoaded(err) {
			return RuntimeState{State: "not-loaded"}, nil
		}
		return RuntimeState{}, err
	}
	return ParseSystemctlShow(string(out)), nil
}

func (s *systemdSupervisor) Logs(ctx context.Context, spec Spec, opts LogOptions) error {
	unit := UnitFileName(spec.Name)
	args := []string{"--user", "-u", unit}
	if opts.Lines > 0 {
		args = append(args, "-n", strconv.Itoa(opts.Lines))
	}
	if opts.Follow {
		args = append(args, "-f")
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	return runSupervisorInherit(ctx, stdout, stderr, "journalctl", args...)
}

func (s *systemdSupervisor) Linger(ctx context.Context) (string, error) {
	uid := strconv.Itoa(os.Getuid())
	out, err := runSupervisor(ctx, supervisorTimeout, "loginctl", "show-user", uid, "--property=Linger")
	if err != nil {
		return "unknown", nil
	}
	return ParseLinger(string(out)), nil
}

func (s *systemdSupervisor) DaemonReload(ctx context.Context) error {
	_, err := runSupervisor(ctx, supervisorTimeout, "systemctl", "--user", "daemon-reload")
	return err
}

func unitNotLoaded(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not loaded") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such")
}

// manualEnableCommand is the command printed when enable fails.
func manualEnableCommand(spec Spec) string {
	return fmt.Sprintf("systemctl --user daemon-reload && systemctl --user enable --now %s", UnitFileName(spec.Name))
}
