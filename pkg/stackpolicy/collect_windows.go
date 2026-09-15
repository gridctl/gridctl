//go:build windows

package stackpolicy

import (
	"os"

	"golang.org/x/sys/windows"
)

const openNoFollow = 0

type fileID struct {
	volume uint32
	index  uint64
}

func identOf(fi os.FileInfo) (fileID, bool) {
	_ = fi
	return fileID{}, false
}

func identFromFile(f *os.File) (fileID, bool) {
	if f == nil {
		return fileID{}, false
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return fileID{}, false
	}
	index := (uint64(info.FileIndexHigh) << 32) | uint64(info.FileIndexLow)
	return fileID{volume: info.VolumeSerialNumber, index: index}, true
}
