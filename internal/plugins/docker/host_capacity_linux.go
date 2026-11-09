//go:build linux

package docker

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gabriwl165/daffy/internal/plugins"
)

func verifyFilesystem(ctx context.Context, root, socketPath string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
	return verifyFilesystemAt(ctx, root, socketPath, "/proc", syscall.Statfs)
}

func verifyFilesystemAt(ctx context.Context, root, socketPath, procRoot string, statFS func(string, *syscall.Statfs_t) error) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
	var result filesystemCapacity
	if ctx.Err() != nil {
		return result, plugins.DockerHostReasonUnverified
	}
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
	data, err := os.ReadFile(filepath.Join(procRoot, "net/unix"))
	if err != nil {
		return result, plugins.DockerHostReasonUnverified
	}
	var inode string
	for _, line := range strings.Split(string(data), "\n") {
		if ctx.Err() != nil {
			return result, plugins.DockerHostReasonUnverified
		}
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[7] == socketPath {
			inode = fields[6]
			break
		}
	}
	if inode == "" {
		return result, plugins.DockerHostReasonUnverified
	}
	currentNS, err := os.Readlink(filepath.Join(procRoot, "self/ns/mnt"))
	if err != nil {
		return result, plugins.DockerHostReasonUnverified
	}
	verified := false
	processes, _ := filepath.Glob(filepath.Join(procRoot, "[0-9]*"))
	for _, process := range processes {
		if ctx.Err() != nil {
			return result, plugins.DockerHostReasonUnverified
		}
		comm, _ := os.ReadFile(filepath.Join(process, "comm"))
		if strings.TrimSpace(string(comm)) != "dockerd" {
			continue
		}
		ns, e := os.Readlink(filepath.Join(process, "ns/mnt"))
		if e != nil || ns != currentNS {
			continue
		}
		selfRoot, e1 := os.Stat(filepath.Join(procRoot, "self/root"))
		daemonRoot, e2 := os.Stat(filepath.Join(process, "root"))
		if e1 != nil || e2 != nil {
			continue
		}
		selfStat, ok1 := selfRoot.Sys().(*syscall.Stat_t)
		daemonStat, ok2 := daemonRoot.Sys().(*syscall.Stat_t)
		if !ok1 || !ok2 || selfStat.Dev != daemonStat.Dev || selfStat.Ino != daemonStat.Ino {
			continue
		}
		fds, _ := filepath.Glob(filepath.Join(process, "fd/*"))
		for _, fd := range fds {
			if ctx.Err() != nil {
				return result, plugins.DockerHostReasonUnverified
			}
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
	if err = statFS(root, &stat); err != nil {
		return result, plugins.DockerHostReasonFilesystem
	}
	if stat.Bsize <= 0 || stat.Blocks < 0 || stat.Bfree < 0 || stat.Bavail < 0 {
		return result, plugins.DockerHostReasonFilesystem
	}
	block := uint64(stat.Bsize)
	if uint64(stat.Blocks) > math.MaxUint64/block || uint64(stat.Bfree) > math.MaxUint64/block || uint64(stat.Bavail) > math.MaxUint64/block {
		return result, plugins.DockerHostReasonFilesystem
	}
	result.total = uint64(stat.Blocks) * block
	result.free = uint64(stat.Bfree) * block
	result.available = uint64(stat.Bavail) * block
	result.inodesTotal = stat.Files
	result.inodesFree = stat.Ffree
	return result, ""
}
