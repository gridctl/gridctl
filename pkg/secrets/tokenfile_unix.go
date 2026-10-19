//go:build !windows

package secrets

import (
	"fmt"
	"os"
	"syscall"
)

func checkTokenOwnerMode(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("token file owner")
	}
	if info.Mode().Perm()&^0o600 != 0 {
		return fmt.Errorf("token file mode")
	}
	return nil
}

func tokenOpenFlag() int {
	return syscall.O_NOFOLLOW
}
