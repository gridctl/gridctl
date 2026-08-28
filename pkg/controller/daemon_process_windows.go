//go:build windows

package controller

import "os/exec"

func configureDaemonProcess(_ *exec.Cmd) {}
