//go:build unix

package stackpolicy

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const openNoFollow = unix.O_NOFOLLOW

type fileID struct {
	dev uint64
	ino uint64
}

func identOf(fi os.FileInfo) (fileID, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || fi == nil {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: st.Ino}, true
}
