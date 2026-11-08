package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakePlatform struct{}

func (fakePlatform) Mounts() ([]Mount, error)                 { return []Mount{{Path: "/", Eligible: true}}, nil }
func (fakePlatform) Capacity(p string) (MountCapacity, error) { return MountCapacity{Path: p}, nil }
func TestScannerTotalsAndExclusion(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f"), []byte("hello"), 0600)
	os.Mkdir(filepath.Join(root, "skip"), 0700)
	os.WriteFile(filepath.Join(root, "skip", "x"), []byte("ignored"), 0600)
	s := scanner{platform: fakePlatform{}}
	r, e := s.Scan(context.Background(), Config{Path: root, Excludes: []string{filepath.Join(root, "skip")}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Totals.ApparentBytes != 5 || r.Coverage.Files != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
}
func BenchmarkScannerWide(b *testing.B) {
	for i := 0; i < b.N; i++ {
		root := b.TempDir()
		for n := 0; n < 500; n++ {
			os.WriteFile(filepath.Join(root, filepath.Base(filepath.Clean(string(rune('a'+n%26))))+string(rune('A'+n/26))), make([]byte, 16), 0600)
		}
		s := scanner{platform: fakePlatform{}}
		_, _ = s.Scan(context.Background(), Config{Path: root})
	}
}
