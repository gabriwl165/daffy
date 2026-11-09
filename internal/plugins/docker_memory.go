package plugins

// DockerMemoryDenominator identifies what a reported memory percentage uses.
type DockerMemoryDenominator string

const (
	DockerMemoryDenominatorContainerLimit DockerMemoryDenominator = "container-limit"
	DockerMemoryDenominatorDaemonHost     DockerMemoryDenominator = "daemon-host"
	DockerMemoryDenominatorUnknown        DockerMemoryDenominator = "unknown"
)

// DockerMemoryDiagnostic contains sampled container memory observations, not application heap data.
type DockerMemoryDiagnostic struct {
	ID                  string                  `json:"id"`
	Name                string                  `json:"name"`
	CurrentBytes        *uint64                 `json:"current_bytes,omitempty"`
	PeakBytes           *uint64                 `json:"peak_bytes,omitempty"`
	FirstBytes          *uint64                 `json:"first_bytes,omitempty"`
	ObservedGrowthBytes *int64                  `json:"observed_growth_bytes,omitempty"`
	LimitBytes          *uint64                 `json:"limit_bytes,omitempty"`
	HeadroomBytes       *uint64                 `json:"headroom_bytes,omitempty"`
	OOMKilled           *bool                   `json:"oom_killed,omitempty"`
	Denominator         DockerMemoryDenominator `json:"denominator"`
	SampleCount         int                     `json:"sample_count"`
	PressureSampleCount int                     `json:"pressure_sample_count"`
	SustainedPressure   bool                    `json:"sustained_pressure"`
	RisingUsage         bool                    `json:"rising_usage"`
}
