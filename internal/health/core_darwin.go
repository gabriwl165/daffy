//go:build darwin

package health

import (
	"context"
	"runtime"
	"time"

	"github.com/gabriwl165/daffy/internal/distribution"
	macos "github.com/gabriwl165/daffy/internal/distribution/macos"
)

type darwinDiagnosticRunner struct{ run integrationExecutor }

func (r darwinDiagnosticRunner) Run(ctx context.Context, c macos.DiagnosticCommand) macos.DiagnosticResult {
	v := r.run(ctx, commandConfig{Timeout: c.Timeout, Tool: distribution.Tool(c.Executable), Args: c.Args})
	return macos.DiagnosticResult{Stdout: v.stdout, Stderr: v.stderr, Err: v.err, Truncated: v.truncated}
}

func collectDarwin(ctx context.Context, opts Options) Report {
	return collectDarwinWith(ctx, opts, integrationRun)
}

func collectDarwinWith(ctx context.Context, opts Options, run integrationExecutor) Report {
	started := time.Now()
	read := func(key darwinSysctlKey) integrationResult {
		if err := ctx.Err(); err != nil {
			return integrationResult{err: err}
		}
		return run(ctx, commandConfig{Timeout: opts.CommandTimeout, Tool: darwinSysctlCommand, Args: []string{"-n", string(key)}})
	}
	kernel := darwinValue(read(darwinKernelKey))
	cpu := darwinValue(read(darwinCPUModelKey))
	memResult := read(darwinMemorySizeKey)
	var samples darwinSamples
	if ctx.Err() == nil {
		if usage, err := (macos.CPUCollector{Runner: darwinDiagnosticRunner{run}}).Collect(ctx, opts.SampleInterval, opts.CommandTimeout); err == nil {
			samples = darwinSamples{CPU: &usage, CPUSampleInterval: usage.SampleInterval}
		}
	}
	if ctx.Err() == nil {
		usage, ramErr, swapErr := (macos.MemoryCollector{Runner: darwinDiagnosticRunner{run}}).Collect(ctx, opts.CommandTimeout)
		samples.Memory, samples.MemoryErr, samples.SwapErr = &usage, ramErr, swapErr
	}
	if ctx.Err() == nil {
		if disk, err := (macos.DiskCollector{Runner: darwinDiagnosticRunner{run}}).Collect(ctx, opts.SampleInterval, opts.CommandTimeout, macos.DiskDefaultDeviceLimit); err == nil {
			samples.Disk = &disk
		}
	}
	r := darwinReport(kernel, cpu, runtime.GOARCH, memResult, samples)
	r.CollectedAt = started
	root := coreRootFilesystemFinding()
	r.Findings = append(r.Findings, root)
	if ctx.Err() != nil {
		r.Findings = append(r.Findings, Finding{ID: coreFindingCollectionCancelled, Area: AreaCore, Severity: SeverityUnknown, Summary: "Collection stopped before all samples completed", Confidence: ConfidenceLow})
	}
	r.Duration = time.Since(started)
	return r
}
