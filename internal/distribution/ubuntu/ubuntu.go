package ubuntu

import "github.com/gabriwl165/daffy-duck/internal/distribution"

func New() distribution.Profile {
	return distribution.Profile{ID: distribution.Ubuntu, Packages: map[distribution.Tool]string{
		distribution.Journalctl: "systemd", distribution.Systemctl: "systemd", distribution.SystemdAnalyze: "systemd",
		distribution.Powerprofilesctl: "power-profiles-daemon", distribution.Sensors: "lm-sensors",
		distribution.Lspci: "pciutils", distribution.Smartctl: "smartmontools",
	}}
}
