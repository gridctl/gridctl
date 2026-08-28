//go:build windows

package state

import (
	"os"

	"golang.org/x/sys/windows"
)

func tryFileLock(file *os.File) (func() error, error) {
	var overlapped windows.Overlapped
	handle := windows.Handle(file.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		return nil, err
	}
	return func() error { return windows.UnlockFileEx(handle, 0, 1, 0, &overlapped) }, nil
}

func processExists(process *os.Process) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return false
	}
	return windows.CloseHandle(handle) == nil
}

func terminateProcess(process *os.Process) error { return process.Kill() }

func killProcess(process *os.Process) error { return process.Kill() }
