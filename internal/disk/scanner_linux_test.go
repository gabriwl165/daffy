//go:build linux

package disk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func metadataAt(t *testing.T, p string) EntryMetadata {
	t.Helper()
	i, e := os.Lstat(p)
	if e != nil {
		t.Fatal(e)
	}
	m, e := entryMetadata(i)
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestScannerAccountsDirectoriesHardlinksSymlinksAndSparseFiles(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "sub"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "a"), []byte("12345"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(filepath.Join(root, "a"), filepath.Join(root, "z-link")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("/", filepath.Join(root, "b-symlink")); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(filepath.Join(root, "c-sparse"))
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(4 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e = os.WriteFile(filepath.Join(root, "sub", "d"), []byte("nested"), 0600); e != nil {
		t.Fatal(e)
	}
	expectedA := metadataAt(t, filepath.Join(root, "a"))
	expectedSparse := metadataAt(t, filepath.Join(root, "c-sparse"))
	expectedLink := metadataAt(t, filepath.Join(root, "b-symlink"))
	expectedSub := metadataAt(t, filepath.Join(root, "sub"))
	expectedD := metadataAt(t, filepath.Join(root, "sub", "d"))
	expectedRoot := metadataAt(t, root)
	wantApp := expectedRoot.ApparentBytes + expectedA.ApparentBytes + expectedSparse.ApparentBytes + expectedLink.ApparentBytes + expectedSub.ApparentBytes + expectedD.ApparentBytes
	wantAlloc := expectedRoot.AllocatedBytes + expectedA.AllocatedBytes + expectedSparse.AllocatedBytes + expectedLink.AllocatedBytes + expectedSub.AllocatedBytes + expectedD.AllocatedBytes
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}
	r, e := s.Scan(context.Background(), Config{Path: root, TopN: 10})
	if e != nil {
		t.Fatal(e)
	}
	if r.Totals.ApparentBytes != wantApp || r.Totals.AllocatedBytes != wantAlloc {
		t.Fatalf("totals got %+v want apparent=%d allocated=%d", r.Totals, wantApp, wantAlloc)
	}
	if r.Coverage.Files != 4 {
		t.Fatalf("hardlink should be accounted once; coverage %+v", r.Coverage)
	}
	links := 0
	for _, v := range r.TopFiles {
		if v.Path == filepath.Join(root, "a") || v.Path == filepath.Join(root, "z-link") {
			links++
		}
	}
	if links != 1 {
		t.Fatalf("hardlink aliases ranked %d times: %+v", links, r.TopFiles)
	}
	if expectedSparse.ApparentBytes <= expectedSparse.AllocatedBytes {
		t.Fatalf("fixture is not sparse: %+v", expectedSparse)
	}
}

func TestScannerMountBoundariesAndExclusions(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "mounted")
	excluded := filepath.Join(root, "virtual")
	for _, p := range []string{nested, excluded, filepath.Join(root, "skip")} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for p, v := range map[string]string{filepath.Join(root, "root-file"): "root", filepath.Join(nested, "child"): "nested", filepath.Join(excluded, "child"): "virtual", filepath.Join(root, "skip", "child"): "skip"} {
		if e := os.WriteFile(p, []byte(v), 0600); e != nil {
			t.Fatal(e)
		}
	}
	platform := &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4), diskMount(nested, "nested", FilesystemBtrfs), diskMount(excluded, "virtual", FilesystemTmpfs)}}
	r, e := (&scanner{platform: platform}).Scan(context.Background(), Config{Path: root, Excludes: []string{filepath.Join(root, "skip")}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Coverage.ScannedMounts != 2 || r.Coverage.SkippedMounts != 1 {
		t.Fatalf("mount counts %+v", r.Coverage)
	}
	if platform.capacities != 2 {
		t.Fatalf("capacity called %d times", platform.capacities)
	}
	if r.Coverage.Files != 2 {
		t.Fatalf("boundary/exclusion traversal leaked entries: %+v", r.Coverage)
	}
	if r.Totals.ApparentBytes < 9 {
		t.Fatalf("eligible mounted data not included: %+v", r.Totals)
	}
	if !r.Coverage.Complete {
		t.Fatalf("intentional mount/exclusion skips marked incomplete: %+v", r.Coverage)
	}
}

func TestScannerExplicitExcludedRootDoesNotOpenOrQueryCapacity(t *testing.T) {
	root := t.TempDir()
	platform := &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}
	s := &scanner{platform: platform, openRoot: func(string) (Directory, error) { t.Fatal("excluded root opened"); return nil, nil }}
	r, e := s.Scan(context.Background(), Config{Path: root, Excludes: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	if platform.capacities != 0 || r.Coverage.SkippedMounts != 1 || r.Coverage.ScannedMounts != 0 || !r.Coverage.Complete {
		t.Fatalf("excluded root handling: report=%+v capacities=%d", r, platform.capacities)
	}
}

func TestScannerRootSymlinkAndMountCapacityErrors(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	if e := os.Mkdir(actual, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(base, "link")
	if e := os.Symlink(actual, link); e != nil {
		t.Fatal(e)
	}
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}
	if _, e := s.Scan(context.Background(), Config{Path: link}); e == nil {
		t.Fatal("symlink root accepted")
	}
	if e := os.WriteFile(filepath.Join(actual, "f"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	platform := &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}, capacityErr: errors.New("statfs denied")}
	r, e := (&scanner{platform: platform}).Scan(context.Background(), Config{Path: actual})
	if e != nil {
		t.Fatal(e)
	}
	if r.Coverage.Complete || r.Coverage.SkippedEntries != 0 || len(r.Issues) != 1 {
		t.Fatalf("capacity failure not surfaced: %+v", r)
	}
}

type interceptDirectory struct {
	Directory
	childCalls   int
	cancel       context.CancelFunc
	failChildren bool
	failClose    bool
}

func (d *interceptDirectory) ChildMetadata(name string) (EntryMetadata, error) {
	d.childCalls++
	if d.failChildren {
		return EntryMetadata{}, errors.New("injected metadata failure")
	}
	m, e := d.Directory.ChildMetadata(name)
	if d.childCalls == 2 && d.cancel != nil {
		d.cancel()
	}
	return m, e
}
func (d *interceptDirectory) OpenChild(name string) (Directory, error) {
	child, e := d.Directory.OpenChild(name)
	if e != nil {
		return nil, e
	}
	return &interceptDirectory{Directory: child, failChildren: d.failChildren}, nil
}
func (d *interceptDirectory) Close() error {
	e := d.Directory.Close()
	if d.failClose {
		return errors.New("injected close failure")
	}
	return e
}

func TestScannerCancellationRetainsPartialTotalsAndTopFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}, openRoot: func(p string) (Directory, error) {
		d, e := openScanRoot(p)
		if e != nil {
			return nil, e
		}
		return &interceptDirectory{Directory: d, cancel: cancel}, nil
	}}
	r, e := s.Scan(ctx, Config{Path: root, TopN: 10})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", e)
	}
	if r.Coverage.Complete || r.Totals.ApparentBytes <= 0 || len(r.TopFiles) != 1 {
		t.Fatalf("partial result lost: report=%+v top=%+v", r, r.TopFiles)
	}
}

func TestScannerBoundedErrorsAndIncompleteCoverage(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 120; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("f-%03d", i)), []byte("x"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	s := &scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}, capacityErr: errors.New("capacity error")}, openRoot: func(p string) (Directory, error) {
		d, e := openScanRoot(p)
		if e != nil {
			return nil, e
		}
		return &interceptDirectory{Directory: d, failChildren: true}, nil
	}}
	r, e := s.Scan(context.Background(), Config{Path: root})
	if e != nil {
		t.Fatal(e)
	}
	if r.Coverage.Complete || r.Coverage.SkippedEntries != 120 || r.Coverage.OmittedErrors != 21 || len(r.Issues) != MaxReportIssues {
		t.Fatalf("bad bounded issue accounting: coverage=%+v issues=%d", r.Coverage, len(r.Issues))
	}
}

func TestScannerRankingBoundedAndDeterministicTies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z", "b", "a", "c"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte("same"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	r, e := (&scanner{platform: &fakePlatform{mounts: []Mount{diskMount("/", "root", FilesystemExt4)}}}).Scan(context.Background(), Config{Path: root, TopN: 2})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.TopFiles) != 2 || filepath.Base(r.TopFiles[0].Path) != "a" || filepath.Base(r.TopFiles[1].Path) != "b" {
		t.Fatalf("unexpected deterministic top files: %+v", r.TopFiles)
	}
}
