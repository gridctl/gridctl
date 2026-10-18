//go:build darwin

package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func newHostSupervisor() Supervisor {
	return &launchdSupervisor{}
}

type launchdSupervisor struct{}

func (s *launchdSupervisor) domain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func (s *launchdSupervisor) target(spec Spec) string {
	return s.domain() + "/" + spec.Label
}

func (s *launchdSupervisor) Enable(ctx context.Context, spec Spec) error {
	// bootstrap fails with an I/O error when the label is already loaded.
	// bootout first and ignore a not-loaded agent.
	if out, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "bootout", s.target(spec)); err != nil {
		if !launchctlNotLoaded(string(out) + err.Error()) {
			// A loaded agent that refuses bootout still has to be reported
			// by bootstrap, which is the step that matters.
			_ = out
		}
	}
	if _, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "bootstrap", s.domain(), spec.UnitPath); err != nil {
		return err
	}
	_, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "enable", s.target(spec))
	return err
}

func (s *launchdSupervisor) Disable(ctx context.Context, spec Spec) error {
	out, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "bootout", s.target(spec))
	if err != nil {
		if isInfra(err) {
			return err
		}
		if launchctlNotLoaded(string(out) + err.Error()) {
			return nil
		}
	}
	return err
}

func (s *launchdSupervisor) Stop(ctx context.Context, spec Spec) error {
	out, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "bootout", s.target(spec))
	if err != nil {
		if isInfra(err) {
			return err
		}
		if launchctlNotLoaded(string(out) + err.Error()) {
			return fmt.Errorf("%w: %s", ErrUnitNotLoaded, err.Error())
		}
	}
	return err
}

func (s *launchdSupervisor) Restart(ctx context.Context, spec Spec) error {
	// bootout plus bootstrap is how a running agent picks up a new plist.
	return s.Enable(ctx, spec)
}

func (s *launchdSupervisor) Query(ctx context.Context, spec Spec) (RuntimeState, error) {
	out, err := runSupervisor(ctx, supervisorTimeout, "launchctl", "print", s.target(spec))
	if err != nil {
		if isInfra(err) {
			return RuntimeState{}, err
		}
		if launchctlNotLoaded(string(out) + err.Error()) {
			return RuntimeState{State: "not-loaded"}, nil
		}
		return RuntimeState{}, err
	}
	return ParseLaunchctlPrint(string(out)), nil
}

func (s *launchdSupervisor) Logs(ctx context.Context, spec Spec, opts LogOptions) error {
	// macOS has no user journal for this unit. The caller tails spec.LogPath.
	return fmt.Errorf("launchd logs are the plist log file %s", spec.LogPath)
}

func (s *launchdSupervisor) Linger(ctx context.Context) (string, error) {
	return "unknown", nil
}

func (s *launchdSupervisor) DaemonReload(ctx context.Context) error {
	return nil
}

// launchctlNotLoaded reports a bootout or print failure that means the
// label is not in the domain. Those are not install or uninstall failures.
func launchctlNotLoaded(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no such process") ||
		strings.Contains(lower, "could not find service") ||
		strings.Contains(lower, "not found")
}

func manualEnableCommand(spec Spec) string {
	return fmt.Sprintf("launchctl bootstrap %s %s && launchctl enable %s",
		"gui/"+strconv.Itoa(os.Getuid()), spec.UnitPath, "gui/"+strconv.Itoa(os.Getuid())+"/"+spec.Label)
}
