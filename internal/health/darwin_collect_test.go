package health

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	macos "github.com/gabriwl165/daffy/internal/distribution/macos"
)

func TestDarwinCPUFindingUniqueAndJSON(t *testing.T) {
	usage := &macos.CPUUsage{UserPercent: 20, SystemPercent: 10, IdlePercent: 70, BusyPercent: 30}
	r := darwinReport("24.1", "Apple", "arm64", integrationResult{}, darwinSamples{CPU: usage, CPUSampleInterval: time.Second})
	count := 0
	for _, f := range r.Findings {
		if f.ID == string(darwinFindingCPUUtilization) {
			count++
			if f.Severity != SeverityInfo || len(f.Evidence) != 1 {
				t.Fatalf("finding: %+v", f)
			}
		}
	}
	if count != 1 {
		t.Fatalf("CPU finding count %d", count)
	}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), "sample_interval=1s") {
		t.Fatalf("JSON %s, %v", b, err)
	}
	r = darwinReport("", "", "arm64", integrationResult{}, darwinSamples{})
	count = 0
	for _, f := range r.Findings {
		if f.ID == string(darwinFindingCPUUtilization) && f.Severity == SeverityUnknown {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unknown CPU finding count %d", count)
	}
}

func TestDarwinReportCapacityAndUnknowns(t *testing.T) {
	valid := darwinReport("24.1", "Apple CPU", "arm64", integrationResult{stdout: []byte("17179869184\n")}, darwinSamples{})
	if valid.System.Kernel != "24.1" || valid.System.CPUModel != "Apple CPU" || valid.Findings[0].Severity != SeverityInfo {
		t.Fatalf("unexpected valid report: %+v", valid)
	}
	if valid.Findings[1].ID != string(darwinFindingCPUUtilization) || valid.Findings[1].Severity != SeverityUnknown {
		t.Fatalf("CPU utilization must remain unknown: %+v", valid.Findings[1])
	}
	for _, result := range []integrationResult{{}, {stdout: []byte("n/a")}, {stdout: []byte("999999"), err: errors.New("failed")}, {stdout: make([]byte, 4097)}} {
		r := darwinReport("", "", "arm64", result, darwinSamples{})
		if r.Findings[0].Severity != SeverityUnknown {
			t.Fatalf("expected unknown capacity: %+v", r.Findings[0])
		}
	}
}
