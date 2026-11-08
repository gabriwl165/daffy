package health

import (
	"context"
	"time"
)

type Options struct {
	ProcRoot         string
	SysRoot          string
	EtcRoot          string
	SampleInterval   time.Duration
	IncludeSensitive bool
	Privileged       bool
	CommandTimeout   time.Duration
}

type Severity string
type Confidence string
type Area string

const (
	SeverityUnknown  Severity   = "unknown"
	SeverityInfo     Severity   = "info"
	SeverityOK       Severity   = "ok"
	SeverityWarning  Severity   = "warning"
	SeverityCritical Severity   = "critical"
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
	AreaCPU          Area       = "cpu"
	AreaProcess      Area       = "process"
	AreaWorkload     Area       = "workload"
	AreaCore         Area       = "core"
	AreaMemory       Area       = "memory"
	AreaThermal      Area       = "thermal"
	AreaDisk         Area       = "disk"
	AreaPressure     Area       = "pressure"
	AreaLogs         Area       = "logs"
	AreaServices     Area       = "services"
	AreaSwap         Area       = "swap"
	AreaBoot         Area       = "boot"
	AreaGPU          Area       = "gpu"
	AreaPower        Area       = "power"
	AreaStorage      Area       = "storage"
)

type Finding struct {
	ID             string     `json:"id"`
	Area           Area       `json:"area"`
	Severity       Severity   `json:"severity"`
	Summary        string     `json:"summary"`
	Evidence       []string   `json:"evidence,omitempty"`
	Recommendation string     `json:"recommendation,omitempty"`
	Confidence     Confidence `json:"confidence"`
}

type SystemInfo struct {
	OS            string `json:"os"`
	Kernel        string `json:"kernel"`
	DistroID      string `json:"distro_id"`
	DistroAdapter string `json:"distro_adapter"`
	CPUModel      string `json:"cpu_model,omitempty"`
	DistroVersion string `json:"distro_version,omitempty"`
}

type Report struct {
	SchemaVersion string        `json:"schema_version"`
	CollectedAt   time.Time     `json:"collected_at"`
	Duration      time.Duration `json:"duration_ns"`
	System        SystemInfo    `json:"system"`
	Findings      []Finding     `json:"findings"`
}

type Check struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail"`
}

// CollectCore gathers read-only signals through the build's native provider.
func CollectCore(ctx context.Context, opts Options) Report {
	return collectPlatformCore(ctx, opts)
}

// SupportedPlatform reports whether this build has a native health provider.
func SupportedPlatform() bool { return platformSupported }
