//go:build !unix

package vault

import "os"

func fileOwnerUID(os.FileInfo) (uint32, bool) {
	return 0, false
}

func passphraseCurrentUID() uint32 {
	return 0
}
