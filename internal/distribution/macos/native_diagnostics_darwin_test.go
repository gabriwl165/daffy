//go:build darwin

package macos

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"
)

type nativeDiagnosticRunner struct{}

func (nativeDiagnosticRunner) Run(parent context.Context, c DiagnosticCommand) DiagnosticResult {
	ctx, cancel := context.WithTimeout(parent, c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Executable, c.Args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return DiagnosticResult{Stdout: out, Stderr: stderr.Bytes(), Err: err, Truncated: len(out) > CPUOutputLimit || len(stderr.Bytes()) > DiskOutputLimit}
}

func TestNativeCPUAndMemoryAndDiskDiagnostics(t *testing.T) {
	ctx := context.Background()
	runner := nativeDiagnosticRunner{}
	cpu, err := (CPUCollector{runner}).Collect(ctx, time.Second, 5*time.Second)
	if err != nil || cpu.SampleInterval != time.Second || math.IsNaN(cpu.UserPercent) || math.IsInf(cpu.UserPercent, 0) || math.IsNaN(cpu.SystemPercent) || math.IsInf(cpu.SystemPercent, 0) || math.IsNaN(cpu.IdlePercent) || math.IsInf(cpu.IdlePercent, 0) || cpu.UserPercent+cpu.SystemPercent+cpu.IdlePercent < 99 || cpu.UserPercent < 0 || cpu.SystemPercent < 0 || cpu.IdlePercent < 0 {
		t.Fatalf("native CPU sample: %+v, %v", cpu, err)
	}
	memory, ramErr, swapErr := (MemoryCollector{runner}).Collect(ctx, 5*time.Second)
	if ramErr != nil {
		t.Fatalf("native RAM sample: %+v, %v", memory, ramErr)
	}
	if swapErr != nil || memory.SwapUsedBytes > memory.SwapTotalBytes || memory.SwapFreeBytes > memory.SwapTotalBytes {
		t.Fatalf("native swap sample: %+v, %v", memory, swapErr)
	}
	disk, err := (DiskCollector{runner}).Collect(ctx, time.Second, 5*time.Second, DiskDefaultDeviceLimit)
	if err != nil || disk.SampleInterval != time.Second || len(disk.Devices) == 0 {
		t.Fatalf("native disk sample: %+v, %v", disk, err)
	}
	for _, d := range disk.Devices {
		if math.IsNaN(d.MegabytesPerSecond) || math.IsInf(d.MegabytesPerSecond, 0) || d.MegabytesPerSecond < 0 {
			t.Fatalf("invalid native disk numbers: %+v", d)
		}
	}
}
