package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/health"
	"github.com/gabriwl165/daffy/internal/plugins"
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

func TestBuiltinAliasesMatchPluginRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   plugins.PluginID
		ops  []plugins.Operation
		args []string
	}{
		{"docker default", plugins.PluginDocker, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{}), op(plugins.OperationRuntime, true, plugins.SupportedOptions{})}, nil},
		{"docker explicit", plugins.PluginDocker, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{}), op(plugins.OperationRuntime, false, plugins.SupportedOptions{})}, []string{"runtime"}},
		{"docker runtime flags alias", plugins.PluginDocker, []plugins.Operation{op(plugins.OperationRuntime, false, plugins.SupportedOptions{RuntimeTrends: true})}, []string{"runtime", "--samples=2", "--interval=100ms", "--runtime-top=3", "--cpu-threshold=125", "--memory-threshold=80"}},
		{"bazel default", plugins.PluginBazel, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Path: true, TopN: true})}, []string{"--path=/tmp", "--top=7"}},
		{"bazel explicit", plugins.PluginBazel, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Path: true, TopN: true})}, []string{"disk", "--path=/tmp", "--top=7"}},
		{"docker invalid options", plugins.PluginDocker, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{})}, []string{"--top=7"}},
		{"bazel invalid options", plugins.PluginBazel, []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{})}, []string{"disk", "--path=/tmp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(prefix []string) (int, string, string, []plugins.Request) {
				p := &fakePlugin{descriptor: plugins.Descriptor{ID: tc.id, Operations: tc.ops}}
				var out, errOut bytes.Buffer
				args := append(prefix, tc.args...)
				code := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
				return code, out.String(), errOut.String(), p.calls
			}
			legacy := []string{"plugins", "run", string(tc.id)}
			alias := []string{string(tc.id)}
			oldCode, oldOut, oldErr, oldCalls := run(legacy)
			newCode, newOut, newErr, newCalls := run(alias)
			if oldCode != newCode || oldOut != newOut || oldErr != newErr || !reflect.DeepEqual(oldCalls, newCalls) {
				t.Fatalf("alias mismatch: old=(%d,%q,%q,%+v) new=(%d,%q,%q,%+v)", oldCode, oldOut, oldErr, oldCalls, newCode, newOut, newErr, newCalls)
			}
		})
	}
}
func run(args []string, d Deps) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &err, Deps: d})
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
	if Run(context.Background(), RunConfig{Args: []string{"--version"}, Out: &b, ErrOut: &b, Deps: d, Version: "v1.2"}) != 0 || !strings.Contains(b.String(), "v1.2") {
		t.Fatal("version")
	}
}

func TestBuiltInAliasHelpAndDispatchErrors(t *testing.T) {
	d, _, _ := fixtures()
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"--help"}, "daffy docker [disk|runtime|config]"}, {[]string{"docker", "--help"}, "Usage: daffy docker"}, {[]string{"bazel", "--help"}, "Usage: daffy bazel"}} {
		code, out, _ := run(tc.args, d)
		if code != 0 || !strings.Contains(out, tc.want) {
			t.Errorf("%v help: exit %d output %q", tc.args, code, out)
		}
	}
	for _, item := range []struct {
		args []string
		id   plugins.PluginID
	}{{[]string{"docker", "unknown"}, plugins.PluginDocker}, {[]string{"bazel", "unknown"}, plugins.PluginBazel}} {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: item.id, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{})}}}
		d.Plugins = fakeRegistry(t, p)
		code, _, errOut := run(item.args, d)
		if code != 2 || !strings.Contains(errOut, "unknown operation") {
			t.Errorf("%v dispatch: %d %q", item.args, code, errOut)
		}
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
	if c := Run(context.Background(), RunConfig{Args: []string{"doctor", "--json"}, Out: bad, ErrOut: ioDiscard{}, Deps: d}); c != 1 {
		t.Fatalf("doctor writer exit %d", c)
	}
	if c := Run(context.Background(), RunConfig{Out: bad, ErrOut: ioDiscard{}, Deps: d}); c != 1 {
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
	code := Run(ctx, RunConfig{Args: []string{"watch", "--json", "--interval", "1s"}, Out: w, ErrOut: &err, Deps: d})
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
