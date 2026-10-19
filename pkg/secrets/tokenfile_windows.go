//go:build windows

package secrets

import "os"

func checkTokenOwnerMode(info os.FileInfo) error {
	return nil
}

func tokenOpenFlag() int {
	return 0
}
