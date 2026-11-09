//go:build linux

package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

func TestPermissionIssueClassificationAndJSONSchema(t *testing.T) {
	for _, err := range []error{fmt.Errorf("wrapped: %w", os.ErrPermission), fmt.Errorf("wrapped: %w", syscall.EPERM)} {
		if got := classifyIssueError(err); got != IssueKindPermission {
			t.Fatalf("classify %v: %v", err, got)
		}
	}
	if got := classifyIssueError(errors.New("permission denied")); got != IssueKindOther {
		t.Fatalf("ordinary error kind=%v", got)
	}
	data, err := json.Marshal(Report{Issues: []Issue{{Kind: IssueKindPermission}}, Coverage: Coverage{PermissionErrors: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"root":"","totals":{"apparent_bytes":0,"allocated_bytes":0},"top_files":null,"top_directories":null,"mounts":null,"candidates":null,"issues":[{"operation":"","error":""}],"coverage":{"mounts":0,"scanned_mounts":0,"skipped_mounts":0,"directories":0,"files":0,"skipped_entries":0,"omitted_errors":0,"complete":false}}` {
		t.Fatalf("unexpected JSON schema: %s", data)
	}
}

func TestEarlyMountErrorsClassifiedAndFatal(t *testing.T) {
	for _, cause := range []error{syscall.EACCES, syscall.EPERM} {
		wrapped := fmt.Errorf("mount lookup: %w", cause)
		r, err := (&scanner{platform: &fakePlatform{mountErr: wrapped}}).Scan(context.Background(), Config{})
		if !errors.Is(err, cause) {
			t.Fatalf("fatal error %v does not preserve %v", err, cause)
		}
		if r.Coverage.PermissionErrors != 1 || len(r.Issues) != 1 || r.Issues[0].Kind != IssueKindPermission {
			t.Fatalf("cause %v report=%+v", cause, r)
		}
	}
}

func TestPermissionIssueAccountingBeyondReportLimit(t *testing.T) {
	root := t.TempDir()
	mounts := make([]Mount, MaxReportIssues+5)
	for i := range mounts {
		path := filepath.Join(root, fmt.Sprintf("mount-%03d", i))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		mounts[i] = diskMount(path, fmt.Sprintf("dev-%03d", i), FilesystemExt4)
	}
	// Include one ordinary failure among typed permission failures.
	platform := &capacitySequencePlatform{mounts: mounts}
	for i := 0; i < len(mounts); i++ {
		if i == 0 {
			platform.errs = append(platform.errs, errors.New("permission denied"))
		} else {
			platform.errs = append(platform.errs, fmt.Errorf("wrapped: %w", syscall.EACCES))
		}
	}
	r, err := (&scanner{platform: platform}).Scan(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Issues) != MaxReportIssues {
		t.Fatalf("retained issues=%d", len(r.Issues))
	}
	if r.Coverage.PermissionErrors != int64(len(mounts)-1) {
		t.Fatalf("permission failures=%d want %d", r.Coverage.PermissionErrors, len(mounts)-1)
	}
	if r.Coverage.OmittedErrors != 5 || r.Coverage.Complete {
		t.Fatalf("coverage=%+v", r.Coverage)
	}
}

type capacitySequencePlatform struct {
	mounts []Mount
	errs   []error
	index  int
}

func (p *capacitySequencePlatform) Mounts() ([]Mount, error) { return p.mounts, nil }
func (p *capacitySequencePlatform) Capacity(string) (MountCapacity, error) {
	err := p.errs[p.index]
	p.index++
	return MountCapacity{}, err
}

func TestCapacityPermissionErrorClassification(t *testing.T) {
	root := t.TempDir()
	p := &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}, capacityErr: fmt.Errorf("capacity: %w", syscall.EPERM)}
	r, err := (&scanner{platform: p}).Scan(context.Background(), Config{Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.PermissionErrors != 1 || len(r.Issues) != 1 || r.Issues[0].Kind != IssueKindPermission {
		t.Fatalf("report=%+v", r)
	}
}

func TestScannerProgressInitialFinalAndError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	var got []Progress
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}
	_, err := s.Scan(context.Background(), Config{Path: root, OnProgress: func(p Progress) { got = append(got, p) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0].Files != 0 || got[len(got)-1].Files != 1 || got[len(got)-1].Directories != 1 {
		t.Fatalf("unexpected progress: %+v", got)
	}
	if got[0].CurrentPath != "" {
		t.Fatalf("initial path=%q", got[0].CurrentPath)
	}

	var failures []Progress
	bad := &scanner{platform: &fakePlatform{mountErr: os.ErrPermission}}
	if _, err = bad.Scan(context.Background(), Config{OnProgress: func(p Progress) { failures = append(failures, p) }}); err == nil {
		t.Fatal("expected error")
	}
	if len(failures) != 2 {
		t.Fatalf("error path callbacks=%d", len(failures))
	}
}

func TestScannerProgressCanceledAndUnset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	s := &scanner{platform: &fakePlatform{}}
	if _, err := s.Scan(ctx, Config{OnProgress: func(Progress) { calls++ }}); err == nil {
		t.Fatal("expected cancellation")
	}
	if calls != 2 {
		t.Fatalf("callbacks=%d", calls)
	}
	s.platform = &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}
	if _, err := s.Scan(context.Background(), Config{Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("unset callback invoked: %d", calls)
	}
}

func TestOfferReplacementPreservesRanking(t *testing.T) {
	h := rankHeap{}
	for _, v := range []rankedEntry{{"c", Totals{AllocatedBytes: 1}}, {"b", Totals{AllocatedBytes: 2}}, {"a", Totals{AllocatedBytes: 2}}, {"d", Totals{AllocatedBytes: 3}}} {
		offer(&h, v, 2)
	}
	if h.Len() != 2 || !((h[0].path == "a" && h[1].path == "d") || (h[0].path == "d" && h[1].path == "a")) {
		t.Fatalf("heap=%+v", h)
	}
}

func TestScannerProgressThrottleAndNestedFinalPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1, 0)
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}, now: func() time.Time { now = now.Add(100 * time.Millisecond); return now }}
	var got []Progress
	_, err := s.Scan(context.Background(), Config{Path: root, OnProgress: func(p Progress) { got = append(got, p) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 {
		t.Fatalf("throttling callbacks=%d: %+v", len(got), got)
	}
	for i := 2; i < len(got)-1; i++ {
		if got[i].Elapsed-got[i-1].Elapsed < 250*time.Millisecond {
			t.Fatalf("unthrottled callbacks: %+v", got)
		}
	}
	final := got[len(got)-1]
	if final.CurrentPath != filepath.Clean(root) || final.Files != 1 || final.Directories != 2 {
		t.Fatalf("final=%+v", final)
	}
}

func BenchmarkOffer(b *testing.B) {
	h := make(rankHeap, 0, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offer(&h, rankedEntry{path: fmt.Sprintf("%08d", i), totals: Totals{AllocatedBytes: int64(i)}}, 100)
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
