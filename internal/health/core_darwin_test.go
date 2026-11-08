//go:build darwin

package health

import (
	"context"
	"errors"
	"testing"
)

func TestCollectDarwinUsesBoundedSysctlAndKeepsRootFinding(t *testing.T) {
	calls := 0
	r := collectDarwinWith(context.Background(), Options{}, func(_ context.Context, cfg commandConfig) integrationResult {
		calls++
		if cfg.Tool != darwinSysctlCommand || cfg.Timeout != 0 || len(cfg.Args) != 2 || cfg.Args[0] != "-n" {
			t.Fatalf("unexpected command config: %+v", cfg)
		}
		for _, arg := range cfg.Args {
			if arg == "journalctl" || arg == "sysctl" && cfg.Args[0] != "-n" {
				t.Fatalf("unexpected command argument: %+v", cfg.Args)
			}
		}
		switch cfg.Args[1] {
		case string(darwinKernelKey):
			return integrationResult{stdout: []byte("24.1\n"), stderr: []byte("private diagnostic")}
		case string(darwinCPUModelKey):
			return integrationResult{stdout: []byte("Apple CPU")}
		case string(darwinMemorySizeKey):
			return integrationResult{stdout: []byte("17179869184")}
		default:
			t.Fatalf("unexpected sysctl key: %s", cfg.Args[1])
		}
		return integrationResult{}
	})
	if calls != 3 || r.CollectedAt.IsZero() {
		t.Fatalf("expected three bounded calls and timestamp: %+v", r)
	}
	for _, f := range r.Findings {
		if f.ID == coreFindingDiskRoot {
			return
		}
		for _, e := range f.Evidence {
			if e == "private diagnostic" {
				t.Fatal("stderr leaked")
			}
		}
	}
	t.Fatal("root filesystem finding missing")
}

func TestCollectDarwinCancellationIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := collectDarwinWith(ctx, Options{}, func(context.Context, commandConfig) integrationResult {
		return integrationResult{err: errors.New("cancelled")}
	})
	for _, f := range r.Findings {
		if f.ID == coreFindingCollectionCancelled {
			return
		}
	}
	t.Fatal("cancellation finding missing")
}
