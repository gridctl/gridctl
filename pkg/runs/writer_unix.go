//go:build unix

package runs

import (
	"os"
	"syscall"
)

func openAppendFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, filePerm)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(filePerm); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
