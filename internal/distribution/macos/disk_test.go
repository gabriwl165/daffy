package macos

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const diskFixture = "              disk0               disk1\n    KB/t  t/s  MB/s     KB/t  t/s  MB/s\n   20.00   10  0.20     30.00  20  0.60\n   10.00    5  0.05     15.00  10  0.15\n"

func TestParseDiskUsageSecondSampleMultiDevice(t *testing.T) {
	u, err := ParseDiskUsage(diskFixture, 2, 32)
	if err != nil {
		t.Fatal(err)
	}
	if u.SampleInterval != 2*time.Second || len(u.Devices) != 2 || u.Devices[0].MegabytesPerSecond != .05 || u.Devices[1].TransfersPerSecond != 10 {
		t.Fatalf("%+v", u)
	}
}

func TestParseDiskUsageRejectsInvalidSamples(t *testing.T) {
	for _, input := range []string{"disk0\nKB/t t/s MB/s\n1 2 3\n", "disk0\nKB/t t/s MB/s\n1 2 NaN\n1 2 3", "disk0\nKB/t t/s MB/s\n1 -1 3\n1 2 3", "disk0\nKB/t unknown MB/s\n1 2 3\n1 2 3"} {
		if _, err := ParseDiskUsage(input, 1, 32); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	if _, err := ParseDiskUsage(strings.Repeat("x", DiskOutputLimit+1), 1, 32); err == nil {
		t.Fatal("accepted oversized output")
	}
	if _, err := ParseDiskUsage("disk0\nKB/t t/s MB/s\n0 0 0\n0 0 0", 1, 1); err != nil {
		t.Fatal(err)
	}
}

type diskFake struct {
	result  DiagnosticResult
	command DiagnosticCommand
	cancel  context.CancelFunc
}

func (f *diskFake) Run(ctx context.Context, c DiagnosticCommand) DiagnosticResult {
	f.command = c
	if f.cancel != nil {
		f.cancel()
	}
	return f.result
}
func TestDiskCollectorCommandErrorsAndCancellation(t *testing.T) {
	f := &diskFake{result: DiagnosticResult{Stdout: []byte(diskFixture)}}
	u, err := (DiskCollector{f}).Collect(context.Background(), 1500*time.Millisecond, 0, 0)
	if err != nil || u.SampleInterval != 2*time.Second || f.command.Executable != DiskExecutable || strings.Join(f.command.Args, " ") != "-d -w 2 -c 2 -n 32" || f.command.Timeout != 4*time.Second {
		t.Fatalf("%+v %v %+v", u, err, f.command)
	}
	for _, r := range []DiagnosticResult{{Err: errors.New("failed"), Stdout: []byte(diskFixture)}, {Stderr: []byte("diag"), Stdout: []byte(diskFixture)}, {Truncated: true, Stdout: []byte(diskFixture)}, {Stdout: make([]byte, DiskOutputLimit+1)}} {
		f.result = r
		if _, err := (DiskCollector{f}).Collect(context.Background(), time.Second, 3*time.Second, 32); err == nil {
			t.Fatal("accepted invalid result")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f = &diskFake{result: DiagnosticResult{Stdout: []byte(diskFixture)}, cancel: cancel}
	if _, err := (DiskCollector{f}).Collect(ctx, time.Second, 3*time.Second, 32); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
