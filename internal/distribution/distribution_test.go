package distribution_test

import (
	"testing"

	"github.com/gabriwl165/daffy/internal/distribution"
	"github.com/gabriwl165/daffy/internal/distribution/ubuntu"
	"github.com/gabriwl165/daffy/internal/distribution/zorin"
)

func TestDistroProfiles(t *testing.T) {
	u := ubuntu.New()
	z := zorin.New()
	if u.Name() != string(distribution.Ubuntu) || z.Name() != string(distribution.Zorin) || u.Name() == z.Name() {
		t.Fatalf("profile IDs: Ubuntu=%q Zorin=%q", u.Name(), z.Name())
	}
	want := map[distribution.Tool]distribution.Package{
		distribution.Journalctl: distribution.PackageSystemd, distribution.Systemctl: distribution.PackageSystemd, distribution.SystemdAnalyze: distribution.PackageSystemd,
		distribution.Powerprofilesctl: distribution.PackagePowerProfilesDaemon, distribution.Sensors: distribution.PackageLMSensors,
		distribution.Lspci: distribution.PackagePCIUtils, distribution.Smartctl: distribution.PackageSmartmontools,
	}
	for name, profile := range map[string]distribution.Profile{"ubuntu": u, "zorin": z} {
		for tool, pkg := range want {
			if got := profile.Packages[tool]; got != pkg {
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
	u.Packages[distribution.Journalctl] = distribution.Package("changed")
	if got := zorin.New().Package("journalctl"); got != "systemd" {
		t.Errorf("Ubuntu mutation leaked into fresh Zorin profile: %q", got)
	}
}

func TestPackageWireValues(t *testing.T) {
	want := map[distribution.Package]string{
		distribution.PackageSystemd:             "systemd",
		distribution.PackagePowerProfilesDaemon: "power-profiles-daemon",
		distribution.PackageLMSensors:           "lm-sensors",
		distribution.PackagePCIUtils:            "pciutils",
		distribution.PackageSmartmontools:       "smartmontools",
	}
	for pkg, value := range want {
		if string(pkg) != value {
			t.Errorf("package %v wire value = %q, want %q", pkg, string(pkg), value)
		}
	}
}
