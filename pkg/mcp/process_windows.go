//go:build windows

package mcp

import "os/exec"

func configureProcessGroup(_ *exec.Cmd, _ bool) {}

func signalProcess(cmd *exec.Cmd, _, _ bool) error {
	return cmd.Process.Kill()
}
