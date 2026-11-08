package disk

// DiagnosticSeverity describes the urgency of a finding.
type DiagnosticSeverity string

const (
	SeverityInfo     DiagnosticSeverity = "info"
	SeverityWarning  DiagnosticSeverity = "warning"
	SeverityCritical DiagnosticSeverity = "critical"
)

// DiagnosticCode identifies a diagnostic category.
type DiagnosticCode string

const (
	DiagnosticLowCapacity        DiagnosticCode = "low-capacity"
	DiagnosticLowInodes          DiagnosticCode = "low-inodes"
	DiagnosticIncompleteCoverage DiagnosticCode = "incomplete-coverage"
	DiagnosticSkippedMount       DiagnosticCode = "skipped-mount"
)

type Diagnostic struct {
	Severity       DiagnosticSeverity `json:"severity"`
	Code           DiagnosticCode     `json:"code"`
	Mount          string             `json:"mount,omitempty"`
	Path           string             `json:"path,omitempty"`
	Message        string             `json:"message"`
	Recommendation string             `json:"recommendation,omitempty"`
}

func atMostFraction(value, total, denominator uint64) bool {
	return value <= total/denominator
}

// AnalyzeDiagnostics derives findings without I/O in O(mounts + issues) time.
func AnalyzeDiagnostics(report Report) []Diagnostic {
	var out []Diagnostic
	for _, m := range report.Mounts {
		if m.TotalBytes > 0 && m.AvailableBytes <= m.TotalBytes {
			// Multiplication-free threshold comparisons avoid uint overflow.
			severity := DiagnosticSeverity("")
			if atMostFraction(m.AvailableBytes, m.TotalBytes, 20) {
				severity = SeverityCritical
			} else if atMostFraction(m.AvailableBytes, m.TotalBytes, 10) {
				severity = SeverityWarning
			}
			if severity != "" {
				out = append(out, Diagnostic{Severity: severity, Code: DiagnosticLowCapacity, Mount: m.Path, Message: "Available filesystem capacity is low; capacity is filesystem-wide, not limited to the selected directory.", Recommendation: "Review the largest directories and cleanup advice; rescan a narrower path to investigate before removing anything."})
			}
		}
		if m.TotalInodes > 0 && m.FreeInodes <= m.TotalInodes {
			severity := DiagnosticSeverity("")
			if atMostFraction(m.FreeInodes, m.TotalInodes, 20) {
				severity = SeverityCritical
			} else if atMostFraction(m.FreeInodes, m.TotalInodes, 10) {
				severity = SeverityWarning
			}
			if severity != "" {
				out = append(out, Diagnostic{Severity: severity, Code: DiagnosticLowInodes, Mount: m.Path, Message: "Available filesystem inodes are low; new files may fail even with bytes available.", Recommendation: "Investigate directories containing many small files, such as caches and build output; removing one large file may not resolve inode pressure."})
			}
		}
	}
	if !report.Coverage.Complete {
		out = append(out, Diagnostic{Severity: SeverityWarning, Code: DiagnosticIncompleteCoverage, Path: report.Root, Message: "Scan coverage is incomplete; results may omit filesystem entries.", Recommendation: "Review scan issues and exclusions before drawing conclusions."})
	}
	for _, issue := range report.Issues {
		if issue.Operation == OperationMountSkip || issue.Operation == OperationExclude {
			out = append(out, Diagnostic{Severity: SeverityInfo, Code: DiagnosticSkippedMount, Mount: issue.Path, Path: issue.Path, Message: "Mount or path was skipped: " + issue.Error, Recommendation: "Review the scan scope if this mount or path should be included."})
		}
	}
	return out
}
