package optimize

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/health"
)

func TestPlanJSONUsesNamedFieldsAndReadOnlyData(t *testing.T) {
	plan := Plan{
		SchemaVersion: "1",
		CollectedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		System:        health.SystemInfo{OS: "linux", Kernel: "6.1"},
		Capabilities: CapabilitySet{
			CPU:       Capability{Status: CapabilityAvailable},
			Memory:    Capability{Status: CapabilityPermissionRestricted, Reason: "restricted"},
			Storage:   Capability{Status: CapabilityMissingDependency},
			Thermal:   Capability{Status: CapabilityUnsupported},
			Processes: Capability{Status: CapabilityUnavailable},
			History:   Capability{Status: CapabilityUnavailable},
		},
		Recommendations: []Recommendation{{
			ID: "cpu-1", Area: health.AreaCPU, Severity: health.SeverityWarning,
			Confidence: health.ConfidenceHigh, Summary: "Review CPU load",
			Evidence: []SourceEvidence{{FindingID: "finding-1", Summary: "Load is high", Details: []string{"load average"}}},
			Action:   SuggestedAction{Description: "Inspect active workload", Command: "ps"},
		}},
		Limitations: []string{"No historical data"},
		ReadOnly:    true,
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		SchemaVersion json.RawMessage `json:"schema_version"`
		CollectedAt   json.RawMessage `json:"collected_at"`
		System        json.RawMessage `json:"system"`
		Capabilities  struct {
			CPU       json.RawMessage `json:"cpu"`
			Memory    json.RawMessage `json:"memory"`
			Storage   json.RawMessage `json:"storage"`
			Thermal   json.RawMessage `json:"thermal"`
			Processes json.RawMessage `json:"processes"`
			History   json.RawMessage `json:"history"`
		} `json:"capabilities"`
		Recommendations json.RawMessage `json:"recommendations"`
		Limitations     json.RawMessage `json:"limitations"`
		ReadOnly        *bool           `json:"read_only"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	planFields := []struct {
		name  string
		value json.RawMessage
	}{
		{"schema_version", wire.SchemaVersion}, {"collected_at", wire.CollectedAt},
		{"system", wire.System}, {"recommendations", wire.Recommendations},
		{"limitations", wire.Limitations},
	}
	for _, field := range planFields {
		key, value := field.name, field.value
		if len(value) == 0 {
			t.Errorf("missing plan field %q", key)
		}
	}
	capabilityFields := []struct {
		name  string
		value json.RawMessage
	}{
		{"cpu", wire.Capabilities.CPU}, {"memory", wire.Capabilities.Memory},
		{"storage", wire.Capabilities.Storage}, {"thermal", wire.Capabilities.Thermal},
		{"processes", wire.Capabilities.Processes}, {"history", wire.Capabilities.History},
	}
	for _, field := range capabilityFields {
		key, value := field.name, field.value
		if len(value) == 0 {
			t.Errorf("missing named capability %q", key)
		}
	}
	if wire.ReadOnly == nil || !*wire.ReadOnly {
		t.Fatal("plan must explicitly declare read-only mode")
	}

	var roundTrip Plan
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Capabilities.Memory.Status != CapabilityPermissionRestricted {
		t.Fatal("capability status did not round-trip")
	}
	statuses := []struct {
		status CapabilityStatus
		wire   string
	}{
		{CapabilityAvailable, "available"},
		{CapabilityUnsupported, "unsupported"},
		{CapabilityPermissionRestricted, "permission_restricted"},
		{CapabilityMissingDependency, "missing_dependency"},
		{CapabilityUnavailable, "unavailable"},
	}
	for _, test := range statuses {
		encodedStatus, err := json.Marshal(test.status)
		if err != nil {
			t.Fatal(err)
		}
		if string(encodedStatus) != `"`+test.wire+`"` {
			t.Errorf("status %q encoded as %s", test.status, encodedStatus)
		}
	}
}
