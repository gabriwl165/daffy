package health

import (
	"os"
	"strings"
)

type distroID string
type distroTool string

const (
	distroUbuntu distroID = "ubuntu"
	distroZorin  distroID = "zorin"
	distroLinux  distroID = "linux"
)

const (
	toolJournalctl       distroTool = "journalctl"
	toolSystemctl        distroTool = "systemctl"
	toolSystemdAnalyze   distroTool = "systemd-analyze"
	toolPowerprofilesctl distroTool = "powerprofilesctl"
	toolSensors          distroTool = "sensors"
	toolLspci            distroTool = "lspci"
	toolSmartctl         distroTool = "smartctl"
)

// distroDescriptor contains advisory package-manager guidance; collectors stay shared.
type distroDescriptor struct {
	name     string
	packages map[distroTool]string
}

func (d distroDescriptor) Name() string               { return d.name }
func (d distroDescriptor) Package(tool string) string { return d.packages[distroTool(tool)] }

var ubuntuPackages = map[distroTool]string{
	toolJournalctl: "systemd", toolSystemctl: "systemd", toolSystemdAnalyze: "systemd",
	toolPowerprofilesctl: "power-profiles-daemon", toolSensors: "lm-sensors",
	toolLspci: "pciutils", toolSmartctl: "smartmontools",
}

func newUbuntuDescriptor() distroDescriptor {
	return distroDescriptor{name: string(distroUbuntu), packages: ubuntuPackages}
}
func newZorinDescriptor() distroDescriptor {
	return distroDescriptor{name: string(distroZorin), packages: ubuntuPackages}
}

var distroRegistry = map[string]distroDescriptor{
	string(distroUbuntu): newUbuntuDescriptor(),
	string(distroZorin):  newZorinDescriptor(),
}
var genericDistro = distroDescriptor{name: string(distroLinux)}

func selectDistroAdapter(id string) distroDescriptor {
	if a, ok := distroRegistry[id]; ok {
		return a
	}
	return genericDistro
}

// parseOSRelease parses the simple assignment format without evaluation or expansion.
func parseOSRelease(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" || strings.Trim(k, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			continue
		}
		v, ok = parseOSReleaseValue(strings.TrimSpace(v))
		if !ok {
			continue
		}
		out[k] = v
	}
	return out, nil
}

func parseOSReleaseValue(v string) (string, bool) {
	if v == "" {
		return "", true
	}
	quote := byte(0)
	if v[0] == '\'' || v[0] == '"' {
		quote = v[0]
		v = v[1:]
	}
	var out strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if quote != 0 && c == quote {
			if strings.TrimSpace(v[i+1:]) != "" {
				return "", false
			}
			return out.String(), true
		}
		if quote == 0 && (c == '\'' || c == '"') {
			return "", false
		}
		if c == '\\' && quote != '\'' {
			if i+1 >= len(v) {
				return "", false
			}
			n := v[i+1]
			if quote == '"' && n != '$' && n != '`' && n != '"' && n != '\\' {
				out.WriteByte(c)
				continue
			}
			i++
			out.WriteByte(n)
			continue
		}
		out.WriteByte(c)
	}
	return out.String(), quote == 0
}
