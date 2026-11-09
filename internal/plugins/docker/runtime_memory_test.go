package docker

import (
	"context"
	"errors"
	"github.com/gabriwl165/daffy/internal/plugins"
	"strings"
	"testing"
	"time"
)

func TestParseMemoryUnits(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  uint64
		ok    bool
	}{
		{"1.5kB", 1500, true}, {"1KB", 1000, true}, {"2 KiB", 2048, true}, {"2 MiB", 2 << 20, true}, {"0B", 0, true},
		{"", 0, false}, {"-1MB", 0, false}, {"NaNMB", 0, false}, {"1e300TB", 0, false}, {"18446744073709551616B", 0, false},
	} {
		got, ok := parseBytes(tc.input)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseBytes(%q) = %d,%v; want %d,%v", tc.input, got, ok, tc.want, tc.ok)
		}
	}
	if _, _, ok := parseMemUsage("12MB"); ok {
		t.Fatal("missing total accepted")
	}
	if _, _, ok := parseMemUsage("12MB / 20MB"); !ok {
		t.Fatal("valid usage rejected")
	}
}

func TestSignedGrowthOverflowIsUnknown(t *testing.T) {
	if _, ok := signedDelta(^uint64(0), 0); ok {
		t.Fatal("overflowing growth reported")
	}
	if got, ok := signedDelta(2, 1); !ok || got != 1 {
		t.Fatalf("signedDelta = %d,%v", got, ok)
	}
}

func TestPressureNeedsRepeatedValidHighSamples(t *testing.T) {
	var acc []memoryAccumulator
	row := plugins.DockerContainerStats{ID: "pressure", Name: "p", MemUsage: "1MiB / 4MiB", MemPerc: "85%"}
	recordMemory(&acc, []plugins.DockerContainerStats{row}, 80)
	row.MemPerc = "NaN%"
	recordMemory(&acc, []plugins.DockerContainerStats{row}, 80)
	got := finishMemory(acc)[0]
	if got.PressureObservationCount != 1 || got.PressureSampleCount != 1 || got.SustainedPressure {
		t.Fatalf("brief pressure: %+v", got)
	}
	row.MemPerc = "85%"
	recordMemory(&acc, []plugins.DockerContainerStats{row}, 80)
	got = finishMemory(acc)[0]
	if !got.SustainedPressure || got.PressureThresholdPercent != 80 {
		t.Fatalf("sustained pressure: %+v", got)
	}
}

func TestMemoryAccumulatorTracksUsageNotLimit(t *testing.T) {
	var items []memoryAccumulator
	rows := []plugins.DockerContainerStats{{ID: "a", Name: "a", MemUsage: "40MB / 1GB"}}
	recordMemory(&items, rows, 80)
	rows[0].MemUsage = "60MB / 1GB"
	recordMemory(&items, rows, 80)
	got := finishMemory(items)[0]
	if *got.PeakBytes != 60_000_000 || *got.CurrentBytes != 60_000_000 || *got.FirstBytes != 40_000_000 || !got.RisingUsage {
		t.Fatalf("unexpected diagnostic: %+v", got)
	}
}

func TestMemoryDiagnosticsCorrelateAllowlistedInspect(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, inspect, usage string
		denominator          plugins.DockerMemoryDenominator
		limit                uint64
		oom                  bool
		partial              bool
	}{
		{"limited", `[{"Id":"` + id + `","HostConfig":{"Memory":100000000},"State":{"OOMKilled":true}}]`, "75MB / 1GB", plugins.DockerMemoryDenominatorContainerLimit, 100000000, true, false},
		{"over limit", `[{"Id":"` + id + `","HostConfig":{"Memory":100000000},"State":{"OOMKilled":false}}]`, "150MB / 1GB", plugins.DockerMemoryDenominatorContainerLimit, 100000000, false, false},
		{"unlimited", `[{"Id":"` + id + `","HostConfig":{"Memory":0},"State":{"OOMKilled":false}}]`, "75MB / 1GB", plugins.DockerMemoryDenominatorDaemonHost, 0, false, false},
		{"unknown", `[{"Id":"` + id + `","HostConfig":{},"State":{}}]`, "75MB / 1GB", plugins.DockerMemoryDenominatorUnknown, 0, false, false},
		{"missing inspect", `[]`, "75MB / 1GB", plugins.DockerMemoryDenominatorUnknown, 0, false, true},
		{"wrong id", `[{"Id":"` + strings.Repeat("b", 64) + `","HostConfig":{"Memory":100}}]`, "75MB / 1GB", plugins.DockerMemoryDenominatorUnknown, 0, false, true},
		{"duplicate id", `[{"Id":"` + id + `"},{"Id":"` + id + `"}]`, "75MB / 1GB", plugins.DockerMemoryDenominatorUnknown, 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := []byte(`{"ID":"` + id + `","Name":"svc","CPUPerc":"1%","MemUsage":"` + tc.usage + `","MemPerc":"85%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`)
			r := &fakeRunner{outputs: [][]byte{stats, []byte(tc.inspect)}}
			o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{MemoryDetails: true}})
			if (err != nil) != tc.partial {
				t.Fatalf("err=%v outcome=%+v", err, o)
			}
			m := o.Details.Docker.Memory
			if len(m) != 1 {
				t.Fatalf("memory: %+v", m)
			}
			if m[0].Denominator != tc.denominator {
				t.Fatalf("denominator: %+v", m[0])
			}
			wantHeadroom := uint64(25000000)
			if tc.name == "over limit" {
				wantHeadroom = 0
			}
			if tc.limit > 0 && (m[0].LimitBytes == nil || *m[0].LimitBytes != tc.limit || m[0].HeadroomBytes == nil || *m[0].HeadroomBytes != wantHeadroom) {
				t.Fatalf("limit/headroom: %+v", m[0])
			}
			if tc.name == "limited" && (m[0].OOMKilled == nil || !*m[0].OOMKilled) {
				t.Fatalf("OOM: %+v", m[0])
			}
		})
	}
}

func TestMemorySortingUsesAbsoluteBytes(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	row := func(id, usage, perc string) []byte {
		return []byte(`{"ID":"` + id + `","Name":"x","CPUPerc":"1%","MemUsage":"` + usage + `","MemPerc":"` + perc + `","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`)
	}
	response := append(row(a, "900MB / 1GB", "90%"), '\n')
	response = append(response, row(b, "2GB / 4GB", "50%")...)
	o, err := New(&fakeRunner{outputs: [][]byte{response}}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{RuntimeSort: plugins.DockerRuntimeSortMemory}})
	if err != nil || len(o.Details.Docker.RuntimeTrend.Containers) != 2 || o.Details.Docker.RuntimeTrend.Containers[0].ID != b {
		t.Fatalf("absolute sort: %+v %v", o, err)
	}
}

func TestMemoryCancellationPreservesCompletedSample(t *testing.T) {
	id := strings.Repeat("c", 64)
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stats := []byte(`{"ID":"` + id + `","Name":"svc","CPUPerc":"1%","MemUsage":"10MiB / 20MiB","MemPerc":"50%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`)
	inspect := []byte(`[{"Id":"` + id + `","HostConfig":{"Memory":20971520},"State":{"OOMKilled":false}}]`)
	calls := 0
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Contains(strings.Join(args, " "), "stats") {
			return stats, nil
		}
		cancel()
		return inspect, nil
	})
	o, err := New(runner).Analyze(cancelCtx, plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{MemoryDetails: true, Samples: 2, Interval: 100 * time.Millisecond}})
	if !errors.Is(err, context.Canceled) || o.Details.Docker.RuntimeTrend == nil || o.Details.Docker.RuntimeTrend.SampleCount != 1 || len(o.Details.Docker.Memory) != 1 || o.Details.Docker.Memory[0].SampleCount != 1 {
		t.Fatalf("cancelled sample lost: calls=%d out=%+v err=%v", calls, o, err)
	}
}

func TestMemoryInspectHonorsContextAndSelectors(t *testing.T) {
	id := strings.Repeat("d", 64)
	rows := [][]byte{[]byte(`{"ID":"` + id + `","Names":"api"}`), []byte(id), []byte(`{"ID":"` + id + `","Name":"api","CPUPerc":"1%","MemUsage":"1MiB / 2MiB","MemPerc":"50%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`), []byte(`[{"Id":"` + id + `","HostConfig":{"Memory":2097152},"State":{"OOMKilled":false}}]`)}
	r := &fakeRunner{outputs: rows}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Context: "dev", Containers: []string{"api"}, MemoryDetails: true}})
	if err != nil || len(o.Details.Docker.Memory) != 1 || len(r.args) != 4 {
		t.Fatalf("target memory: %+v err=%v calls=%#v", o, err, r.args)
	}
	for _, args := range r.args {
		if !containsArg(args, "--context") || !containsArg(args, "dev") {
			t.Fatalf("context not carried to command: %#v", r.args)
		}
	}
}

func TestMemoryInspectHonorsProjectSelection(t *testing.T) {
	id := strings.Repeat("e", 64)
	config := `[{"Id":"` + id + `","Name":"/api","State":{"Status":"running","OOMKilled":false},"Config":{"Labels":{"com.docker.compose.project":"shop"}},"HostConfig":{"Memory":2097152,"Privileged":false}}]`
	stats := `{"ID":"` + id + `","Name":"api","CPUPerc":"1%","MemUsage":"1MiB / 2MiB","MemPerc":"50%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`
	listed := []byte(`{"ID":"` + id + `","Names":"api"}`)
	r := &fakeRunner{outputs: [][]byte{listed, listed, []byte(config), []byte(id), []byte(stats), []byte(config)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Project: "shop", MemoryDetails: true}})
	if err != nil || len(o.Details.Docker.Memory) != 1 || o.Details.Docker.Memory[0].ID != id {
		t.Fatalf("project memory: %+v err=%v", o, err)
	}
}
