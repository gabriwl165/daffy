package macos

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	CPUExecutable    = "/usr/bin/top"
	CPUOutputLimit   = 16 * 1024
	CPUUsagePrefix   = "CPU usage:"
	CPUPercentSuffix = "%"
	CPUUserField     = "user"
	CPUSystemField   = "sys"
	CPUIdleField     = "idle"
)

type CPUUsage struct {
	SampleInterval time.Duration
	UserPercent    float64
	SystemPercent  float64
	IdlePercent    float64
	BusyPercent    float64
}

type CPUCollector struct{ Runner DiagnosticRunner }

type cpuField uint8

const (
	cpuFieldUnknown cpuField = iota
	cpuFieldUser
	cpuFieldSystem
	cpuFieldIdle
)

func parseCPUField(s string) cpuField {
	switch strings.TrimSuffix(s, ":") {
	case CPUUserField:
		return cpuFieldUser
	case CPUSystemField:
		return cpuFieldSystem
	case CPUIdleField:
		return cpuFieldIdle
	default:
		return cpuFieldUnknown
	}
}

// Collect samples macOS CPU utilization. A positive commandTimeout is respected
// exactly; if it cannot accommodate the requested top interval, no command runs.
// An unspecified timeout defaults to the rounded sampling interval plus two seconds.
func (c CPUCollector) Collect(ctx context.Context, sampleInterval, commandTimeout time.Duration) (CPUUsage, error) {
	if err := ctx.Err(); err != nil {
		return CPUUsage{}, err
	}
	seconds := int(math.Ceil(sampleInterval.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	if c.Runner == nil {
		return CPUUsage{}, fmt.Errorf("CPU diagnostic runner unavailable")
	}
	if commandTimeout <= 0 {
		commandTimeout = time.Duration(seconds+2) * time.Second
	} else if commandTimeout <= time.Duration(seconds)*time.Second {
		return CPUUsage{}, fmt.Errorf("command timeout too short for CPU sample")
	}
	r := c.Runner.Run(ctx, DiagnosticCommand{Executable: CPUExecutable, Args: []string{"-l", "2", "-s", strconv.Itoa(seconds), "-n", "0", "-F"}, Timeout: commandTimeout})
	if r.Err != nil {
		return CPUUsage{}, r.Err
	}
	if err := ctx.Err(); err != nil {
		return CPUUsage{}, err
	}
	if r.Truncated || len(r.Stdout) > CPUOutputLimit || len(r.Stderr) != 0 {
		return CPUUsage{}, fmt.Errorf("top output invalid or truncated")
	}
	u, err := ParseCPUUsage(string(r.Stdout))
	if err != nil {
		return CPUUsage{}, err
	}
	u.SampleInterval = time.Duration(seconds) * time.Second
	return u, nil
}

func ParseCPUUsage(output string) (CPUUsage, error) {
	if len(output) > CPUOutputLimit {
		return CPUUsage{}, fmt.Errorf("top output exceeds limit")
	}
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), CPUUsagePrefix) {
			lines = append(lines, line)
		}
	}
	if len(lines) < 2 {
		return CPUUsage{}, fmt.Errorf("second CPU usage sample unavailable")
	}
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[1]), CPUUsagePrefix))
	var user, system, idle float64
	var seenUser, seenSystem, seenIdle bool
	for _, item := range strings.Split(line, ",") {
		fields := strings.Fields(strings.TrimSpace(item))
		if len(fields) != 2 || !strings.HasSuffix(fields[0], CPUPercentSuffix) {
			return CPUUsage{}, fmt.Errorf("malformed CPU usage field")
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], CPUPercentSuffix), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return CPUUsage{}, fmt.Errorf("invalid CPU percentage")
		}
		switch parseCPUField(fields[1]) {
		case cpuFieldUser:
			if seenUser {
				return CPUUsage{}, fmt.Errorf("duplicate CPU usage field")
			}
			user, seenUser = v, true
		case cpuFieldSystem:
			if seenSystem {
				return CPUUsage{}, fmt.Errorf("duplicate CPU usage field")
			}
			system, seenSystem = v, true
		case cpuFieldIdle:
			if seenIdle {
				return CPUUsage{}, fmt.Errorf("duplicate CPU usage field")
			}
			idle, seenIdle = v, true
		default:
			return CPUUsage{}, fmt.Errorf("unknown CPU usage field")
		}
	}
	if !seenUser || !seenSystem || !seenIdle || math.Abs(user+system+idle-100) > 1.0 {
		return CPUUsage{}, fmt.Errorf("inconsistent CPU usage totals")
	}
	return CPUUsage{UserPercent: user, SystemPercent: system, IdlePercent: idle, BusyPercent: user + system}, nil
}
