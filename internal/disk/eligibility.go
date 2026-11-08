package disk

import (
	"path/filepath"
	"strings"
)

func within(parent, child string) bool {
	return parent == child || parent == "/" || strings.HasPrefix(child, parent+string(filepath.Separator))
}

func eligible(m Mount) bool {
	if !m.Eligible {
		return false
	}
	switch m.Filesystem {
	case FilesystemExt2, FilesystemExt3, FilesystemExt4, FilesystemXFS, FilesystemBtrfs, FilesystemF2FS, FilesystemVFAT, FilesystemExFAT, FilesystemNTFS, FilesystemNTFS3, FilesystemFuseblk, FilesystemZFS, FilesystemBcacheFS, FilesystemOverlay, FilesystemISO9660, FilesystemEROFS, FilesystemSquashFS, FilesystemAPFS:
	case FilesystemTmpfs, FilesystemRamfs, FilesystemProc, FilesystemSysfs, FilesystemDevpts, FilesystemDevtmpfs, FilesystemCgroup, FilesystemCgroup2, FilesystemSecurityFS, FilesystemDebugFS, FilesystemTraceFS, FilesystemConfigFS, FilesystemPstore, FilesystemMqueue, FilesystemFusectl, FilesystemRPCPipeFS, FilesystemNFS, FilesystemNFS4, FilesystemCIFS, FilesystemSMB3, Filesystem9P, FilesystemAutofs, FilesystemFuse:
		return false
	default:
		return false
	}
	if m.Filesystem == FilesystemOverlay && m.Path != "/" {
		return false
	}
	return !removablePath(m.Path)
}

func removablePath(p string) bool { return within("/media", p) || within("/run/media", p) }
