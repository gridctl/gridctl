//go:build !windows

package controller

import (
	"os/exec"
	"syscall"
)

func configureDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
