//go:build darwin

package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/distribution"
	"github.com/gabriwl165/daffy/internal/distribution/macos"
)

func TestCollectDarwinUsesBoundedSysctlAndKeepsRootFinding(t *testing.T) {
	calls := 0
	r := collectDarwinWith(context.Background(), Options{}, func(_ context.Context, cfg commandConfig) integrationResult {
		calls++
		if cfg.Tool == distribution.Tool(macos.CPUExecutable) {
			if cfg.Timeout != 3*time.Second || len(cfg.Args) != 7 {
				t.Fatalf("unexpected CPU command config: %+v", cfg)
			}
			return integrationResult{stdout: []byte("CPU usage: 10% user, 5% sys, 85% idle\nCPU usage: 20% user, 10% sys, 70% idle\n")}
		}
		if cfg.Tool == distribution.Tool(macos.MemoryVMStatExecutable) {
			return integrationResult{stdout: []byte("Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 20.\nPages speculative: 10.\n")}
		}
		if cfg.Tool == distribution.Tool(macos.MemorySysctlExecutable) {
			if len(cfg.Args) != 2 || cfg.Args[1] != string(macos.MemorySwapUsageKey) {
				t.Fatalf("unexpected swap args: %+v", cfg.Args)
			}
			return integrationResult{stdout: []byte("total = 1G used = 512M free = 512M")}
		}
		if cfg.Tool == distribution.Tool(macos.DiskExecutable) {
			if cfg.Timeout != 3*time.Second || len(cfg.Args) != 7 {
				t.Fatalf("unexpected iostat config: %+v", cfg)
			}
			return integrationResult{stdout: []byte("disk0\nKB/t t/s MB/s\n1 2 3\n4 5 6\n")}
		}
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
	if calls != 7 || r.CollectedAt.IsZero() {
		t.Fatalf("expected seven bounded calls and timestamp: %+v", r)
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
		t.Fatal("executor called after cancellation")
		return integrationResult{err: errors.New("cancelled")}
	})
	for _, f := range r.Findings {
		if f.ID == coreFindingCollectionCancelled {
			return
		}
	}
	t.Fatal("cancellation finding missing")
}
