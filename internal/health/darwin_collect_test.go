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

func TestDarwinMemoryFindingsIndependentAndUnique(t *testing.T) {
	m := &macos.MemoryUsage{ImmediatelyFreeBytes: 8192, SpeculativeBytes: 4096, SpeculativePagesPresent: true, SwapTotalBytes: 1 << 30, SwapUsedBytes: 1 << 29, SwapFreeBytes: 1 << 29}
	r := darwinReport("", "", "arm64", integrationResult{}, darwinSamples{Memory: m})
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.ID]++
		if f.ID == string(darwinFindingMemoryFree) && f.Severity != SeverityInfo {
			t.Fatalf("free: %+v", f)
		}
		if f.ID == string(darwinFindingMemorySwap) && f.Severity != SeverityInfo {
			t.Fatalf("swap: %+v", f)
		}
	}
	for _, id := range []darwinFindingID{darwinFindingMemoryFree, darwinFindingMemorySpeculative, darwinFindingMemorySwap} {
		if counts[string(id)] != 1 {
			t.Fatalf("%s count %d", id, counts[string(id)])
		}
	}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), "immediately_free_bytes=8192") {
		t.Fatalf("json %s %v", b, err)
	}
	r = darwinReport("", "", "arm64", integrationResult{}, darwinSamples{Memory: m, MemoryErr: errors.New("ram fail")})
	for _, f := range r.Findings {
		if f.ID == string(darwinFindingMemorySwap) && f.Severity != SeverityInfo {
			t.Fatal("RAM failure discarded valid swap")
		}
	}
}

func TestDarwinDiskFindingUnique(t *testing.T) {
	d := &macos.DiskUsage{Devices: []macos.DiskDeviceActivity{{Device: "disk0", KilobytesPerTransfer: 1, TransfersPerSecond: 2, MegabytesPerSecond: 3}}, SampleInterval: time.Second, DeviceLimit: 32, CoverageNote: "second sample"}
	r := darwinReport("", "", "arm64", integrationResult{}, darwinSamples{Disk: d})
	count := 0
	for _, f := range r.Findings {
		if f.ID == string(darwinFindingDiskActivity) {
			count++
			if f.Severity != SeverityInfo || len(f.Evidence) != 2 {
				t.Fatalf("disk finding: %+v", f)
			}
		}
	}
	if count != 1 {
		t.Fatalf("disk activity finding count %d", count)
	}
	r = darwinReport("", "", "arm64", integrationResult{}, darwinSamples{})
	count = 0
	for _, f := range r.Findings {
		if f.ID == string(darwinFindingDiskActivity) && f.Severity == SeverityUnknown {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unknown disk finding count %d", count)
	}
}

func TestDarwinReportCapacityAndUnknowns(t *testing.T) {
	valid := darwinReport("24.1", "Apple CPU", "arm64", integrationResult{stdout: []byte("17179869184\n")}, darwinSamples{})
	if valid.System.Kernel != "24.1" || valid.System.CPUModel != "Apple CPU" || valid.Findings[0].Severity != SeverityInfo {
		t.Fatalf("unexpected valid report: %+v", valid)
	}
	for _, finding := range valid.Findings {
		if finding.ID == string(darwinFindingCPUUtilization) && finding.Severity != SeverityUnknown {
			t.Fatalf("CPU utilization must remain unknown: %+v", finding)
		}
	}
	for _, result := range []integrationResult{{}, {stdout: []byte("n/a")}, {stdout: []byte("999999"), err: errors.New("failed")}, {stdout: make([]byte, 4097)}} {
		r := darwinReport("", "", "arm64", result, darwinSamples{})
		if r.Findings[0].Severity != SeverityUnknown {
			t.Fatalf("expected unknown capacity: %+v", r.Findings[0])
		}
	}
}
