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

type Profile struct {
	ID       ID
	Packages map[Tool]string
}

func (p Profile) Name() string               { return string(p.ID) }
func (p Profile) Package(tool string) string { return p.Packages[Tool(tool)] }
