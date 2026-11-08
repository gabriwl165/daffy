//go:build linux || darwin

package health

import (
	"math"
	"syscall"
)

func coreRootFilesystemFinding() Finding {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity unavailable"}
	}
	if st.Bsize <= 0 || st.Blocks > math.MaxUint64/uint64(st.Bsize) || st.Bavail > math.MaxUint64/uint64(st.Bsize) {
		return Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity inconsistent"}
	}
	total, avail := st.Blocks*uint64(st.Bsize), st.Bavail*uint64(st.Bsize)
	return coreRootCapacity(total, avail)
}
