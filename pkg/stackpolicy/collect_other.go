//go:build !unix && !windows

package stackpolicy

import "os"

const openNoFollow = 0

type fileID struct {
	name string
}

func identOf(fi os.FileInfo) (fileID, bool) {
	if fi == nil {
		return fileID{}, false
	}
	return fileID{}, false
}

func identFromFile(f *os.File) (fileID, bool) {
	_ = f
	return fileID{}, false
}
