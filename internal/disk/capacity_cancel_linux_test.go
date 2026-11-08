//go:build linux

package disk

import (
	"context"
	"errors"
	"testing"
)

type cancelOnFirstCapacityPlatform struct {
	cancel        context.CancelFunc
	capacityCalls int
}

func (p *cancelOnFirstCapacityPlatform) Mounts() ([]Mount, error) {
	return []Mount{
		diskMount("/synthetic-one", "device-one", FilesystemExt4),
		diskMount("/synthetic-two", "device-two", FilesystemExt4),
	}, nil
}

func (p *cancelOnFirstCapacityPlatform) Capacity(path string) (MountCapacity, error) {
	p.capacityCalls++
	if p.capacityCalls == 1 {
		p.cancel()
	}
	return MountCapacity{Path: path}, nil
}

func TestScannerCancellationStopsCapacityLoopBeforeOpeningRoots(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	platform := &cancelOnFirstCapacityPlatform{cancel: cancel}
	s := &scanner{
		platform: platform,
		openRoot: func(path string) (Directory, error) {
			t.Fatalf("opened root after capacity cancellation: %q", path)
			return nil, nil
		},
	}
	report, err := s.Scan(ctx, Config{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan error = %v, want context.Canceled", err)
	}
	if report.Coverage.Complete {
		t.Fatalf("canceled scan marked complete: %+v", report.Coverage)
	}
	if platform.capacityCalls != 1 {
		t.Fatalf("Capacity called %d times, want 1", platform.capacityCalls)
	}
}
