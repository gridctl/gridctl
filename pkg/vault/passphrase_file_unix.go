//go:build unix

package vault

import (
	"os"
	"syscall"
)

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, false
	}
	return st.Uid, true
}

func passphraseCurrentUID() uint32 {
	return uint32(os.Getuid())
}
