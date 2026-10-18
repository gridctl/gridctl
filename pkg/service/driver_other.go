//go:build !linux && !darwin

package service

import (
	"context"
	"fmt"
)

func newHostSupervisor() Supervisor {
	return unsupportedSupervisor{}
}

type unsupportedSupervisor struct{}

func (unsupportedSupervisor) Enable(context.Context, Spec) error {
	return unsupportedErr()
}

func (unsupportedSupervisor) Disable(context.Context, Spec) error {
	return unsupportedErr()
}

func (unsupportedSupervisor) Stop(context.Context, Spec) error {
	return unsupportedErr()
}

func (unsupportedSupervisor) Query(context.Context, Spec) (RuntimeState, error) {
	return RuntimeState{}, unsupportedErr()
}

func (unsupportedSupervisor) Logs(context.Context, Spec, LogOptions) error {
	return unsupportedErr()
}

func (unsupportedSupervisor) Linger(context.Context) (string, error) {
	return "unknown", unsupportedErr()
}

func (unsupportedSupervisor) DaemonReload(context.Context) error {
	return unsupportedErr()
}

func (unsupportedSupervisor) Restart(context.Context, Spec) error {
	return unsupportedErr()
}

func unsupportedErr() error {
	return fmt.Errorf("gridctl service is not supported on this operating system")
}

func manualEnableCommand(spec Spec) string {
	return "gridctl service is not supported on this operating system"
}
