package health

import (
	"errors"
	"testing"
)

func TestDarwinReportCapacityAndUnknowns(t *testing.T) {
	valid := darwinReport("24.1", "Apple CPU", "arm64", integrationResult{stdout: []byte("17179869184\n")})
	if valid.System.Kernel != "24.1" || valid.System.CPUModel != "Apple CPU" || valid.Findings[0].Severity != SeverityInfo {
		t.Fatalf("unexpected valid report: %+v", valid)
	}
	if valid.Findings[1].ID != string(darwinFindingCPUUtilization) || valid.Findings[1].Severity != SeverityUnknown {
		t.Fatalf("CPU utilization must remain unknown: %+v", valid.Findings[1])
	}
	for _, result := range []integrationResult{{}, {stdout: []byte("n/a")}, {stdout: []byte("999999"), err: errors.New("failed")}, {stdout: make([]byte, 4097)}} {
		r := darwinReport("", "", "arm64", result)
		if r.Findings[0].Severity != SeverityUnknown {
			t.Fatalf("expected unknown capacity: %+v", r.Findings[0])
		}
	}
}
