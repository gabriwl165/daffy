package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
)

type diskFunc func(context.Context, disk.Config) (disk.Report, error)

func (f diskFunc) Scan(c context.Context, x disk.Config) (disk.Report, error) { return f(c, x) }

func TestDiskFlags(t *testing.T) {
	p, c, e := diskFlags([]string{"/home", "--format", "json", "--exclude", "/tmp", "--exclude=/proc", "--time-budget=0", "--top", "12"})
	if e != nil || len(p) != 1 || p[0] != "/home" || c.format != diskFormatJSON || len(c.excludes) != 2 || c.budget != 0 || c.top != 12 {
		t.Fatalf("%v %+v %v", p, c, e)
	}
	if p, c, e = diskFlags(nil); e != nil || len(p) != 0 || c.top != 20 {
		t.Fatalf("default path must be omitted: %v %+v %v", p, c, e)
	}
	if _, c, e := diskFlags([]string{"--verbose"}); e != nil || !c.verbose {
		t.Fatalf("verbose flag: %+v %v", c, e)
	}
	if _, _, e := diskFlags([]string{"--verbose=true"}); e == nil {
		t.Fatal("verbose must not accept a value")
	}
	for _, a := range [][]string{{"--top", "2oops"}, {"--top", "1001"}, {"--json=garbage"}, {"--format", "xml"}, {"--time-budget", "-1s"}} {
		if _, _, e := diskFlags(a); e == nil {
			t.Errorf("accepted %v", a)
		}
	}
}

func TestDiskProgressIntegration(t *testing.T) {
	var saw bool
	d := Deps{Disk: diskFunc(func(_ context.Context, c disk.Config) (disk.Report, error) {
		if c.OnProgress == nil {
			if saw {
				t.Fatal("callback changed")
			}
			return disk.Report{}, nil
		}
		saw = true
		c.OnProgress(disk.Progress{Elapsed: time.Second, Files: 3, Directories: 2, CurrentPath: "/tmp\x1b"})
		return disk.Report{Root: "/"}, nil
	})}
	var out, errout bytes.Buffer
	if c := Run(context.Background(), RunConfig{Args: []string{"disk", "--json"}, Out: &out, ErrOut: &errout, Deps: d}); c != 0 || saw || errout.Len() != 0 {
		t.Fatalf("default: %d %v %q", c, saw, errout.String())
	}
	out.Reset()
	errout.Reset()
	if c := Run(context.Background(), RunConfig{Args: []string{"disk", "--json", "--progress"}, Out: &out, ErrOut: &errout, Deps: d}); c != 0 || !saw || !json.Valid(bytes.TrimSpace(out.Bytes())) || !strings.Contains(errout.String(), "files=3") || strings.Contains(errout.String(), "\x1b") {
		t.Fatalf("progress: %d %q %q", c, out.String(), errout.String())
	}
	d.Disk = diskFunc(func(_ context.Context, c disk.Config) (disk.Report, error) {
		c.OnProgress(disk.Progress{Elapsed: time.Second, CurrentPath: "/x"})
		return disk.Report{}, nil
	})
	if c := Run(context.Background(), RunConfig{Args: []string{"disk", "--json", "--progress"}, Out: &out, ErrOut: shortWriter{}, Deps: d}); c != 1 {
		t.Fatalf("writer failure exit %d", c)
	}
}

func TestDiskRunConfigJSONAndPartial(t *testing.T) {
	var got disk.Config
	d := Deps{Disk: diskFunc(func(_ context.Context, c disk.Config) (disk.Report, error) {
		got = c
		return disk.Report{Root: "/x", Totals: disk.Totals{AllocatedBytes: 2}}, context.DeadlineExceeded
	})}
	var out, errout bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: []string{"disk", "scan", "--json"}, Out: &out, ErrOut: &errout, Deps: d})
	if code != 1 || got.Path != "" || !strings.Contains(out.String(), `"root":"/x"`) {
		t.Fatalf("code=%d config=%+v output=%q err=%q", code, got, out.String(), errout.String())
	}
	bad := Deps{Disk: diskFunc(func(context.Context, disk.Config) (disk.Report, error) { return disk.Report{}, errors.New("broken") })}
	out.Reset()
	if code = Run(context.Background(), RunConfig{Args: []string{"disk", "scan", "--json"}, Out: &out, ErrOut: &errout, Deps: bad}); code != 1 || out.Len() != 0 {
		t.Fatalf("fatal emitted report: %d %q", code, out.String())
	}
}

func TestDiskDispatchAndHelp(t *testing.T) {
	calls := 0
	var paths []string
	d := Deps{Disk: diskFunc(func(_ context.Context, c disk.Config) (disk.Report, error) {
		calls++
		paths = append(paths, c.Path)
		return disk.Report{}, nil
	})}
	for _, tc := range []struct {
		args []string
		want int
		call bool
		path string
	}{{[]string{"disk", "scan"}, 0, true, ""}, {[]string{"disk"}, 0, true, ""}, {[]string{"disk", "/home"}, 0, true, "/home"}, {[]string{"disk", "--help"}, 0, false, ""}, {[]string{"disk", "scan", "/home", "--help"}, 0, false, ""}} {
		before := calls
		var out, errout bytes.Buffer
		if got := Run(context.Background(), RunConfig{Args: tc.args, Out: &out, ErrOut: &errout, Deps: d}); got != tc.want {
			t.Fatalf("%v exit %d: %s", tc.args, got, errout.String())
		}
		if (calls > before) != tc.call {
			t.Fatalf("%v calls=%d", tc.args, calls)
		}
		if tc.call && paths[len(paths)-1] != tc.path {
			t.Fatalf("%v path=%q want %q", tc.args, paths[len(paths)-1], tc.path)
		}
	}
	for _, a := range [][]string{{"disk", "scan", "--exclude"}, {"disk", "scan", "/a", "/b"}, {"disk", "scan", "--", "--help", "/extra"}} {
		var out, errout bytes.Buffer
		if c := Run(context.Background(), RunConfig{Args: a, Out: &out, ErrOut: &errout, Deps: d}); c != 2 {
			t.Errorf("%v exit=%d", a, c)
		}
	}
}

func TestDiskCancelAndWriterFailure(t *testing.T) {
	d := Deps{Disk: diskFunc(func(context.Context, disk.Config) (disk.Report, error) {
		return disk.Report{Root: "partial"}, context.Canceled
	})}
	var out, errout bytes.Buffer
	if c := Run(context.Background(), RunConfig{Args: []string{"disk", "scan", "--json"}, Out: &out, ErrOut: &errout, Deps: d}); c != 130 || !strings.Contains(out.String(), "partial") {
		t.Fatalf("cancel: %d %q", c, out.String())
	}
	d.Disk = diskFunc(func(context.Context, disk.Config) (disk.Report, error) { return disk.Report{}, nil })
	if c := Run(context.Background(), RunConfig{Args: []string{"disk", "scan", "--json"}, Out: failWriter{}, ErrOut: &errout, Deps: d}); c != 1 {
		t.Fatalf("writer failure exit %d", c)
	}
}

func TestDiskTextSanitizationAndSizes(t *testing.T) {
	var b bytes.Buffer
	r := disk.Report{Root: "/tmp\n", Candidates: []disk.Candidate{{Path: "/cache\x1b", Category: disk.CategoryPackageCache, Risk: disk.RiskLow, ObservedAllocatedBytes: 1024, Explanation: "safe\nreason", Recommendation: "review"}}}
	if e := printDisk(&b, r); e != nil {
		t.Fatal(e)
	}
	if strings.ContainsAny(b.String(), "\x1b") || strings.Contains(b.String(), "safe\nreason") || !strings.Contains(b.String(), "1.0 KiB") {
		t.Fatalf("unsafe/poor output %q", b.String())
	}
}

func TestDiskPermissionVerbosityAndJSON(t *testing.T) {
	permission := disk.Issue{Path: "/private\n\x1b", Operation: disk.OperationReadDir, Error: "denied", Kind: disk.IssueKindPermission}
	other := disk.Issue{Path: "/io", Operation: disk.OperationLstat, Error: "I/O failed", Kind: disk.IssueKindOther}
	makeReport := func(counter int64) disk.Report {
		return disk.Report{Root: "/", Issues: []disk.Issue{permission, other}, Coverage: disk.Coverage{PermissionErrors: counter, Complete: false, SkippedEntries: 1}}
	}
	var normal, verbose bytes.Buffer
	if err := printDisk(&normal, makeReport(5)); err != nil {
		t.Fatal(err)
	}
	if err := printDiskOptions(&verbose, makeReport(5), true); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{normal.String(), verbose.String()} {
		if !strings.Contains(s, "I/O failed") || !strings.Contains(s, "Results are partial") {
			t.Fatalf("lost EIO/partial status: %q", s)
		}
		if strings.Contains(strings.ToLower(s), "sudo") {
			t.Fatalf("sudo advice: %q", s)
		}
	}
	if !strings.Contains(normal.String(), "5 permission errors; use --verbose for details.") || strings.Contains(normal.String(), "denied") || !strings.Contains(normal.String(), "SCAN ISSUES") {
		t.Fatalf("default output: %q", normal.String())
	}
	if !strings.Contains(verbose.String(), "denied") || strings.Contains(verbose.String(), "permission errors; use --verbose") || strings.ContainsAny(verbose.String(), "\x1b\r") {
		t.Fatalf("verbose output: %q", verbose.String())
	}
	for _, counter := range []int64{0, 1} {
		var b bytes.Buffer
		if err := printDisk(&b, disk.Report{Issues: []disk.Issue{permission}, Coverage: disk.Coverage{PermissionErrors: counter}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "1 permission error;") {
			t.Fatalf("retained fallback counter %d: %q", counter, b.String())
		}
		if strings.Contains(b.String(), "SCAN ISSUES") || strings.Contains(b.String(), "denied") {
			t.Fatalf("permission-only report leaked details or an empty section: %q", b.String())
		}
	}
	d := Deps{Disk: diskFunc(func(context.Context, disk.Config) (disk.Report, error) { return makeReport(5), nil })}
	outputs := make([]string, 2)
	for i, args := range [][]string{{"disk", "--json"}, {"disk", "--json", "--verbose"}} {
		var out, errout bytes.Buffer
		if code := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &errout, Deps: d}); code != 0 || errout.Len() != 0 {
			t.Fatalf("json run %v: %d %q", args, code, errout.String())
		}
		outputs[i] = out.String()
	}
	if outputs[0] != outputs[1] || !json.Valid([]byte(outputs[0])) {
		t.Fatalf("JSON differs by verbosity: %q / %q", outputs[0], outputs[1])
	}
}

func TestDiskTextLayoutDiagnosticsAndBounds(t *testing.T) {
	var b bytes.Buffer
	r := disk.Report{Root: "/root", Coverage: disk.Coverage{Mounts: 2, ScannedMounts: 2, SkippedMounts: 1, Complete: true}, Mounts: []disk.MountCapacity{{Path: "/m\n\x1b", TotalBytes: 100, AvailableBytes: 25, TotalInodes: 100, FreeInodes: 0}, {Path: "/zero", TotalBytes: 0, AvailableBytes: 0}, {Path: "/bad", TotalBytes: 10, AvailableBytes: 20}}, TopFiles: []disk.FileEntry{{Path: "/f\r\x1b", AllocatedBytes: 5}, {Path: "/g", AllocatedBytes: 9}}, Issues: []disk.Issue{{Path: "/p\n", Operation: "read-dir\x1b", Error: "denied\r\x1b"}}}
	if err := printDisk(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"75% used) [#########---]", "0% free", "capacity unknown", "DIAGNOSTICS", "read-dir /p: denied", "Bar shows used space", "snapshots, shared blocks"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.ContainsAny(out, "\x1b\r") || strings.Contains(out, "Results are partial") || !strings.Contains(out, "  #  SIZE") || !strings.Contains(out, "2 ") {
		t.Fatalf("layout/sanitization: %q", out)
	}
	if got := bytesTextU(^uint64(0)); !strings.Contains(got, "EiB") {
		t.Fatalf("uint64 maximum clamped: %s", got)
	}
	if percent(0, 0, false) != "unknown" || bar(0, 0, false) != "[ capacity unknown ]" || percent(0, 10, false) != "unknown" {
		t.Fatal("invalid capacity presented as valid")
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }
func TestPrintDiskWriterFailure(t *testing.T) {
	if printDisk(shortWriter{}, disk.Report{}) == nil {
		t.Fatal("expected short write failure")
	}
}

func TestDiskDiagnosticLocationsAndInvalidInodes(t *testing.T) {
	r := disk.Report{Coverage: disk.Coverage{Complete: true}, Mounts: []disk.MountCapacity{
		{Path: "/critical\x1b", TotalBytes: 100, AvailableBytes: 1, TotalInodes: 10, FreeInodes: 20},
		{Path: "/other\nmount", TotalBytes: 100, AvailableBytes: 2, TotalInodes: 10, FreeInodes: 10},
	}}
	var out bytes.Buffer
	if err := printDisk(&out, r); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.ContainsAny(s, "\x1b\r") || !strings.Contains(s, "/critical") || !strings.Contains(s, "/othermount") {
		t.Fatalf("diagnostic locations missing/unsafe: %q", s)
	}
	if strings.Contains(s, "20% free") || strings.Contains(s, "200% free") {
		t.Fatalf("invalid inode count presented as valid: %q", s)
	}
	if strings.Index(s, "DIAGNOSTICS") > strings.Index(s, "LARGEST FILES") {
		t.Fatalf("findings should precede rankings: %q", s)
	}
}
