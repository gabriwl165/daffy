package ubuntu

import "github.com/gabriwl165/daffy-duck/internal/distribution"

func New() distribution.Profile {
	return distribution.Profile{ID: distribution.Ubuntu, Packages: map[distribution.Tool]distribution.Package{
		distribution.Journalctl: distribution.PackageSystemd, distribution.Systemctl: distribution.PackageSystemd, distribution.SystemdAnalyze: distribution.PackageSystemd,
		distribution.Powerprofilesctl: distribution.PackagePowerProfilesDaemon, distribution.Sensors: distribution.PackageLMSensors,
		distribution.Lspci: distribution.PackagePCIUtils, distribution.Smartctl: distribution.PackageSmartmontools,
	}}
}
