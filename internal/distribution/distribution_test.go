package distribution_test

import (
	"testing"

	"github.com/gabriwl165/daffy-duck/internal/distribution"
	"github.com/gabriwl165/daffy-duck/internal/distribution/ubuntu"
	"github.com/gabriwl165/daffy-duck/internal/distribution/zorin"
)

func TestDistroProfiles(t *testing.T) {
	u := ubuntu.New()
	z := zorin.New()
	if u.Name() != string(distribution.Ubuntu) || z.Name() != string(distribution.Zorin) || u.Name() == z.Name() {
		t.Fatalf("profile IDs: Ubuntu=%q Zorin=%q", u.Name(), z.Name())
	}
	want := map[string]string{
		"journalctl": "systemd", "systemctl": "systemd", "systemd-analyze": "systemd",
		"powerprofilesctl": "power-profiles-daemon", "sensors": "lm-sensors",
		"lspci": "pciutils", "smartctl": "smartmontools",
	}
	for name, profile := range map[string]distribution.Profile{"ubuntu": u, "zorin": z} {
		for tool, pkg := range want {
			if got := profile.Package(tool); got != pkg {
				t.Errorf("%s package for %s = %q, want %q", name, tool, got, pkg)
			}
		}
		if got := profile.Package("unknown-tool"); got != "" {
			t.Errorf("%s unknown tool package = %q", name, got)
		}
	}
	if got := (distribution.Profile{ID: distribution.Linux}).Package("journalctl"); got != "" {
		t.Errorf("generic profile package = %q", got)
	}
	u.Packages[distribution.Journalctl] = "changed"
	if got := zorin.New().Package("journalctl"); got != "systemd" {
		t.Errorf("Ubuntu mutation leaked into fresh Zorin profile: %q", got)
	}
}
