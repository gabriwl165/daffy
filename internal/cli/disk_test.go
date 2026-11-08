package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

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
	for _, a := range [][]string{{"--top", "2oops"}, {"--top", "1001"}, {"--json=garbage"}, {"--format", "xml"}, {"--time-budget", "-1s"}} {
		if _, _, e := diskFlags(a); e == nil {
			t.Errorf("accepted %v", a)
		}
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
	d := Deps{Disk: diskFunc(func(_ context.Context, c disk.Config) (disk.Report, error) {
		calls++
		if c.Path != "" {
			t.Errorf("default path = %q", c.Path)
		}
		return disk.Report{}, nil
	})}
	for _, tc := range []struct {
		args []string
		want int
		call bool
	}{{[]string{"disk", "scan"}, 0, true}, {[]string{"disk", "--help"}, 0, false}, {[]string{"disk", "scan", "/home", "--help"}, 0, false}} {
		before := calls
		var out, errout bytes.Buffer
		if got := Run(context.Background(), RunConfig{Args: tc.args, Out: &out, ErrOut: &errout, Deps: d}); got != tc.want {
			t.Fatalf("%v exit %d: %s", tc.args, got, errout.String())
		}
		if (calls > before) != tc.call {
			t.Fatalf("%v calls=%d", tc.args, calls)
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
