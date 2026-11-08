package macos

import (
	"context"
	"errors"
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
