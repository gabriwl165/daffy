package health

import "testing"

func TestCorePolicyRecommendations(t *testing.T) {
	tests := []struct {
		id       string
		severity Severity
		want     string
	}{
		{coreFindingMemoryCapacity, SeverityWarning, "Review memory-heavy applications and available capacity."},
		{coreFindingMemorySwap, SeverityWarning, "Review swap configuration and workload memory needs."},
		{coreFindingCPUUtilization, SeverityWarning, "Investigate sustained CPU saturation and competing workloads."},
		{coreFindingProcessCPU, SeverityWarning, "Review the listed process CPU consumers."},
		{coreFindingThermalTemperature, SeverityWarning, "Check cooling, airflow, and thermal limits."},
		{coreFindingThermalThrottle, SeverityWarning, "Check cooling and reduce sustained compute load if throttling continues."},
		{coreFindingDiskRoot, SeverityWarning, "Free space or expand the root filesystem."},
		{coreFindingDiskActivity, SeverityWarning, "Correlate I/O activity with application workload."},
		{coreFindingPressureCPU, SeverityWarning, "Investigate CPU contention and runnable workload."},
		{coreFindingPressureMemory, SeverityWarning, "Investigate memory pressure and reclaim activity."},
		{coreFindingPressureIO, SeverityWarning, "Investigate storage latency and competing I/O."},
		{coreFindingWorkloadBuild, SeverityUnknown, "Reduce concurrent builds or other Java workloads if contention persists."},
		{"other", SeverityUnknown, "Check that the relevant Linux procfs/sysfs data is mounted and readable."},
		{"other", SeverityInfo, ""},
	}
	for _, tt := range tests {
		if got := coreRecommendation(tt.id, tt.severity); got != tt.want {
			t.Errorf("coreRecommendation(%q,%q)=%q, want %q", tt.id, tt.severity, got, tt.want)
		}
	}
}

func TestCorePolicyConfidence(t *testing.T) {
	for _, tt := range []struct {
		severity Severity
		want     Confidence
	}{{SeverityUnknown, ConfidenceLow}, {SeverityOK, ConfidenceMedium}, {SeverityWarning, ConfidenceMedium}, {SeverityCritical, ConfidenceHigh}, {SeverityInfo, ""}, {Severity("unexpected"), ""}} {
		if got := coreConfidence(tt.severity); got != tt.want {
			t.Errorf("coreConfidence(%q)=%q, want %q", tt.severity, got, tt.want)
		}
	}
}
