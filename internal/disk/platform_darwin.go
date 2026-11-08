//go:build darwin

package disk

import (
	"context"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type darwinPlatform struct{}

const maxDarwinMountSnapshot = 1 << 16

const (
	darwinSystemPath  Path = "/System"
	darwinVolumesPath Path = "/System/Volumes"
	darwinDataPath    Path = "/System/Volumes/Data"
)

func restrictedDarwinPhysicalRoot(path string) bool {
	if path == string(darwinDataPath) {
		return true
	}
	if within(string(darwinDataPath), path) {
		return false
	}
	return path == string(darwinSystemPath) || within(string(darwinSystemPath), path)
}

func darwinString(v []byte) string {
	n := 0
	for n < len(v) && v[n] != 0 {
		n++
	}
	b := make([]byte, n)
	copy(b, v[:n])
	return string(b)
}

func (darwinPlatform) Mounts() ([]Mount, error) {
	return mountsFromSnapshot(unix.Getfsstat)
}

type darwinSnapshotQuery func([]unix.Statfs_t, int) (int, error)

func mountsFromSnapshot(query darwinSnapshotQuery) ([]Mount, error) {
	count, err := query(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("invalid filesystem snapshot size")
	}
	if count > maxDarwinMountSnapshot {
		return nil, fmt.Errorf("filesystem snapshot exceeds mount limit")
	}
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]unix.Statfs_t, count)
		got, e := query(buf, unix.MNT_NOWAIT)
		if e != nil {
			return nil, e
		}
		if got > len(buf) {
			if got > maxDarwinMountSnapshot {
				return nil, fmt.Errorf("filesystem snapshot exceeds mount limit")
			}
			count = got
			continue
		}
		if got == len(buf) {
			next, e := query(nil, unix.MNT_NOWAIT)
			if e != nil {
				return nil, e
			}
			if next > len(buf) {
				if next > maxDarwinMountSnapshot {
					return nil, fmt.Errorf("filesystem snapshot exceeds mount limit")
				}
				count = next
				continue
			}
		}
		out := make([]Mount, 0, got)
		for _, st := range buf[:got] {
			path := darwinString(st.Mntonname[:])
			fs := darwinString(st.Fstypename[:])
			if path == "" || path[0] != '/' {
				return nil, fmt.Errorf("invalid mount path in filesystem snapshot")
			}
			m := Mount{Path: path, Root: "/", Device: darwinString(st.Mntfromname[:]), Source: darwinString(st.Mntfromname[:]), Filesystem: filesystemForDarwin(fs), Eligible: fs == string(FilesystemAPFS)}
			if !m.Eligible {
				m.SkipReason = SkipUnsupportedFilesystem
				m.Reason = "unsupported filesystem"
			}
			out = append(out, m)
		}
		return out, nil
	}
	return nil, fmt.Errorf("filesystem snapshot kept growing")
}

func filesystemForDarwin(name string) Filesystem {
	if name == string(FilesystemAPFS) {
		return FilesystemAPFS
	}
	return Filesystem(name)
}

func (darwinPlatform) Capacity(path string) (MountCapacity, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return MountCapacity{}, err
	}
	bsize := uint64(st.Bsize)
	if st.Bsize <= 0 || st.Bavail < 0 || st.Blocks < 0 || st.Bavail > st.Blocks {
		return MountCapacity{}, fmt.Errorf("invalid filesystem capacity values")
	}
	if st.Blocks != 0 && bsize > ^uint64(0)/uint64(st.Blocks) {
		return MountCapacity{}, fmt.Errorf("filesystem capacity overflow")
	}
	if st.Bavail != 0 && bsize > ^uint64(0)/uint64(st.Bavail) {
		return MountCapacity{}, fmt.Errorf("filesystem available capacity overflow")
	}
	return MountCapacity{Path: path, TotalBytes: uint64(st.Blocks) * bsize, AvailableBytes: uint64(st.Bavail) * bsize, TotalInodes: uint64(st.Files), FreeInodes: uint64(st.Ffree)}, nil
}

func newScanner() Scanner { return &darwinScanner{} }

type darwinScanner struct{}

type darwinScopedPlatform struct{ root string }

func (p darwinScopedPlatform) Mounts() ([]Mount, error) {
	mounts, err := (darwinPlatform{}).Mounts()
	if err != nil || p.root == "" {
		return mounts, err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(p.root, &st); err != nil {
		return nil, err
	}
	if darwinString(st.Mntonname[:]) == p.root {
		return mounts, nil
	}
	mounts = append(mounts, scopedDarwinMount(p.root, st))
	return mounts, nil
}

func scopedDarwinMount(root string, st unix.Statfs_t) Mount {
	device := darwinString(st.Mntfromname[:])
	fs := filesystemForDarwin(darwinString(st.Fstypename[:]))
	m := Mount{Path: root, Root: "/", Device: device, Source: device, Filesystem: fs, Eligible: fs == FilesystemAPFS}
	if !m.Eligible {
		m.SkipReason = SkipUnsupportedFilesystem
		m.Reason = "unsupported filesystem"
	}
	return m
}

func (p darwinScopedPlatform) Capacity(path string) (MountCapacity, error) {
	if p.root != "" && path == p.root {
		return (darwinPlatform{}).Capacity(p.root)
	}
	return (darwinPlatform{}).Capacity(path)
}

func (s *darwinScanner) Scan(ctx context.Context, c Config) (Report, error) {
	selected := c.Path
	if selected != "" {
		if !filepath.IsAbs(selected) {
			absolute, err := filepath.Abs(selected)
			if err != nil {
				return Report{}, err
			}
			selected = absolute
		}
		selected = filepath.Clean(selected)
	}
	restricted := selected == "" || selected == "/"
	for _, path := range []Path{darwinSystemPath, darwinVolumesPath, darwinDataPath} {
		if selected == string(path) || within(string(path), selected) {
			restricted = true
		}
	}
	if !restricted {
		root, err := openScanRootCanonical(selected, true)
		if err != nil {
			return Report{Root: selected, Coverage: Coverage{Complete: false}}, err
		}
		physical, err := descriptorPath(root.(*darwinDirectory).file, unix.F_GETPATH_NOFIRMLINK)
		closeErr := root.Close()
		if err != nil {
			return Report{Root: selected, Coverage: Coverage{Complete: false}}, err
		}
		if closeErr != nil {
			return Report{Root: selected, Coverage: Coverage{Complete: false}}, closeErr
		}
		restricted = restrictedDarwinPhysicalRoot(physical)
	}
	if restricted {
		return Report{Root: selected, Coverage: Coverage{Complete: false}}, fmt.Errorf("%w: whole-system macOS scanning is unsupported; select an explicit directory", ErrUnsupportedPlatform)
	}
	c.Path = selected
	scan := &scanner{platform: darwinScopedPlatform{root: selected}}
	return scan.Scan(ctx, c)
}
