package optimize

import (
	"slices"
	"sort"

	"github.com/gabriwl165/daffy/internal/health"
)

const (
	fCPU            = "cpu.utilization"
	fMemory         = "memory.capacity"
	fDisk           = "disk.root"
	fTemperature    = "thermal.temperature"
	fThrottle       = "thermal.throttle"
	fProcess        = "process.cpu"
	fPressureCPU    = "pressure.cpu"
	fPressureMemory = "pressure.memory"
	fPressureIO     = "pressure.io"
	fBuild          = "workload.build"
)

type observedFinding struct {
	Present bool
	Finding health.Finding
}
type signalSet struct {
	CPU, Memory, Storage, Temperature, Throttle, Process observedFinding
	PressureCPU, PressureMemory, PressureIO, Build       observedFinding
}

func (s *signalSet) selectFinding(id string) *observedFinding {
	switch id {
	case fCPU:
		return &s.CPU
	case fMemory:
		return &s.Memory
	case fDisk:
		return &s.Storage
	case fTemperature:
		return &s.Temperature
	case fThrottle:
		return &s.Throttle
	case fProcess:
		return &s.Process
	case fPressureCPU:
		return &s.PressureCPU
	case fPressureMemory:
		return &s.PressureMemory
	case fPressureIO:
		return &s.PressureIO
	case fBuild:
		return &s.Build
	default:
		return nil
	}
}

// Analyze derives a read-only plan without collecting data or executing actions.
func Analyze(report health.Report) Plan {
	plan := Plan{SchemaVersion: "1", CollectedAt: report.CollectedAt, System: report.System, ReadOnly: true, Recommendations: make([]Recommendation, 0), Limitations: make([]string, 0)}
	var signals signalSet
	for _, f := range report.Findings {
		p := signals.selectFinding(f.ID)
		if p == nil {
			continue
		}
		if !p.Present || findingLess(f, p.Finding) {
			p.Present = true
			p.Finding = cloneFinding(f)
		}
	}
	plan.Capabilities = CapabilitySet{
		CPU: capability(signals.CPU, fCPU), Memory: capability(signals.Memory, fMemory), Storage: capability(signals.Storage, fDisk),
		Thermal: capability(signals.Temperature, fTemperature), Processes: capability(signals.Process, fProcess),
		History: Capability{Status: CapabilityUnsupported, Reason: "Historical analysis is not implemented."},
	}
	for _, p := range []struct {
		id    string
		value observedFinding
	}{{fTemperature, signals.Temperature}, {fThrottle, signals.Throttle}, {fCPU, signals.CPU}, {fProcess, signals.Process}, {fMemory, signals.Memory}, {fDisk, signals.Storage}, {fPressureCPU, signals.PressureCPU}, {fPressureMemory, signals.PressureMemory}, {fPressureIO, signals.PressureIO}, {fBuild, signals.Build}} {
		if p.value.Present && (p.value.Finding.Severity == health.SeverityWarning || p.value.Finding.Severity == health.SeverityCritical) {
			plan.Recommendations = append(plan.Recommendations, recommendation(p.value.Finding))
		}
	}
	sort.Slice(plan.Recommendations, func(i, j int) bool {
		a, b := plan.Recommendations[i], plan.Recommendations[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		return a.ID < b.ID
	})
	plan.Limitations = append(plan.Limitations, "History is not implemented; sampled observations do not establish cause.", "No recursive storage analysis or reclaim estimate is performed.", "Missing capabilities are not evidence of healthy conditions.")
	for _, p := range []struct {
		id    string
		value observedFinding
	}{{fTemperature, signals.Temperature}, {fThrottle, signals.Throttle}, {fCPU, signals.CPU}, {fProcess, signals.Process}, {fMemory, signals.Memory}, {fDisk, signals.Storage}, {fPressureCPU, signals.PressureCPU}, {fPressureMemory, signals.PressureMemory}, {fPressureIO, signals.PressureIO}, {fBuild, signals.Build}} {
		if !p.value.Present {
			plan.Limitations = append(plan.Limitations, p.id+": no finding was supplied; availability cannot be established.")
		} else if !validSeverity(p.value.Finding.Severity) {
			plan.Limitations = append(plan.Limitations, p.id+": "+p.value.Finding.Summary)
		}
	}
	return plan
}

func validSeverity(s health.Severity) bool {
	return s == health.SeverityOK || s == health.SeverityInfo || s == health.SeverityWarning || s == health.SeverityCritical
}
func capability(o observedFinding, id string) Capability {
	if o.Present && validSeverity(o.Finding.Severity) {
		return Capability{Status: CapabilityAvailable}
	}
	if o.Present {
		return Capability{Status: CapabilityUnavailable, Reason: "Finding for " + id + " has unknown or invalid severity: " + o.Finding.Summary}
	}
	return Capability{Status: CapabilityUnavailable, Reason: "No finding for " + id + "; legacy reports do not distinguish unsupported signals from unavailable data."}
}
func cloneFinding(f health.Finding) health.Finding {
	f.Evidence = append([]string{}, f.Evidence...)
	return f
}
func findingLess(a, b health.Finding) bool {
	if severityRank(a.Severity) != severityRank(b.Severity) {
		return severityRank(a.Severity) < severityRank(b.Severity)
	}
	if a.ID != b.ID {
		return a.ID < b.ID
	}
	if a.Area != b.Area {
		return a.Area < b.Area
	}
	if a.Summary != b.Summary {
		return a.Summary < b.Summary
	}
	if a.Confidence != b.Confidence {
		return a.Confidence < b.Confidence
	}
	return slices.Compare(a.Evidence, b.Evidence) < 0
}
func severityRank(s health.Severity) int {
	switch s {
	case health.SeverityCritical:
		return 0
	case health.SeverityWarning:
		return 1
	case health.SeverityInfo, health.SeverityOK:
		return 2
	default:
		return 3
	}
}
func recommendation(f health.Finding) Recommendation {
	desc := "Review the reported signal and investigate the underlying condition manually."
	switch f.ID {
	case fTemperature:
		desc = "Stop heavy work and monitor cooling. If temperature remains high, idle the system; if it stays high, shut down and inspect cooling."
	case fThrottle:
		desc = "Review the thermal throttle observation and cooling; stop heavy work if temperature is high."
	case fCPU:
		desc = "Review CPU saturation and competing workloads; identify changes to consider manually."
	case fProcess:
		desc = "Review the reported process CPU consumption and decide whether to adjust it; no background activity is inferred."
	case fMemory:
		desc = "Review memory capacity and consumers; consider appropriate memory-management steps."
	case fPressureCPU:
		desc = "Review CPU contention and competing workloads."
	case fPressureMemory:
		desc = "Review memory reclaim pressure and memory consumers."
	case fPressureIO:
		desc = "Review I/O latency and competing I/O workloads."
	case fDisk:
		desc = "Review disk usage manually. As an explicit potentially expensive follow-up, you may run daffy disk /."
	case fBuild:
		desc = "Review build workload and reduce concurrent builds if appropriate."
	}
	action := SuggestedAction{Description: desc}
	if f.ID == fDisk {
		action.Command = "daffy disk /"
	}
	details := append([]string{}, f.Evidence...)
	return Recommendation{ID: f.ID, Area: f.Area, Severity: f.Severity, Confidence: f.Confidence, Summary: f.Summary, Evidence: []SourceEvidence{{FindingID: f.ID, Summary: f.Summary, Details: details}}, Action: action}
}
