package service

import "os/exec"

func execLookPath(name string) (string, error) {
	return exec.LookPath(name)
}
