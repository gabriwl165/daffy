package health

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPressureParser(t *testing.T) {
	cases := []struct {
		input       string
		valid, high bool
		evidence    []string
	}{
		{"some avg10=19.99 avg60=0.00 avg300=0.00 total=1\nfull avg10=20.00 avg60=0 avg300=0 total=2\n", true, true, []string{"some avg10=19.99 avg60=0.00 avg300=0.00 total=1", "full avg10=20.00 avg60=0 avg300=0 total=2"}},
		{" some   avg10=0 avg60=0 avg300=0 total=1 \n\n", true, false, []string{"some avg10=0 avg60=0 avg300=0 total=1"}},
		{"some avg10=1 avg10=101 avg60=0 avg300=0 total=1", false, false, []string{"some avg10=1 avg10=101 avg60=0 avg300=0 total=1"}},
		{"some avg10=NaN avg60=0 avg300=0 total=1", false, false, []string{"some avg10=NaN avg60=0 avg300=0 total=1"}},
		{"some avg10=Inf avg60=0 avg300=0 total=1", false, false, []string{"some avg10=Inf avg60=0 avg300=0 total=1"}},
		{"some avg10=-1 avg60=0 avg300=0 total=1", false, false, []string{"some avg10=-1 avg60=0 avg300=0 total=1"}},
		{"some avg10=101 avg60=0 avg300=0 total=1", false, false, []string{"some avg10=101 avg60=0 avg300=0 total=1"}},
		{"some avg10=0 avg60=0", false, false, nil},
		{"", false, false, nil}, {"  \n\t", false, false, nil},
		{"neither avg10=0 avg60=0 avg300=0", false, false, nil},
		{"neither avg10=99 avg60=0 avg300=0", false, false, nil},
		{"some avg10=20", false, false, nil},
		{"some avg10=0 avg10=30 avg60=0 avg300=0 total=1 extra", true, true, []string{"some avg10=0 avg10=30 avg60=0 avg300=0 total=1 extra"}},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(len(tc.input)), func(t *testing.T) {
			got := parsePressure(tc.input)
			if got.valid != tc.valid || got.high != tc.high || !equalStrings(got.evidence, tc.evidence) {
				t.Fatalf("parsePressure(%q) = %#v", tc.input, got)
			}
			baseline := parsePressureBaseline(tc.input)
			if got.valid != baseline.valid || got.high != baseline.high || !equalStrings(got.evidence, baseline.evidence) {
				t.Fatalf("baseline mismatch for %q: got %#v, baseline %#v", tc.input, got, baseline)
			}
		})
	}
}

func TestCorePressureResources(t *testing.T) {
	proc := t.TempDir()
	for _, resource := range pressureResources {
		path := filepath.Join(proc, pressureDirectory, resource.name())
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("some avg10=20 avg60=0 avg300=0 total=1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var findings []Finding
	corePressure(proc, func(f Finding) { findings = append(findings, f) })
	if len(findings) != len(pressureResources) {
		t.Fatalf("got %d findings", len(findings))
	}
	for i, resource := range pressureResources {
		if findings[i].ID != resource.findingID || findings[i].Severity != SeverityWarning || len(findings[i].Evidence) != 1 {
			t.Errorf("finding %d: %#v", i, findings[i])
		}
	}
	if err := os.Remove(filepath.Join(proc, pressureDirectory, pressureResources[2].name())); err != nil {
		t.Fatal(err)
	}
	findings = nil
	corePressure(proc, func(f Finding) { findings = append(findings, f) })
	if findings[2].Severity != SeverityUnknown || findings[2].Summary != "io pressure unavailable" {
		t.Errorf("missing resource: %#v", findings[2])
	}
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parsePressureBaseline(input string) pressureResult {
	values := []string{}
	high, valid := false, true
	for _, line := range strings.Split(strings.TrimSpace(input), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || (f[0] != "some" && f[0] != "full") {
			valid = false
			break
		}
		found := false
		for _, x := range f[1:] {
			k, v, ok := strings.Cut(x, "=")
			if ok && k == "avg10" {
				n, e := strconv.ParseFloat(v, 64)
				if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
					valid = false
				} else if n >= 20 {
					high = true
				}
				found = true
			}
		}
		if !found {
			valid = false
		}
		values = append(values, strings.Join(f, " "))
	}
	return pressureResult{valid: valid && len(values) > 0, high: high, evidence: values}
}

var pressureBenchResult pressureResult

func BenchmarkPressure(b *testing.B) {
	cases := map[string]string{
		"cpu":             "some avg10=0.04 avg60=0.03 avg300=0.01 total=123456\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"memory":          "some avg10=1.20 avg60=0.90 avg300=0.30 total=1234567\nfull avg10=0.10 avg60=0.05 avg300=0.01 total=1234\n",
		"synthetic-large": strings.Repeat("some avg10=19.99 avg60=10.25 avg300=4.00 total=123456789\n", 512),
	}
	for name, input := range cases {
		for _, impl := range []struct {
			name string
			fn   func(string) pressureResult
		}{{"baseline", parsePressureBaseline}, {"optimized", parsePressure}} {
			b.Run(name+"/"+impl.name, func(b *testing.B) {
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					pressureBenchResult = impl.fn(input)
				}
			})
		}
	}
}
