//go:build !windows

package varrun

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func setupProcess(cmd *exec.Cmd, group bool) {
	if group {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}
func processSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
}
func signalProcess(p *os.Process, sig os.Signal, group bool) error {
	if group {
		if s, ok := sig.(syscall.Signal); ok {
			if err := syscall.Kill(-p.Pid, s); errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			} else {
				return err
			}
		}
	}
	return p.Signal(sig)
}
func terminateProcess(p *os.Process, group bool) error {
	return signalProcess(p, syscall.SIGTERM, group)
}
func decodeExit(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 0, false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return exit.ExitCode(), true
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	return status.ExitStatus(), true
}
