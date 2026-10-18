package service

import (
	"context"
	"io"
	"time"
)

// supervisorTimeout bounds every supervisor invocation.
const supervisorTimeout = 30 * time.Second

// RuntimeState is the parsed supervisor view of one unit.
type RuntimeState struct {
	// State is running, failed, inactive, not-loaded, or unknown.
	State   string
	Enabled bool
	PID     int
	Detail  string
}

// LogOptions configures a log follow.
type LogOptions struct {
	Lines  int
	Follow bool
	Stdout io.Writer
	Stderr io.Writer
}

// Supervisor controls one user-scoped service manager. Implementations
// shell out to systemctl, loginctl, journalctl, or launchctl. Tests inject
// a fake.
type Supervisor interface {
	Enable(ctx context.Context, spec Spec) error
	Disable(ctx context.Context, spec Spec) error
	// Stop stops the unit without disabling it. destroy uses this so
	// Restart=on-failure does not see a killed process.
	Stop(ctx context.Context, spec Spec) error
	Query(ctx context.Context, spec Spec) (RuntimeState, error)
	Logs(ctx context.Context, spec Spec, opts LogOptions) error
	// Linger reports "yes", "no", or "unknown". Non-Linux supervisors
	// return "unknown" and a nil error.
	Linger(ctx context.Context) (string, error)
	// DaemonReload refreshes the user manager after a unit file is
	// removed. Non-Linux supervisors return nil.
	DaemonReload(ctx context.Context) error
	// Restart reloads unit text and restarts a unit the supervisor
	// already owns. It is not KillDaemon: a five-second SIGKILL would
	// trip Restart=on-failure.
	Restart(ctx context.Context, spec Spec) error
}

// NewSupervisor returns the host supervisor. A missing binary is reported
// when a method runs, not here, so status and reset can still read the
// lockfile on a machine without systemctl.
func NewSupervisor() Supervisor {
	return newHostSupervisor()
}
