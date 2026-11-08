package health

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/distribution"
	macos "github.com/gabriwl165/daffy/internal/distribution/macos"
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
	darwinFindingMemoryFree         darwinFindingID = "memory.free"
	darwinFindingMemorySpeculative  darwinFindingID = "memory.speculative"
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

type darwinSamples struct {
	CPU               *macos.CPUUsage
	CPUSampleInterval time.Duration
	Memory            *macos.MemoryUsage
	MemoryErr         error
	SwapErr           error
}

func darwinReport(kernel, cpu, arch string, mem integrationResult, samples darwinSamples) Report {
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
		{darwinFindingProcessCPU, AreaProcess, "Per-process CPU sampling unavailable"},
		{darwinFindingThermalTemperature, AreaThermal, "Temperature sensors unavailable"},
		{darwinFindingThermalThrottle, AreaThermal, "Thermal throttle counters unavailable"},
		{darwinFindingDiskActivity, AreaDisk, "Disk activity counters unavailable"},
		{darwinFindingPressureCPU, AreaPressure, "CPU pressure unavailable"},
		{darwinFindingPressureMemory, AreaPressure, "Memory pressure unavailable"},
		{darwinFindingPressureIO, AreaPressure, "I/O pressure unavailable"},
		{darwinFindingSwapActivity, AreaSwap, "Swap activity unavailable"},
	}
	if samples.Memory != nil && samples.MemoryErr == nil {
		m := samples.Memory
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemoryFree), Area: AreaMemory, Severity: SeverityInfo, Summary: fmt.Sprintf("Immediately free memory: %d MiB; excludes speculative pages and is not an available-memory estimate", m.ImmediatelyFreeBytes/(1024*1024)), Evidence: []string{fmt.Sprintf("immediately_free_bytes=%d", m.ImmediatelyFreeBytes)}, Confidence: ConfidenceMedium})
		if m.SpeculativePagesPresent {
			r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemorySpeculative), Area: AreaMemory, Severity: SeverityInfo, Summary: fmt.Sprintf("Speculative memory pages: %d MiB; reported separately, not included in immediately free memory", m.SpeculativeBytes/(1024*1024)), Evidence: []string{fmt.Sprintf("speculative_bytes=%d", m.SpeculativeBytes)}, Confidence: ConfidenceMedium})
		}
	} else {
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemoryFree), Area: AreaMemory, Severity: SeverityUnknown, Summary: "Immediately free memory unavailable", Confidence: ConfidenceLow})
	}
	if samples.Memory != nil && samples.SwapErr == nil {
		m := samples.Memory
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemorySwap), Area: AreaMemory, Severity: SeverityInfo, Summary: fmt.Sprintf("Swap usage: %d MiB used of %d MiB", m.SwapUsedBytes/(1024*1024), m.SwapTotalBytes/(1024*1024)), Evidence: []string{fmt.Sprintf("total_bytes=%d used_bytes=%d free_bytes=%d", m.SwapTotalBytes, m.SwapUsedBytes, m.SwapFreeBytes)}, Confidence: ConfidenceMedium})
	} else {
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingMemorySwap), Area: AreaMemory, Severity: SeverityUnknown, Summary: "Swap usage unavailable", Confidence: ConfidenceLow})
	}
	if samples.CPU != nil {
		u := samples.CPU
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingCPUUtilization), Area: AreaCPU, Severity: SeverityInfo, Summary: fmt.Sprintf("CPU utilization: %.1f%% busy", u.BusyPercent), Evidence: []string{fmt.Sprintf("sample_interval=%s user=%.1f%% system=%.1f%% idle=%.1f%%", samples.CPUSampleInterval, u.UserPercent, u.SystemPercent, u.IdlePercent)}, Confidence: ConfidenceMedium})
	} else {
		r.Findings = append(r.Findings, Finding{ID: string(darwinFindingCPUUtilization), Area: AreaCPU, Severity: SeverityUnknown, Summary: "CPU utilization sampling unavailable", Confidence: ConfidenceLow})
	}
	for _, item := range unknowns {
		r.Findings = append(r.Findings, Finding{ID: string(item.id), Area: item.area, Severity: SeverityUnknown, Summary: item.summary, Confidence: ConfidenceLow})
	}
	return r
}
