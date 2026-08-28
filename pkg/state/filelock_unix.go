//go:build !windows

package state

import (
	"os"
	"syscall"
)

func tryFileLock(file *os.File) (func() error, error) {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return func() error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }, nil
}

func processExists(process *os.Process) bool {
	return process.Signal(syscall.Signal(0)) == nil
}

func terminateProcess(process *os.Process) error { return process.Signal(syscall.SIGTERM) }

func killProcess(process *os.Process) error { return process.Signal(syscall.SIGKILL) }
