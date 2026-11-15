package optimize

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriwl165/daffy/internal/health"
)

func TestAnalyzeCapabilitiesAndUnknownReasons(t *testing.T) {
	p := Analyze(health.Report{Findings: []health.Finding{{ID: fTemperature, Severity: health.SeverityUnknown, Summary: "temperature unavailable"}, {ID: fThrottle, Severity: health.SeverityOK, Summary: "historical throttle"}}})
	if p.Capabilities.Thermal.Status != CapabilityUnavailable || p.Capabilities.Thermal.Reason == "" {
		t.Fatalf("unknown temperature capability: %+v", p.Capabilities.Thermal)
	}
	if p.Capabilities.CPU.Status != CapabilityUnavailable || p.Capabilities.CPU.Reason == "" {
		t.Fatalf("absent capability: %+v", p.Capabilities.CPU)
	}
	if p.Capabilities.History.Status != CapabilityUnsupported || len(p.Recommendations) != 0 {
		t.Fatalf("unexpected inference: %+v", p)
	}
	if !hasLimitation(p, "temperature unavailable") {
		t.Fatal("unknown finding reason lost")
	}
}

func TestAnalyzeEligibleAndIgnoredIDs(t *testing.T) {
	ids := []string{fTemperature, fThrottle, fCPU, fProcess, fMemory, fDisk, fPressureCPU, fPressureMemory, fPressureIO, fBuild, "disk.activity", "swap.activity", "collection.cancelled", "future.signal"}
	findings := make([]health.Finding, 0, len(ids))
	for _, id := range ids {
		findings = append(findings, health.Finding{ID: id, Severity: health.SeverityWarning, Summary: id})
	}
	p := Analyze(health.Report{Findings: findings})
	if len(p.Recommendations) != 10 {
		t.Fatalf("recommendations count %d: %+v", len(p.Recommendations), p.Recommendations)
	}
	for _, r := range p.Recommendations {
		if r.ID == "disk.activity" || r.ID == "swap.activity" {
			t.Fatalf("ignored ID recommended: %s", r.ID)
		}
	}
}

func TestAnalyzePermutationDedupAndEvidenceCopy(t *testing.T) {
	a := health.Finding{ID: fCPU, Severity: health.SeverityWarning, Summary: "cpu", Confidence: health.ConfidenceHigh, Evidence: []string{"z", "a"}}
	b := health.Finding{ID: fCPU, Severity: health.SeverityWarning, Summary: "cpu", Confidence: health.ConfidenceHigh, Evidence: []string{"a", "a"}}
	c := health.Finding{ID: fMemory, Severity: health.SeverityCritical, Summary: "memory", Evidence: []string{"x"}}
	r1 := health.Report{Findings: []health.Finding{a, b, c}}
	r2 := health.Report{Findings: []health.Finding{c, b, a}}
	snapshot := cloneFinding(a)
	p1, p2 := Analyze(r1), Analyze(r2)
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("input order changed plan: %#v != %#v", p1, p2)
	}
	if len(p1.Recommendations) != 2 || p1.Recommendations[0].ID != fMemory || p1.Recommendations[1].ID != fCPU {
		t.Fatalf("severity/ID ordering or dedup failed: %+v", p1.Recommendations)
	}
	if !reflect.DeepEqual(r1.Findings[0], snapshot) {
		t.Fatal("input mutated")
	}
	if p1.Recommendations[1].Confidence != b.Confidence {
		t.Fatal("source confidence lost")
	}
	r1.Findings[1].Evidence[0] = "changed"
	if p1.Recommendations[1].Evidence[0].Details[0] != "a" {
		t.Fatal("plan aliases source evidence")
	}
}

func TestAnalyzeEmptyJSONArrays(t *testing.T) {
	p := Analyze(health.Report{})
	type wire struct {
		Recommendations []Recommendation `json:"recommendations"`
		Limitations     []string         `json:"limitations"`
	}
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var got wire
	if e = json.Unmarshal(b, &got); e != nil {
		t.Fatal(e)
	}
	if got.Recommendations == nil || got.Limitations == nil || len(got.Recommendations) != 0 {
		t.Fatalf("arrays not non-nil empty: %+v", got)
	}
	if got.Recommendations == nil {
		t.Fatalf("JSON: %s", b)
	}
	for _, r := range Analyze(health.Report{Findings: []health.Finding{{ID: fCPU, Severity: health.SeverityWarning}}}).Recommendations {
		if r.Evidence[0].Details == nil {
			t.Fatal("empty details must encode as []")
		}
	}
}

func hasLimitation(p Plan, text string) bool {
	for _, s := range p.Limitations {
		if strings.Contains(s, text) {
			return true
		}
	}
	return false
}
