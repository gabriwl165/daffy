package health

const (
	coreFindingMemoryCapacity      = "memory.capacity"
	coreFindingMemorySwap          = "memory.swap"
	coreFindingCPUUtilization      = "cpu.utilization"
	coreFindingProcessCPU          = "process.cpu"
	coreFindingThermalTemperature  = "thermal.temperature"
	coreFindingThermalThrottle     = "thermal.throttle"
	coreFindingDiskRoot            = "disk.root"
	coreFindingDiskActivity        = "disk.activity"
	coreFindingPressureCPU         = "pressure.cpu"
	coreFindingPressureMemory      = "pressure.memory"
	coreFindingPressureIO          = "pressure.io"
	coreFindingWorkloadBuild       = "workload.build"
	coreFindingSwapActivity        = "swap.activity"
	coreFindingCollectionCancelled = "collection.cancelled"
)

func coreRecommendation(id string, severity Severity) string {
	if id == coreFindingWorkloadBuild {
		return "Reduce concurrent builds or other Java workloads if contention persists."
	}
	if severity == SeverityUnknown {
		return "Check that the relevant Linux procfs/sysfs data is mounted and readable."
	}
	switch id {
	case coreFindingMemoryCapacity:
		return "Review memory-heavy applications and available capacity."
	case coreFindingMemorySwap:
		return "Review swap configuration and workload memory needs."
	case coreFindingCPUUtilization:
		return "Investigate sustained CPU saturation and competing workloads."
	case coreFindingProcessCPU:
		return "Review the listed process CPU consumers."
	case coreFindingThermalTemperature:
		return "Check cooling, airflow, and thermal limits."
	case coreFindingThermalThrottle:
		return "Check cooling and reduce sustained compute load if throttling continues."
	case coreFindingDiskRoot:
		return "Free space or expand the root filesystem."
	case coreFindingDiskActivity:
		return "Correlate I/O activity with application workload."
	case coreFindingPressureCPU:
		return "Investigate CPU contention and runnable workload."
	case coreFindingPressureMemory:
		return "Investigate memory pressure and reclaim activity."
	case coreFindingPressureIO:
		return "Investigate storage latency and competing I/O."
	default:
		return ""
	}
}

func coreConfidence(severity Severity) Confidence {
	switch severity {
	case SeverityUnknown:
		return ConfidenceLow
	case SeverityOK, SeverityWarning:
		return ConfidenceMedium
	case SeverityCritical:
		return ConfidenceHigh
	default:
		return ""
	}
}
