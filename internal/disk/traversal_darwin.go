//go:build darwin

package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const darwinBlockBytes int64 = 512

type darwinDirectory struct{ file *os.File }

func validDarwinChild(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func openDarwinAt(parent *os.File, name string, flags int) (int, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	runtime.KeepAlive(parent)
	return fd, err
}

func openScanRoot(path string) (Directory, error) {
	return openScanRootCanonical(path, false)
}

func openScanRootCanonical(path string, allowFirmlink bool) (Directory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("scan root must be a clean absolute path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "/")
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := openDarwinAt(current, part, unix.O_RDONLY|unix.O_DIRECTORY)
		if e != nil {
			_ = current.Close()
			return nil, e
		}
		following := os.NewFile(uintptr(next), part)
		_ = current.Close()
		current = following
	}
	pathOnDisk, err := descriptorPath(current, unix.F_GETPATH)
	if err != nil || pathOnDisk != path {
		_ = current.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("scan root spelling is not canonical (resolved to %q)", pathOnDisk)
	}
	if !allowFirmlink {
		physical, e := descriptorPath(current, unix.F_GETPATH_NOFIRMLINK)
		if e != nil {
			_ = current.Close()
			return nil, e
		}
		if restrictedDarwinPhysicalRoot(physical) {
			_ = current.Close()
			return nil, fmt.Errorf("whole-system macOS scanning is unsupported")
		}
	}
	return &darwinDirectory{file: current}, nil
}

func descriptorPath(f *os.File, command int) (string, error) {
	buf := make([]byte, 4096)
	// x/sys/unix exposes only FcntlInt on Darwin; F_GETPATH requires a writable pointer buffer.
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, f.Fd(), uintptr(command), uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(f)
	runtime.KeepAlive(buf)
	if errno != 0 {
		return "", errno
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return "", fmt.Errorf("descriptor path exceeded buffer")
}

func (d *darwinDirectory) ReadDir(n int) ([]os.DirEntry, error) { return d.file.ReadDir(n) }
func (d *darwinDirectory) Stat() (os.FileInfo, error)           { return d.file.Stat() }
func (d *darwinDirectory) Close() error                         { return d.file.Close() }

func darwinMetadata(st *unix.Stat_t) (EntryMetadata, error) {
	if st.Blocks < 0 || st.Blocks > int64(^uint64(0)>>1)/darwinBlockBytes {
		return EntryMetadata{}, fmt.Errorf("allocated block count overflow")
	}
	return EntryMetadata{ApparentBytes: st.Size, AllocatedBytes: st.Blocks * darwinBlockBytes,
		Identity: FileIdentity{Device: uint64(st.Dev), Inode: st.Ino}, Links: uint64(st.Nlink), Mode: darwinMode(st.Mode)}, nil
}

func darwinMode(mode uint16) os.FileMode {
	out := os.FileMode(mode & 0777)
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		out |= os.ModeDir
	case unix.S_IFLNK:
		out |= os.ModeSymlink
	case unix.S_IFIFO:
		out |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		out |= os.ModeSocket
	case unix.S_IFCHR:
		out |= os.ModeDevice | os.ModeCharDevice
	case unix.S_IFBLK:
		out |= os.ModeDevice
	}
	return out
}

func (d *darwinDirectory) ChildMetadata(name string) (EntryMetadata, error) {
	if !validDarwinChild(name) {
		return EntryMetadata{}, unix.EINVAL
	}
	var st unix.Stat_t
	if err := unix.Fstatat(int(d.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		runtime.KeepAlive(d.file)
		return EntryMetadata{}, err
	}
	runtime.KeepAlive(d.file)
	return darwinMetadata(&st)
}

func entryMetadata(info os.FileInfo) (EntryMetadata, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return EntryMetadata{}, fmt.Errorf("file metadata does not contain Darwin stat information")
	}
	blocks := int64(st.Blocks)
	if blocks < 0 || blocks > int64(^uint64(0)>>1)/darwinBlockBytes {
		return EntryMetadata{}, fmt.Errorf("allocated block count overflow")
	}
	return EntryMetadata{ApparentBytes: st.Size, AllocatedBytes: blocks * darwinBlockBytes,
		Identity: FileIdentity{Device: uint64(st.Dev), Inode: st.Ino}, Links: uint64(st.Nlink), Mode: info.Mode()}, nil
}

func (d *darwinDirectory) OpenChild(name string) (Directory, error) {
	if !validDarwinChild(name) {
		return nil, unix.EINVAL
	}
	fd, err := openDarwinAt(d.file, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	return &darwinDirectory{file: os.NewFile(uintptr(fd), name)}, nil
}
