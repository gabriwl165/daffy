package health

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gabriwl165/daffy/internal/distribution"
)

const darwinSysctlCommand distribution.Tool = "sysctl"

type darwinSysctlKey string

const (
	darwinKernelKey     darwinSysctlKey = "kern.osrelease"
	darwinCPUModelKey   darwinSysctlKey = "machdep.cpu.brand_string"
	darwinMemorySizeKey darwinSysctlKey = "hw.memsize"
	darwinDistroID      distribution.ID = "macos"
)

type darwinFindingID string

const (
	darwinFindingMemoryCapacity     darwinFindingID = "memory.capacity"
	darwinFindingCPUUtilization     darwinFindingID = "cpu.utilization"
	darwinFindingProcessCPU         darwinFindingID = "process.cpu"
	darwinFindingMemorySwap         darwinFindingID = "memory.swap"
	darwinFindingThermalTemperature darwinFindingID = "thermal.temperature"
	darwinFindingThermalThrottle    darwinFindingID = "thermal.throttle"
	darwinFindingDiskActivity       darwinFindingID = "disk.activity"
	darwinFindingPressureCPU        darwinFindingID = "pressure.cpu"
	darwinFindingPressureMemory     darwinFindingID = "pressure.memory"
	darwinFindingPressureIO         darwinFindingID = "pressure.io"
	darwinFindingSwapActivity       darwinFindingID = "swap.activity"
)

func darwinValue(result integrationResult) string {
	if result.err != nil || result.truncated || len(result.stderr) != 0 || len(result.stdout) > 4096 {
		return ""
	}
	return strings.TrimSpace(string(result.stdout))
}

func darwinReport(kernel, cpu, arch string, mem integrationResult) Report {
	r := Report{SchemaVersion: "1", System: SystemInfo{OS: "macOS", Kernel: kernel, DistroID: string(darwinDistroID), DistroAdapter: "unsupported"}, Findings: []Finding{}}
	if cpu == "" {
		cpu = arch
	}
	r.System.CPUModel = cpu
	value := darwinValue(mem)
	bytes, err := strconv.ParseUint(value, 10, 64)
	if value != "" && err == nil && bytes > 0 {
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemoryCapacity), Area: AreaMemory, Severity: SeverityInfo, Summary: fmt.Sprintf("Physical memory capacity: %d MiB; available memory is not measured", bytes/(1024*1024)), Evidence: []string{fmt.Sprintf("total_bytes=%d", bytes)}, Confidence: ConfidenceMedium})
	} else {
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemoryCapacity), Area: AreaMemory, Severity: SeverityUnknown, Summary: "Memory capacity unavailable or malformed", Confidence: ConfidenceLow})
	}
	unknowns := []struct {
		id      darwinFindingID
		area    Area
		summary string
	}{
		{darwinFindingCPUUtilization, AreaCPU, "CPU utilization sampling unavailable"},
		{darwinFindingProcessCPU, AreaProcess, "Per-process CPU sampling unavailable"},
		{darwinFindingMemorySwap, AreaMemory, "Swap usage unavailable"},
		{darwinFindingThermalTemperature, AreaThermal, "Temperature sensors unavailable"},
		{darwinFindingThermalThrottle, AreaThermal, "Thermal throttle counters unavailable"},
		{darwinFindingDiskActivity, AreaDisk, "Disk activity counters unavailable"},
		{darwinFindingPressureCPU, AreaPressure, "CPU pressure unavailable"},
		{darwinFindingPressureMemory, AreaPressure, "Memory pressure unavailable"},
		{darwinFindingPressureIO, AreaPressure, "I/O pressure unavailable"},
		{darwinFindingSwapActivity, AreaSwap, "Swap activity unavailable"},
	}
	for _, item := range unknowns {
		r.Findings = append(r.Findings, Finding{ID: string(item.id), Area: item.area, Severity: SeverityUnknown, Summary: item.summary, Confidence: ConfidenceLow})
	}
	return r
}
