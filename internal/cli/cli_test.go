package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy-duck/internal/health"
)

func fixtures() (Deps, *health.Options, *int) {
	got := &health.Options{}
	n := 0
	d := Deps{Collect: func(_ context.Context, o health.Options) health.Report {
		*got = o
		n++
		return health.Report{SchemaVersion: "1", Findings: []health.Finding{{ID: "z", Severity: "warning"}, {ID: "c", Severity: "critical"}}}
	}, Doctor: func(_ context.Context, o health.Options) []health.Check {
		*got = o
		return []health.Check{{Name: "proc", Available: true}}
	}}
	return d, got, &n
}
func run(args []string, d Deps) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(context.Background(), args, &out, &err, d)
	return code, out.String(), err.String()
}
func TestRoutesDefaultsOptions(t *testing.T) {
	for _, cmd := range []string{"scan", "", "report --json", "doctor"} {
		d, p, n := fixtures()
		a := strings.Fields(cmd)
		if len(a) > 0 && a[0] == "report" {
			a = append(a, "--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged")
		}
		code, _, _ := run(a, d)
		if code != 0 {
			t.Fatalf("%q exit %d", cmd, code)
		}
		if cmd == "doctor" {
			if p.SampleInterval != time.Second || p.CommandTimeout != 3*time.Second {
				t.Fatalf("defaults: %+v", p)
			}
		} else if *n != 1 {
			t.Fatalf("%q not collected", cmd)
		}
	}
	d, p, _ := fixtures()
	code, _, _ := run([]string{"--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged"}, d)
	if code != 0 || p.SampleInterval != 2*time.Second || p.CommandTimeout != 4*time.Second || !p.IncludeSensitive || !p.Privileged {
		t.Fatalf("options: %d %+v", code, p)
	}
}
func TestHelpErrorsAndVersion(t *testing.T) {
	d, _, _ := fixtures()
	for _, a := range [][]string{{"--help"}, {"scan", "-h"}} {
		code, out, _ := run(a, d)
		if code != 0 || !strings.Contains(out, "Usage:") {
			t.Fatalf("help %v: %d %s", a, code, out)
		}
	}
	for _, a := range [][]string{{"wat"}, {"--nope"}, {"--sample", "nonsense"}, {"--sample", "1ms"}, {"--timeout", "0s"}, {"--interval", "-1s"}, {"report"}} {
		code, _, _ := run(a, d)
		if code != 2 {
			t.Errorf("%v exit %d, want 2", a, code)
		}
	}
	var b bytes.Buffer
	if RunVersion(context.Background(), []string{"--version"}, &b, &b, d, "v1.2") != 0 || !strings.Contains(b.String(), "v1.2") {
		t.Fatal("version")
	}
}
func TestJSONSchemaCleanAndSorted(t *testing.T) {
	d, _, _ := fixtures()
	code, out, _ := run([]string{"--json"}, d)
	if code != 0 {
		t.Fatal(code)
	}
	var r health.Report
	if e := json.Unmarshal([]byte(out), &r); e != nil {
		t.Fatalf("not clean JSON: %q: %v", out, e)
	}
	if r.SchemaVersion != "1" || len(r.Findings) != 2 || r.Findings[0].Severity != "critical" {
		t.Fatalf("schema/order: %+v", r)
	}
}
func TestReportJsonAndDoctorOutputErrors(t *testing.T) {
	d, _, _ := fixtures()
	if c, _, _ := run([]string{"report"}, d); c != 2 {
		t.Fatal(c)
	}
	bad := failWriter{}
	if c := Run(context.Background(), []string{"doctor", "--json"}, bad, ioDiscard{}, d); c != 1 {
		t.Fatalf("doctor writer exit %d", c)
	}
	if c := Run(context.Background(), nil, bad, ioDiscard{}, d); c != 1 {
		t.Fatalf("scan writer exit %d", c)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken") }

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
func TestWatchNDJSONCancellation(t *testing.T) {
	d, _, n := fixtures()
	ctx, cancel := context.WithCancel(context.Background())
	var out, err bytes.Buffer
	w := cancelWriter{w: &out, cancel: cancel}
	d.Collect = func(ctx context.Context, o health.Options) health.Report {
		return health.Report{SchemaVersion: "watch"}
	}
	code := Run(ctx, []string{"watch", "--json", "--interval", "1s"}, w, &err, d)
	if code != 130 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one NDJSON record: %q (%d)", out.String(), *n)
	}
	var r health.Report
	if json.Unmarshal([]byte(lines[0]), &r) != nil || r.SchemaVersion != "watch" {
		t.Fatalf("invalid NDJSON %q", out.String())
	}
}

type cancelWriter struct {
	w      *bytes.Buffer
	cancel context.CancelFunc
}

func (w cancelWriter) Write(p []byte) (int, error) { n, e := w.w.Write(p); w.cancel(); return n, e }
func TestSanitization(t *testing.T) {
	var b bytes.Buffer
	r := health.Report{Findings: []health.Finding{{Severity: "warning\n[CRITICAL] forged", Summary: "x\x1b[2J\x07\u202eInjected", Evidence: []string{"line\nbreak"}}}}
	if e := printReport(&b, r); e != nil {
		t.Fatal(e)
	}
	s := b.String()
	if strings.ContainsAny(s, "\x1b\x07\n[CRITICAL] forged") {
		if strings.Contains(s, "\x1b") || strings.Contains(s, "\x07") || strings.Contains(s, "[CRITICAL] forged") {
			t.Fatalf("unsafe output %q", s)
		}
	}
	if strings.Contains(s, "line\nbreak") || strings.Contains(s, "\u202e") {
		t.Fatalf("multiline/format control not stripped: %q", s)
	}
	var j bytes.Buffer
	_ = json.NewEncoder(&j).Encode(health.Report{System: health.SystemInfo{OS: "\x1b[2J"}})
	if !strings.Contains(j.String(), `\u001b`) {
		t.Fatalf("json control not escaped: %q", j.String())
	}
}
