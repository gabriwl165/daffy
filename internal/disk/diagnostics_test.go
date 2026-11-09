package disk

import "testing"

func TestAnalyzeDiagnosticsThresholdCases(t *testing.T) {
	tests := []struct {
		name             string
		total, available uint64
		want             DiagnosticSeverity
	}{
		{"one byte full", 1, 1, ""},
		{"one byte empty", 1, 0, SeverityCritical},
		{"exact ten percent", 100, 10, SeverityWarning},
		{"above ten percent", 100, 11, ""},
		{"exact five percent", 100, 5, SeverityCritical},
		{"indivisible above five percent", 101, 6, SeverityWarning},
		{"maximum near warning", ^uint64(0), ^uint64(0) / 10, SeverityWarning},
		{"maximum near critical", ^uint64(0), ^uint64(0) / 20, SeverityCritical},
		{"free exceeds total", 100, 101, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := AnalyzeDiagnostics(Report{Coverage: Coverage{Complete: true}, Mounts: []MountCapacity{{Path: "/m", TotalBytes: tt.total, AvailableBytes: tt.available}}})
			if tt.want == "" {
				if len(ds) != 0 {
					t.Fatalf("got %#v", ds)
				}
				return
			}
			if len(ds) != 1 || ds[0].Severity != tt.want {
				t.Fatalf("got %#v, want %s", ds, tt.want)
			}
		})
	}
}

func TestAnalyzeDiagnosticsInodeCases(t *testing.T) {
	tests := []struct {
		name        string
		total, free uint64
		want        DiagnosticSeverity
	}{
		{"warning", 100, 10, SeverityWarning}, {"above warning", 100, 11, ""},
		{"critical", 100, 5, SeverityCritical}, {"small total", 1, 1, ""},
		{"invalid free", 100, 101, ""}, {"unknown", 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := AnalyzeDiagnostics(Report{Coverage: Coverage{Complete: true}, Mounts: []MountCapacity{{Path: "/m", TotalInodes: tt.total, FreeInodes: tt.free}}})
			if tt.want == "" {
				if len(ds) != 0 {
					t.Fatalf("got %#v", ds)
				}
				return
			}
			if len(ds) != 1 || ds[0].Code != DiagnosticLowInodes || ds[0].Severity != tt.want {
				t.Fatalf("got %#v", ds)
			}
		})
	}
}

func TestAnalyzeDiagnosticsBoundariesAndScope(t *testing.T) {
	r := Report{Root: "/data/sub", Coverage: Coverage{Complete: true}, Mounts: []MountCapacity{
		{Path: "/data", TotalBytes: 1000, AvailableBytes: 100},
		{Path: "/critical", TotalBytes: 1000, AvailableBytes: 50, TotalInodes: 100, FreeInodes: 5},
		{Path: "/edge", TotalBytes: 1000, AvailableBytes: 101},
		{Path: "/unknown", TotalBytes: 0, AvailableBytes: 0, TotalInodes: 0, FreeInodes: 0},
	}}
	ds := AnalyzeDiagnostics(r)
	if len(ds) != 3 {
		t.Fatalf("diagnostics = %#v", ds)
	}
	if ds[0].Code != DiagnosticLowCapacity || ds[0].Severity != SeverityWarning || ds[0].Mount != "/data" || ds[0].Path != "" {
		t.Fatalf("scoped capacity diagnostic: %#v", ds[0])
	}
	if ds[1].Severity != SeverityCritical || ds[2].Code != DiagnosticLowInodes || ds[2].Severity != SeverityCritical {
		t.Fatalf("critical thresholds: %#v", ds)
	}
}

func TestAnalyzeDiagnosticsCoverageAndSkipped(t *testing.T) {
	r := Report{Root: "/chosen", Coverage: Coverage{Complete: false}, Issues: []Issue{{Path: "/other", Operation: OperationMountSkip, Error: string(SkipExcludedMount)}}}
	ds := AnalyzeDiagnostics(r)
	if len(ds) != 2 || ds[0].Code != DiagnosticIncompleteCoverage || ds[0].Path != "/chosen" || ds[1].Code != DiagnosticSkippedMount || ds[1].Mount != "/other" {
		t.Fatalf("diagnostics: %#v", ds)
	}
}

func TestCompleteCoverageWithIntentionalSkipIsInfoOnly(t *testing.T) {
	ds := AnalyzeDiagnostics(Report{Coverage: Coverage{Complete: true}, Issues: []Issue{{Path: "/excluded", Operation: OperationExclude, Error: "excluded"}}})
	if len(ds) != 1 || ds[0].Code != DiagnosticSkippedMount || ds[0].Severity != SeverityInfo {
		t.Fatalf("got %#v", ds)
	}
}
