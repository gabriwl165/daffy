package health

import (
	"fmt"
	"math/bits"
)

func coreRootCapacity(total, avail uint64) Finding {
	if avail > total {
		return Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity inconsistent"}
	}
	used := uint64(0)
	if total > 0 {
		hi, lo := bits.Mul64(total-avail, 100)
		used, _ = bits.Div64(hi, lo, total)
	}
	severity := SeverityOK
	if used >= 95 {
		severity = SeverityCritical
	} else if used >= 85 {
		severity = SeverityWarning
	}
	return Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: severity, Summary: fmt.Sprintf("Root filesystem used: %d%%", used), Evidence: []string{fmt.Sprintf("used_percent=%d", used), fmt.Sprintf("available_bytes=%d", avail)}}
}
