//go:build windows

package varrun

import (
	"errors"
	"os"
	"os/exec"
)

func setupProcess(_ *exec.Cmd, _ bool)                         {}
func processSignals() []os.Signal                              { return []os.Signal{os.Interrupt} }
func signalProcess(p *os.Process, sig os.Signal, _ bool) error { return p.Signal(sig) }
func terminateProcess(p *os.Process, _ bool) error             { return p.Kill() }
func decodeExit(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 0, false
	}
	return exit.ExitCode(), true
}
