package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/health"
)

func TestRunConfigVersions(t *testing.T) {
	d := Deps{Collect: func(context.Context, health.Options) health.Report { return health.Report{} }, Doctor: func(context.Context, health.Options) []health.Check { return nil }}
	for _, tc := range []struct{ version, want string }{{"explicit", "explicit"}, {"", "dev"}} {
		var out bytes.Buffer
		code := Run(context.Background(), RunConfig{Args: []string{"--version"}, Out: &out, ErrOut: &bytes.Buffer{}, Deps: d, Version: tc.version})
		if code != 0 || strings.TrimSpace(out.String()) != "daffy "+tc.want {
			t.Fatalf("version %q: code=%d output=%q", tc.version, code, out.String())
		}
	}
}

func TestRunConfigRoutesForwardOptions(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		doctor bool
	}{
		{"scan", []string{"--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged"}, false},
		{"report", []string{"report", "--json", "--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged"}, false},
		{"doctor", []string{"doctor", "--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged"}, true},
		{"watch", []string{"watch", "--json", "--interval", "1s", "--sample", "2s", "--timeout", "4s", "--include-sensitive", "--privileged"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got health.Options
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			collect := func(_ context.Context, _ health.Options) health.Report {
				if tc.name == "watch" {
					cancel()
				}
				return health.Report{}
			}
			d := Deps{Collect: func(ctx context.Context, o health.Options) health.Report { got = o; return collect(ctx, o) }, Doctor: func(_ context.Context, o health.Options) []health.Check { got = o; return nil }}
			var out, err bytes.Buffer
			code := Run(ctx, RunConfig{Args: tc.args, Out: &out, ErrOut: &err, Deps: d})
			if code != 0 && !(tc.name == "watch" && code == 130) {
				t.Fatalf("exit %d: %s", code, err.String())
			}
			if tc.name == "watch" && code != 130 {
				t.Fatalf("watch exit %d", code)
			}
			want := health.Options{SampleInterval: 2 * time.Second, CommandTimeout: 4 * time.Second, IncludeSensitive: true, Privileged: true}
			if got != want {
				t.Fatalf("got options %+v want %+v", got, want)
			}
		})
	}
}

func TestDurationBoundaries(t *testing.T) {
	d := Deps{Collect: func(context.Context, health.Options) health.Report { return health.Report{} }, Doctor: func(context.Context, health.Options) []health.Check { return nil }}
	for _, sample := range []string{"100ms", "10s"} {
		c := Run(context.Background(), RunConfig{Args: []string{"--sample", sample}, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, Deps: d})
		if c != 0 {
			t.Errorf("boundary %s exit %d", sample, c)
		}
	}
	for _, sample := range []string{"99ms", "10001ms"} {
		c := Run(context.Background(), RunConfig{Args: []string{"--sample", sample}, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, Deps: d})
		if c != 2 {
			t.Errorf("invalid boundary %s exit %d", sample, c)
		}
	}
}

func TestSystemInfoHumanOrderAndRequiredBlanks(t *testing.T) {
	var b bytes.Buffer
	r := health.Report{System: health.SystemInfo{OS: "Linux", Kernel: "K", DistroID: "id", DistroAdapter: "adapter", CPUModel: "cpu", DistroVersion: "v"}}
	if err := printReport(&b, r); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	keys := []string{"cpu_model: cpu", "distro_adapter: adapter", "distro_id: id", "distro_version: v", "kernel: K", "os: Linux"}
	prev := -1
	for _, k := range keys {
		i := strings.Index(s, k)
		if i <= prev {
			t.Fatalf("metadata order %q in %q", k, s)
		}
		prev = i
	}
	b.Reset()
	r.System = health.SystemInfo{OS: "Linux"}
	if err := printReport(&b, r); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"distro_adapter: \n", "distro_id: \n", "kernel: \n", "os: Linux\n"} {
		if !strings.Contains(b.String(), k) {
			t.Errorf("missing required metadata %q in %q", k, b.String())
		}
	}
}

func TestSeverityRankingAndStableTieOrdering(t *testing.T) {
	for _, tc := range []struct {
		s    health.Severity
		rank int
	}{{"CRITICAL", 0}, {"Warning", 1}, {"iNfO", 2}, {"OK", 3}, {"other", 4}} {
		if got := severityRank(tc.s); got != tc.rank {
			t.Errorf("rank %q=%d want %d", tc.s, got, tc.rank)
		}
	}
	r := health.Report{Findings: []health.Finding{{ID: "same", Area: health.AreaCPU, Severity: "unknown"}, {ID: "same", Area: health.AreaDisk, Severity: "UNKNOWN"}, {ID: "z", Severity: "critical"}, {ID: "a", Severity: "Critical"}}}
	sortReport(&r)
	want := []string{"a", "z", string(health.AreaCPU), string(health.AreaDisk)}
	for i, w := range want {
		got := r.Findings[i].ID
		if i > 1 {
			got = string(r.Findings[i].Area)
		}
		if got != w {
			t.Fatalf("order[%d]=%q want %q", i, got, w)
		}
	}
}

func TestJSONSystemInfoOptionalKeys(t *testing.T) {
	b, e := json.Marshal(health.Report{System: health.SystemInfo{OS: "Linux"}})
	if e != nil {
		t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(b, &obj); e != nil {
		t.Fatal(e)
	}
	var sys map[string]json.RawMessage
	if e = json.Unmarshal(obj["system"], &sys); e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"os", "kernel", "distro_id", "distro_adapter"} {
		if _, ok := sys[k]; !ok {
			t.Errorf("required key %s absent", k)
		}
	}
	for _, k := range []string{"cpu_model", "distro_version"} {
		if _, ok := sys[k]; ok {
			t.Errorf("optional empty key %s present", k)
		}
	}
}

func TestWatchNDJSONTwoNonoverlappingRunsThenCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var active, calls atomic.Int32
	d := Deps{Doctor: func(context.Context, health.Options) []health.Check { return nil }, Collect: func(context.Context, health.Options) health.Report {
		if active.Add(1) != 1 {
			t.Error("overlapping collection")
		}
		n := calls.Add(1)
		active.Add(-1)
		if n == 3 {
			cancel()
		}
		return health.Report{SchemaVersion: "watch"}
	}}
	var out, err bytes.Buffer
	code := Run(ctx, RunConfig{Args: []string{"watch", "--json", "--interval", "1ms"}, Out: &out, ErrOut: &err, Deps: d})
	if code != 130 {
		t.Fatalf("exit %d: %s", code, err.String())
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("records=%d output=%q", len(lines), out.String())
	}
	for _, line := range lines {
		var r health.Report
		if e := json.Unmarshal([]byte(line), &r); e != nil || r.SchemaVersion != "watch" {
			t.Fatalf("bad NDJSON %q: %v", line, e)
		}
	}
}
