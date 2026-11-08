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

type Finding struct {
	ID             string   `json:"id"`
	Area           string   `json:"area"`
	Severity       string   `json:"severity"`
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Confidence     string   `json:"confidence"`
}

type Report struct {
	SchemaVersion string            `json:"schema_version"`
	CollectedAt   time.Time         `json:"collected_at"`
	Duration      time.Duration     `json:"duration_ns"`
	System        map[string]string `json:"system"`
	Findings      []Finding         `json:"findings"`
}

type Check struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail"`
}

// CollectCore gathers read-only Linux health signals from procfs and sysfs.
func CollectCore(ctx context.Context, opts Options) Report {
	return collectCore(ctx, opts)
}
