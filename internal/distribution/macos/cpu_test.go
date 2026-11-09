package macos

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseCPUUsage(t *testing.T) {
	got, err := ParseCPUUsage("CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: 20% user, 10% sys, 70% idle\n")
	if err != nil || got.BusyPercent != 30 {
		t.Fatalf("got %#v, %v", got, err)
	}
}

type fakeRunner struct {
	result  DiagnosticResult
	command DiagnosticCommand
	after   context.CancelFunc
}

func (f *fakeRunner) Run(ctx context.Context, c DiagnosticCommand) DiagnosticResult {
	f.command = c
	if f.after != nil {
		f.after()
	}
	return f.result
}

func TestCollectCommandConfigAndFailures(t *testing.T) {
	valid := []byte("CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: 20% user, 10% sys, 70% idle\n")
	ctx := context.Background()
	runner := &fakeRunner{result: DiagnosticResult{Stdout: valid}}
	if _, err := (CPUCollector{runner}).Collect(ctx, 1500*time.Millisecond, 0); err != nil {
		t.Fatal(err)
	}
	if runner.command.Executable != CPUExecutable || runner.command.Timeout != 4*time.Second || strings.Join(runner.command.Args, " ") != "-l 2 -s 2 -n 0 -F" {
		t.Fatalf("command: %+v", runner.command)
	}
	for _, result := range []DiagnosticResult{{Err: errors.New("failed")}, {Stderr: []byte("diagnostic"), Stdout: valid}, {Stdout: valid, Truncated: true}, {Stdout: make([]byte, CPUOutputLimit+1)}} {
		runner.result = result
		if _, err := (CPUCollector{runner}).Collect(ctx, time.Second, 5*time.Second); err == nil {
			t.Fatal("accepted invalid command result")
		}
	}
	runner.result = DiagnosticResult{Stdout: valid}
	cancelled, cancel := context.WithCancel(ctx)
	runner.after = cancel
	if _, err := (CPUCollector{runner}).Collect(cancelled, time.Second, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := (CPUCollector{}).Collect(ctx, time.Second, 5*time.Second); err == nil {
		t.Fatal("nil runner accepted")
	}
	if _, err := (CPUCollector{runner}).Collect(ctx, time.Second, time.Second); err == nil {
		t.Fatal("short timeout accepted")
	}
}

func TestParseCPUUsageRejectsBadTotalsAndNonfinite(t *testing.T) {
	for _, s := range []string{"CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: 20% user, 10% sys, 60% idle", "CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: NaN% user, 10% sys, 70% idle"} {
		if _, err := ParseCPUUsage(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}

func TestParseCPUUsageAllowsTopUnpaddedHundredths(t *testing.T) {
	// Apple's top prints fractional integers without zero-padding. Tick counts
	// 108, 209, 9683 out of 10000 display as 1.8, 2.9, 96.83 (total 101.53).
	got, err := ParseCPUUsage("CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: 1.8% user, 2.9% sys, 96.83% idle")
	if err != nil || math.Abs(got.BusyPercent-4.7) > 0.0001 {
		t.Fatalf("got %#v, %v", got, err)
	}
	for _, total := range []string{"20% user, 11% sys, 72% idle", "20% user, 11% sys, 66% idle"} {
		input := "CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: " + total
		if _, err := ParseCPUUsage(input); err == nil {
			t.Fatalf("accepted inconsistent total: %q", input)
		}
	}
}
