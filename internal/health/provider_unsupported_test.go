//go:build !linux && !darwin

package health

import "testing"

func TestUnsupportedProviderUsesExplicitFinding(t *testing.T) {
	r := unsupportedPlatformReport()
	if len(r.Findings) != 1 || r.Findings[0].ID != findingPlatformUnsupported {
		t.Fatalf("unexpected unsupported report: %+v", r)
	}
}
