//go:build unix

package registry

import (
	"os"
	"syscall"
)

// Nonblocking refuses a regular-file-to-FIFO replacement without hanging.
const manifestReadFlags = os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
