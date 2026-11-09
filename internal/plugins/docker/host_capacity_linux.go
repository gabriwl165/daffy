//go:build linux

package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gabriwl165/daffy/internal/plugins"
)

func verifyFilesystem(root, socketPath string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
	var result filesystemCapacity
	if !filepath.IsAbs(root) || !filepath.IsAbs(socketPath) {
		return result, plugins.DockerHostReasonUnverified
	}
	info, err := os.Stat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return result, plugins.DockerHostReasonUnverified
	}
	if resolved, e := filepath.EvalSymlinks(socketPath); e == nil {
		socketPath = resolved
	}
	data, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return result, plugins.DockerHostReasonUnverified
	}
	var inode string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[7] == socketPath {
			inode = fields[6]
			break
		}
	}
	if inode == "" {
		return result, plugins.DockerHostReasonUnverified
	}
	currentNS, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		return result, plugins.DockerHostReasonUnverified
	}
	verified := false
	processes, _ := filepath.Glob("/proc/[0-9]*")
	for _, process := range processes {
		comm, _ := os.ReadFile(filepath.Join(process, "comm"))
		if strings.TrimSpace(string(comm)) != "dockerd" {
			continue
		}
		ns, e := os.Readlink(filepath.Join(process, "ns/mnt"))
		if e != nil || ns != currentNS {
			continue
		}
		fds, _ := filepath.Glob(filepath.Join(process, "fd/*"))
		for _, fd := range fds {
			link, e := os.Readlink(fd)
			if e == nil && link == fmt.Sprintf("socket:[%s]", inode) {
				verified = true
				break
			}
		}
		if verified {
			break
		}
	}
	if !verified {
		return result, plugins.DockerHostReasonUnverified
	}
	var stat syscall.Statfs_t
	if err = syscall.Statfs(root, &stat); err != nil {
		return result, plugins.DockerHostReasonFilesystem
	}
	block := uint64(stat.Bsize)
	result.total = uint64(stat.Blocks) * block
	result.free = uint64(stat.Bfree) * block
	result.available = uint64(stat.Bavail) * block
	result.inodesTotal = stat.Files
	result.inodesFree = stat.Ffree
	return result, ""
}
