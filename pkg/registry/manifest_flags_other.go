//go:build !unix

package registry

import "os"

const manifestReadFlags = os.O_RDONLY
