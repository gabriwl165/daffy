package docker

import "testing"

func TestParseMemoryUnits(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  uint64
		ok    bool
	}{
		{"1.5kB", 1536, true}, {"2 MiB", 2 << 20, true}, {"0B", 0, true},
		{"", 0, false}, {"-1MB", 0, false}, {"NaNMB", 0, false}, {"1e300TB", 0, false},
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
