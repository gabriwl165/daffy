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
func metadata(path string) (int64, int64, uint64, uint64, error) {
	var s syscall.Stat_t
	if e := syscall.Lstat(path, &s); e != nil {
		return 0, 0, 0, 0, e
	}
	return s.Size, s.Blocks * 512, uint64(s.Dev), uint64(s.Ino), nil
}
