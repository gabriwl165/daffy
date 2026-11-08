package distribution

type ID string

const (
	Ubuntu ID = "ubuntu"
	Zorin  ID = "zorin"
	Linux  ID = "linux"
)

type Tool string

const (
	Journalctl       Tool = "journalctl"
	Systemctl        Tool = "systemctl"
	SystemdAnalyze   Tool = "systemd-analyze"
	Powerprofilesctl Tool = "powerprofilesctl"
	Sensors          Tool = "sensors"
	Lspci            Tool = "lspci"
	Smartctl         Tool = "smartctl"
)

type Package string

const (
	PackageSystemd             Package = "systemd"
	PackagePowerProfilesDaemon Package = "power-profiles-daemon"
	PackageLMSensors           Package = "lm-sensors"
	PackagePCIUtils            Package = "pciutils"
	PackageSmartmontools       Package = "smartmontools"
)

type Profile struct {
	ID       ID
	Packages map[Tool]Package
}

func (p Profile) Name() string               { return string(p.ID) }
func (p Profile) Package(tool string) string { return string(p.Packages[Tool(tool)]) }
