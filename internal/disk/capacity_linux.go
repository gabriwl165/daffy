//go:build linux

package disk

import (
	"syscall"
)

func capacity(path string) (MountCapacity, error) {
	var s syscall.Statfs_t
	if e := syscall.Statfs(path, &s); e != nil {
		return MountCapacity{}, e
	}
	return MountCapacity{Path: path, TotalBytes: s.Blocks * uint64(s.Bsize), AvailableBytes: s.Bavail * uint64(s.Bsize), TotalInodes: s.Files, FreeInodes: s.Ffree}, nil
}
