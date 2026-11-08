package bazel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
	"github.com/gabriwl165/daffy/internal/plugins"
)

type fakeScanner struct {
	report disk.Report
	err    error
	calls  int
	got    disk.Config
}

func (f *fakeScanner) Scan(_ context.Context, c disk.Config) (disk.Report, error) {
	f.calls++
	f.got = c
	return f.report, f.err
}

func fixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "output")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnalyzeConfigurationAndSuccessfulReport(t *testing.T) {
	path := fixture(t)
	excludes := []string{"one", "two"}
	wantReport := disk.Report{Root: path, Coverage: disk.Coverage{Complete: true}, Totals: disk.Totals{AllocatedBytes: 123}}
	f := &fakeScanner{report: wantReport}
	out, err := New(f).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{Path: path, Excludes: excludes, TopN: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != plugins.StatusComplete || out.Details.Bazel == nil || !reflect.DeepEqual(*out.Details.Bazel.Disk, wantReport) {
		t.Fatalf("unexpected outcome: %#v", out)
	}
	if f.calls != 1 || f.got.Path != path || f.got.TopN != 7 || !reflect.DeepEqual(f.got.Excludes, excludes) {
		t.Fatalf("scanner config/calls: %#v, %d", f.got, f.calls)
	}
	excludes[0] = "mutated"
	if f.got.Excludes[0] != "one" {
		t.Fatal("scanner excludes alias caller slice")
	}
}

func TestAnalyzeRejectsInvalidRequestsWithoutScanning(t *testing.T) {
	dir := fixture(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f := &fakeScanner{}
	tests := []struct {
		name    string
		req     plugins.Request
		invalid bool
	}{
		{"missing path", plugins.Request{}, true},
		{"unknown operation", plugins.Request{Operation: "unknown", Options: plugins.Options{Path: dir}}, false},
		{"negative topn", plugins.Request{Options: plugins.Options{Path: dir, TopN: -1}}, true},
		{"too large topn", plugins.Request{Options: plugins.Options{Path: dir, TopN: maxTopN + 1}}, true},
		{"file root", plugins.Request{Options: plugins.Options{Path: file}}, true},
		{"symlink root", plugins.Request{Options: plugins.Options{Path: link}}, true},
		{"nonexistent root", plugins.Request{Options: plugins.Options{Path: filepath.Join(t.TempDir(), "absent")}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(f).Analyze(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.invalid && !errors.Is(err, plugins.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("scanner called %d times", f.calls)
	}
}

func TestAnalyzeCoverageAndErrorsArePartialAndPreserveReport(t *testing.T) {
	path := fixture(t)
	for _, tt := range []struct {
		name   string
		report disk.Report
		err    error
	}{
		{"skipped entries", disk.Report{Coverage: disk.Coverage{Complete: true, SkippedEntries: 1}}, nil},
		{"issues", disk.Report{Coverage: disk.Coverage{Complete: true}, Issues: []disk.Issue{{Operation: "read"}}}, nil},
		{"omitted errors", disk.Report{Coverage: disk.Coverage{Complete: true, OmittedErrors: 1}}, nil},
		{"incomplete", disk.Report{Coverage: disk.Coverage{Complete: false}}, nil},
		{"scan error", disk.Report{Totals: disk.Totals{AllocatedBytes: 42}}, errors.New("scan failed")},
		{"cancel", disk.Report{Totals: disk.Totals{AllocatedBytes: 42}}, context.Canceled},
		{"deadline", disk.Report{Totals: disk.Totals{AllocatedBytes: 42}}, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeScanner{report: tt.report, err: tt.err}
			out, err := New(f).Analyze(context.Background(), plugins.Request{Options: plugins.Options{Path: path}})
			if (err != nil) != (tt.err != nil) {
				t.Fatalf("error=%v", err)
			}
			if out.Status != plugins.StatusPartial {
				t.Fatalf("status=%s", out.Status)
			}
			if out.Details.Bazel == nil || !reflect.DeepEqual(*out.Details.Bazel.Disk, tt.report) {
				t.Fatalf("lost report: %#v", out)
			}
		})
	}
}

func TestAnalyzeCanceledContextDoesNotScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeScanner{}
	out, err := New(f).Analyze(ctx, plugins.Request{Options: plugins.Options{Path: fixture(t)}})
	if !errors.Is(err, context.Canceled) || out.Status != plugins.StatusPartial || f.calls != 0 {
		t.Fatalf("out=%#v err=%v calls=%d", out, err, f.calls)
	}
}

func TestAnalyzeDeadlineContextDoesNotScan(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	f := &fakeScanner{}
	out, err := New(f).Analyze(ctx, plugins.Request{Options: plugins.Options{Path: fixture(t)}})
	if !errors.Is(err, context.DeadlineExceeded) || out.Status != plugins.StatusPartial || f.calls != 0 {
		t.Fatalf("out=%#v err=%v calls=%d", out, err, f.calls)
	}
}
