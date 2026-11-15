// Package optimize defines read-only optimizer plan data models.
package optimize

import (
	"time"

	"github.com/gabriwl165/daffy/internal/health"
)

// CapabilityStatus describes what the current health data can establish.
type CapabilityStatus string

const (
	CapabilityAvailable            CapabilityStatus = "available"
	CapabilityUnsupported          CapabilityStatus = "unsupported"
	CapabilityPermissionRestricted CapabilityStatus = "permission_restricted"
	CapabilityMissingDependency    CapabilityStatus = "missing_dependency"
	CapabilityUnavailable          CapabilityStatus = "unavailable"
)

// Capability records the status and explanation for one named capability.
type Capability struct {
	Status CapabilityStatus `json:"status"`
	Reason string           `json:"reason,omitempty"`
}

// CapabilitySet contains explicit capability categories; statuses should not
// imply distinctions that are not supported by the underlying health data.
type CapabilitySet struct {
	CPU       Capability `json:"cpu"`
	Memory    Capability `json:"memory"`
	Storage   Capability `json:"storage"`
	Thermal   Capability `json:"thermal"`
	Processes Capability `json:"processes"`
	History   Capability `json:"history"`
}

// SourceEvidence links a recommendation to a health finding and its evidence.
type SourceEvidence struct {
	FindingID string   `json:"finding_id"`
	Summary   string   `json:"summary"`
	Details   []string `json:"details"`
}

// SuggestedAction is descriptive only; the optional command is never run.
type SuggestedAction struct {
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
}

// Recommendation is a concrete, read-only suggestion derived from findings.
type Recommendation struct {
	ID         string            `json:"id"`
	Area       health.Area       `json:"area"`
	Severity   health.Severity   `json:"severity"`
	Confidence health.Confidence `json:"confidence"`
	Summary    string            `json:"summary"`
	Evidence   []SourceEvidence  `json:"evidence"`
	Action     SuggestedAction   `json:"action"`
}

// Plan is a read-only optimizer result. It describes suggestions and does not
// execute actions or persist data.
type Plan struct {
	SchemaVersion   string            `json:"schema_version"`
	CollectedAt     time.Time         `json:"collected_at"`
	System          health.SystemInfo `json:"system"`
	Capabilities    CapabilitySet     `json:"capabilities"`
	Recommendations []Recommendation  `json:"recommendations"`
	Limitations     []string          `json:"limitations"`
	ReadOnly        bool              `json:"read_only"`
}
