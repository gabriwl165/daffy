//go:build linux

package disk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type fakePlatform struct {
	mounts      []Mount
	mountErr    error
	capacityErr error
	capacities  int
}

func (f *fakePlatform) Mounts() ([]Mount, error) { return f.mounts, f.mountErr }
func (f *fakePlatform) Capacity(p string) (MountCapacity, error) {
	f.capacities++
	if f.capacityErr != nil {
		return MountCapacity{}, f.capacityErr
	}
	return MountCapacity{Path: p}, nil
}
func diskMount(path, device string, fs Filesystem) Mount {
	return Mount{Path: path, Root: "/", Device: device, Filesystem: fs, Eligible: true}
}

func TestScannerConfigAndMountDiscoveryErrors(t *testing.T) {
	s := &scanner{platform: &fakePlatform{mountErr: os.ErrPermission}}
	for _, c := range []Config{{Path: t.TempDir()}, {}} {
		if _, e := s.Scan(context.Background(), c); e == nil {
			t.Fatal("mount discovery error swallowed")
		}
	}
	for _, c := range []Config{{TopN: -1}, {TopN: MaxTopN + 1}, {BatchSize: -1}, {BatchSize: MaxBatchSize + 1}, {TimeBudget: -1}} {
		if _, e := (&scanner{platform: &fakePlatform{}}).Scan(context.Background(), c); e == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
	}
}

func BenchmarkScannerTrees(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("wide-%d", count), func(b *testing.B) {
			root := b.TempDir()
			for i := 0; i < count; i++ {
				if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%05d", i)), make([]byte, 32), 0600); e != nil {
					b.Fatal(e)
				}
			}
			s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, e := s.Scan(context.Background(), Config{Path: root, TopN: 10}); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
	b.Run("deep", func(b *testing.B) {
		root := b.TempDir()
		p := root
		for i := 0; i < 200; i++ {
			p = filepath.Join(p, "d")
			if e := os.Mkdir(p, 0700); e != nil {
				b.Fatal(e)
			}
		}
		if e := os.WriteFile(filepath.Join(p, "leaf"), []byte("data"), 0600); e != nil {
			b.Fatal(e)
		}
		s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, e := s.Scan(context.Background(), Config{Path: root, TopN: 10}); e != nil {
				b.Fatal(e)
			}
		}
	})
}
