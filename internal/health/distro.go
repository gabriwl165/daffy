package health

import (
	"os"
	"strings"
)

// distroAdapter contains advisory package-manager guidance; collectors stay shared.
type distroAdapter interface {
	Name() string
	Package(tool string) string
}

type ubuntuFamily struct{}

func (ubuntuFamily) Package(tool string) string {
	return map[string]string{"journalctl": "systemd", "systemctl": "systemd", "systemd-analyze": "systemd", "powerprofilesctl": "power-profiles-daemon", "sensors": "lm-sensors", "lspci": "pciutils", "smartctl": "smartmontools"}[tool]
}

type ubuntuAdapter struct{ ubuntuFamily }

func (ubuntuAdapter) Name() string { return "ubuntu" }

type zorinAdapter struct{ ubuntuFamily }

func (zorinAdapter) Name() string { return "zorin" }

type linuxAdapter struct{}

func (linuxAdapter) Name() string          { return "linux" }
func (linuxAdapter) Package(string) string { return "" }

var distroRegistry = map[string]distroAdapter{
	"ubuntu": ubuntuAdapter{},
	"zorin":  zorinAdapter{},
}
var genericDistro distroAdapter = linuxAdapter{}

func selectDistroAdapter(id string) distroAdapter {
	if a := distroRegistry[id]; a != nil {
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
