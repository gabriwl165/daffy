//go:build darwin

package health

import (
	"context"
	"runtime"
	"time"
)

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
	r := darwinReport(kernel, cpu, runtime.GOARCH, memResult)
	r.CollectedAt = started
	root := coreRootFilesystemFinding()
	r.Findings = append(r.Findings, root)
	if ctx.Err() != nil {
		r.Findings = append(r.Findings, Finding{ID: coreFindingCollectionCancelled, Area: AreaCore, Severity: SeverityUnknown, Summary: "Collection stopped before all samples completed", Confidence: ConfidenceLow})
	}
	r.Duration = time.Since(started)
	return r
}
