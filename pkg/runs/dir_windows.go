//go:build windows

package runs

import "os"

func openDirNoFollow(path string) (*os.File, error) {
	if err := refuseSymlink(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}
	return os.Open(path)
}
