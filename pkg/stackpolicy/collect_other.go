//go:build !unix

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
