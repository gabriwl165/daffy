package health

import "testing"

func TestRootCapacityPercentageDoesNotOverflow(t *testing.T) {
	f := coreRootCapacity(^uint64(0), 0)
	if f.Severity != SeverityCritical || f.Summary != "Root filesystem used: 100%" {
		t.Fatalf("maximum capacity: %+v", f)
	}
}
