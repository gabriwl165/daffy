package health

import "testing"

func TestSupportedPlatformAndDoctor(t *testing.T) {
	checks := Doctor(nil, Options{})
	if len(checks) == 0 {
		t.Fatal("doctor returned no platform status")
	}
	if checks[0].Available != SupportedPlatform() {
		t.Fatalf("support mismatch: supported=%v check=%#v", SupportedPlatform(), checks[0])
	}
}
