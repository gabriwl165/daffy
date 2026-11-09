package docker

import (
	"github.com/gabriwl165/daffy/internal/plugins"
	"testing"
)

func TestParseMemoryUnits(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  uint64
		ok    bool
	}{
		{"1.5kB", 1500, true}, {"2 KiB", 2048, true}, {"2 MiB", 2 << 20, true}, {"0B", 0, true},
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

func TestMemoryAccumulatorTracksUsageNotLimit(t *testing.T) {
	var items []memoryAccumulator
	rows := []plugins.DockerContainerStats{{ID: "a", Name: "a", MemUsage: "40MB / 1GB"}}
	recordMemory(&items, rows)
	rows[0].MemUsage = "60MB / 1GB"
	recordMemory(&items, rows)
	got := finishMemory(items)[0]
	if *got.PeakBytes != 60_000_000 || *got.CurrentBytes != 60_000_000 || *got.FirstBytes != 40_000_000 || !got.RisingUsage {
		t.Fatalf("unexpected diagnostic: %+v", got)
	}
}
