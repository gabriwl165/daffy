//go:build linux

package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

const oPath = 0x200000
const linuxBlockBytes = 512

type linuxDirectory struct{ file *os.File }

func validChild(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func openAt(parent *os.File, name string, flags int) (int, error) {
	fd := int(parent.Fd())
	child, err := syscall.Openat(fd, name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	runtime.KeepAlive(parent)
	return child, err
}

func openScanRoot(path string) (Directory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("scan root must be a clean absolute path")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "/")
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := openAt(current, part, syscall.O_RDONLY|syscall.O_DIRECTORY)
		if e != nil {
			current.Close()
			return nil, e
		}
		following := os.NewFile(uintptr(next), part)
		current.Close()
		current = following
	}
	return &linuxDirectory{file: current}, nil
}

func (d *linuxDirectory) ReadDir(n int) ([]os.DirEntry, error) { return d.file.ReadDir(n) }
func (d *linuxDirectory) Stat() (os.FileInfo, error)           { return d.file.Stat() }
func (d *linuxDirectory) Close() error                         { return d.file.Close() }

func (d *linuxDirectory) ChildMetadata(name string) (EntryMetadata, error) {
	if !validChild(name) {
		return EntryMetadata{}, syscall.EINVAL
	}
	fd, err := openAt(d.file, name, oPath)
	if err != nil {
		return EntryMetadata{}, err
	}
	defer syscall.Close(fd)
	var st syscall.Stat_t
	if err = syscall.Fstat(fd, &st); err != nil {
		return EntryMetadata{}, err
	}
	return EntryMetadata{ApparentBytes: st.Size, AllocatedBytes: st.Blocks * linuxBlockBytes,
		Identity: FileIdentity{Device: uint64(st.Dev), Inode: st.Ino}, Links: uint64(st.Nlink), Mode: modeFromStat(st)}, nil
}

func entryMetadata(info os.FileInfo) (EntryMetadata, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return EntryMetadata{}, fmt.Errorf("file metadata does not contain Linux stat information")
	}
	return EntryMetadata{
		ApparentBytes: info.Size(), AllocatedBytes: st.Blocks * linuxBlockBytes,
		Identity: FileIdentity{Device: uint64(st.Dev), Inode: st.Ino},
		Links:    uint64(st.Nlink), Mode: info.Mode(),
	}, nil
}

func modeFromStat(st syscall.Stat_t) os.FileMode {
	mode := os.FileMode(st.Mode & 0777)
	switch st.Mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		mode |= os.ModeDir
	case syscall.S_IFLNK:
		mode |= os.ModeSymlink
	case syscall.S_IFIFO:
		mode |= os.ModeNamedPipe
	case syscall.S_IFSOCK:
		mode |= os.ModeSocket
	case syscall.S_IFCHR:
		mode |= os.ModeDevice | os.ModeCharDevice
	case syscall.S_IFBLK:
		mode |= os.ModeDevice
	}
	return mode
}

func (d *linuxDirectory) OpenChild(name string) (Directory, error) {
	if !validChild(name) {
		return nil, syscall.EINVAL
	}
	fd, err := openAt(d.file, name, syscall.O_RDONLY|syscall.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	return &linuxDirectory{file: os.NewFile(uintptr(fd), name)}, nil
}
