package disk

import (
	"fmt"
	"testing"
)

func TestBuildMountPlanWholeMachineAndAliases(t *testing.T) {
	mounts := []Mount{
		{Path: "/", Root: "/", Device: "8:1", Filesystem: FilesystemExt4, Eligible: true},
		{Path: "/data", Root: "/", Device: "8:2", Filesystem: FilesystemXFS, Eligible: true},
		{Path: "/proc", Root: "/", Device: "0:5", Filesystem: FilesystemProc, Eligible: true},
		{Path: "/net", Root: "/", Device: "0:8", Filesystem: FilesystemNFS, Eligible: true},
		{Path: "/bind", Root: "/", Device: "8:2", Filesystem: FilesystemXFS, Eligible: true},
		{Path: "/btrfs-a", Root: "/vol/a", Device: "8:3", Filesystem: FilesystemBtrfs, Eligible: true},
		{Path: "/btrfs-b", Root: "/vol/b", Device: "8:3", Filesystem: FilesystemBtrfs, Eligible: true},
		{Path: "/run", Root: "/", Device: "0:9", Filesystem: FilesystemTmpfs, Eligible: true},
	}
	p, e := BuildMountPlan(mounts, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 4 {
		t.Fatalf("roots = %#v", p.Roots)
	}
	if _, ok := p.Boundaries[Path("/proc")]; !ok {
		t.Fatal("ineligible mount boundary missing")
	}
	if len(p.Skipped) != 4 {
		t.Fatalf("skipped = %#v", p.Skipped)
	}
}

func TestBuildMountPlanExplicitSelectionAndBoundaries(t *testing.T) {
	m := []Mount{{Path: "/", Root: "/", Device: "a", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/a", Root: "/", Device: "b", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/a/no", Root: "/", Device: "c", Filesystem: FilesystemTmpfs, Eligible: true}, {Path: "/abc", Root: "/", Device: "d", Filesystem: FilesystemExt4, Eligible: true}}
	p, e := BuildMountPlan(m, "/a", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || p.Roots[0].Path != "/a" {
		t.Fatalf("roots = %#v", p.Roots)
	}
	if _, ok := p.Boundaries[Path("/a/no")]; !ok {
		t.Fatal("excluded boundary absent")
	}
	if p.Exclusions.Contains("/abc/x") {
		t.Fatal("component-prefix false match")
	}
	if _, e := BuildMountPlan(m, "/a/no", nil); e == nil {
		t.Fatal("explicit ineligible filesystem accepted")
	}
}

func TestBuildMountPlanExplicitIncludesNestedEligibleRoots(t *testing.T) {
	m := []Mount{{Path: "/", Root: "/", Device: "a", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/work", Root: "/", Device: "b", Filesystem: FilesystemXFS, Eligible: true}, {Path: "/work/data", Root: "/", Device: "c", Filesystem: FilesystemBtrfs, Eligible: true}, {Path: "/work/cache", Root: "/", Device: "d", Filesystem: FilesystemTmpfs, Eligible: true}}
	p, e := BuildMountPlan(m, "/work", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 2 || p.Roots[0].Path != "/work" || p.Roots[1].Path != "/work/data" {
		t.Fatalf("roots %#v", p.Roots)
	}
}

func TestPathTrieComponentContainmentAndConfig(t *testing.T) {
	var x PathTrie
	if e := x.Add("/a"); e != nil {
		t.Fatal(e)
	}
	if !x.Contains("/a/b") || !x.Contains("/a") || x.Contains("/abc") {
		t.Fatal("bad component matching")
	}
	if e := x.Add("relative"); e == nil {
		t.Fatal("relative exclusion accepted")
	}
	if MaxTopN != 1000 {
		t.Fatal(MaxTopN)
	}
}

func TestBuildMountPlanDuplicateMountpoint(t *testing.T) {
	p, e := BuildMountPlan([]Mount{{Path: "/", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/", Filesystem: FilesystemXFS, Eligible: true}, {Path: "/work", Root: "/", Device: "w", Filesystem: FilesystemExt4, Eligible: true}}, "/", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || p.Roots[0].Path != "/work" || len(p.Skipped) != 1 || p.Skipped[0].SkipReason != SkipAmbiguousMount {
		t.Fatalf("stacked mount plan: %+v", p)
	}
	if _, ok := p.Boundaries[Path("/")]; !ok {
		t.Fatal("ambiguous boundary missing")
	}
	if _, e = BuildMountPlan([]Mount{{Path: "/", Root: "/", Device: "r", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/dupe", Root: "/", Device: "a", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/dupe", Root: "/", Device: "b", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/work", Root: "/", Device: "w", Filesystem: FilesystemExt4, Eligible: true}}, "/work", nil); e != nil {
		t.Fatalf("unrelated explicit path failed: %v", e)
	}
	if _, e = BuildMountPlan([]Mount{{Path: "/", Root: "/", Device: "r", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/dupe", Root: "/", Device: "a", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/dupe", Root: "/", Device: "b", Filesystem: FilesystemExt4, Eligible: true}}, "/dupe", nil); e == nil {
		t.Fatal("explicit ambiguous path accepted")
	}
}

func TestBindRepresentativeSelectionHonorsScopeAndExclusions(t *testing.T) {
	m := []Mount{{Path: "/", Root: "/", Device: "root", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/bind", Root: "/", Device: "shared", Filesystem: FilesystemBtrfs, Eligible: true}, {Path: "/data", Root: "/", Device: "shared", Filesystem: FilesystemBtrfs, Eligible: true}}
	p, e := BuildMountPlan(m, "/data", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || p.Roots[0].Path != "/data" {
		t.Fatalf("selected alias root suppressed: %+v", p.Roots)
	}
	p, e = BuildMountPlan(m, "/", []string{"/bind"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 2 || p.Roots[1].Path != "/data" {
		t.Fatalf("excluded alias suppressed usable root: roots=%+v", p.Roots)
	}
	found := false
	for _, s := range p.Skipped {
		if s.Path == "/bind" && s.SkipReason == SkipExcludedMount {
			found = true
		}
	}
	if !found {
		t.Fatalf("excluded alias not recorded: %+v", p.Skipped)
	}
	preferred := []Mount{{Path: "/", Root: "/", Device: "root", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/data", Root: "/vol/sub", Device: "same", Filesystem: FilesystemBtrfs, Eligible: true}, {Path: "/data/child", Root: "/vol", Device: "same", Filesystem: FilesystemBtrfs, Eligible: true}}
	p, e = BuildMountPlan(preferred, "/data", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || p.Roots[0].Path != "/data" {
		t.Fatalf("containing mount lost to nested broad alias: %+v", p.Roots)
	}
}

func TestPathTrieOverlapsComponentBoundaries(t *testing.T) {
	var trie PathTrie
	for _, p := range []string{"/a/b", "/x"} {
		if e := trie.Add(p); e != nil {
			t.Fatal(e)
		}
	}
	for _, p := range []string{"/a", "/a/b/c", "/x/y"} {
		if !trie.Overlaps(p) {
			t.Fatalf("expected overlap %s", p)
		}
	}
	for _, p := range []string{"/abc", "/a/c", "/xy"} {
		if trie.Overlaps(p) {
			t.Fatalf("false overlap %s", p)
		}
	}
}

func TestBuildMountPlanOverlayAndRemovable(t *testing.T) {
	m := []Mount{{Path: "/", Root: "/", Device: "a", Filesystem: FilesystemOverlay, Eligible: true}, {Path: "/cache", Root: "/", Device: "b", Filesystem: FilesystemOverlay, Eligible: true}, {Path: "/media/disk", Root: "/", Device: "c", Filesystem: FilesystemExt4, Eligible: true}}
	p, e := BuildMountPlan(m, "/", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || len(p.Skipped) != 2 {
		t.Fatalf("plan %#v", p)
	}
}

func TestBuildMountPlanOrdersAliasesByFilesystemRoot(t *testing.T) {
	// Mountpoint lexical/depth order deliberately opposes root hierarchy order.
	m := []Mount{{Path: "/b", Root: "/sub", Device: "d", Filesystem: FilesystemBtrfs, Eligible: true}, {Path: "/longpath", Root: "/", Device: "d", Filesystem: FilesystemBtrfs, Eligible: true}}
	p, e := BuildMountPlan(m, "/", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || p.Roots[0].Path != "/longpath" {
		t.Fatalf("broad root was not retained: %#v", p.Roots)
	}
}

func TestBuildMountPlanUnknownNetworkFilesystemExcluded(t *testing.T) {
	m := []Mount{{Path: "/", Root: "/", Device: "d", Filesystem: FilesystemExt4, Eligible: true}, {Path: "/ssh", Root: "/", Device: "n", Filesystem: Filesystem("fuse.sshfs"), Eligible: true}}
	p, e := BuildMountPlan(m, "/", nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Roots) != 1 || len(p.Skipped) != 1 {
		t.Fatalf("plan %#v", p)
	}
}

func BenchmarkBuildMountPlanThousands(b *testing.B) {
	m := make([]Mount, 10000)
	for i := range m {
		m[i] = Mount{Path: fmt.Sprintf("/mount/%05d", i), Root: fmt.Sprintf("/vol/%05d", i), Device: "shared", Filesystem: FilesystemBtrfs, Eligible: true}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildMountPlan(m, "/", nil); err != nil {
			b.Fatal(err)
		}
	}
}
