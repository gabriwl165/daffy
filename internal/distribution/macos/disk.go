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
	DiskExecutable               = "/usr/sbin/iostat"
	DiskOutputLimit              = 64 * 1024
	DiskDefaultDeviceLimit       = 32
	DiskHeaderKBPerTransfer      = "KB/t"
	DiskHeaderTransfersPerSecond = "tps"
	DiskHeaderMegabytesPerSecond = "MB/s"
	DiskDevicePrefix             = "disk"
)

type DiskDeviceActivity struct {
	Device               string
	KilobytesPerTransfer float64
	TransfersPerSecond   float64
	MegabytesPerSecond   float64
}

type DiskUsage struct {
	Devices        []DiskDeviceActivity
	SampleInterval time.Duration
	DeviceLimit    int
	CoverageNote   string
}

type DiskCollector struct{ Runner DiagnosticRunner }

// Collect samples the second iostat interval. Positive timeouts are respected
// exactly and rejected before execution when they cannot cover the sample.
func (c DiskCollector) Collect(ctx context.Context, sampleInterval, commandTimeout time.Duration, deviceLimit int) (DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return DiskUsage{}, err
	}
	if c.Runner == nil {
		return DiskUsage{}, fmt.Errorf("disk diagnostic runner unavailable")
	}
	seconds := int(math.Ceil(sampleInterval.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	if deviceLimit <= 0 || deviceLimit > DiskDefaultDeviceLimit {
		deviceLimit = DiskDefaultDeviceLimit
	}
	if commandTimeout <= 0 {
		commandTimeout = time.Duration(seconds+2) * time.Second
	} else if commandTimeout <= time.Duration(seconds)*time.Second {
		return DiskUsage{}, fmt.Errorf("command timeout too short for disk sample")
	}
	r := c.Runner.Run(ctx, DiagnosticCommand{Executable: DiskExecutable, Args: []string{"-d", "-w", strconv.Itoa(seconds), "-c", "2", "-n", strconv.Itoa(deviceLimit)}, Timeout: commandTimeout})
	if r.Err != nil {
		return DiskUsage{}, r.Err
	}
	if err := ctx.Err(); err != nil {
		return DiskUsage{}, err
	}
	if r.Truncated || len(r.Stdout) > DiskOutputLimit || len(r.Stderr) != 0 {
		return DiskUsage{}, fmt.Errorf("iostat output invalid or truncated")
	}
	u, err := ParseDiskUsage(string(r.Stdout), seconds, deviceLimit)
	if err != nil {
		return DiskUsage{}, err
	}
	return u, nil
}

type diskColumn uint8

const (
	diskColumnInvalid diskColumn = iota
	diskColumnKBPerTransfer
	diskColumnTransfersPerSecond
	diskColumnMegabytesPerSecond
)

func parseDiskColumn(s string) diskColumn {
	switch s {
	case DiskHeaderKBPerTransfer:
		return diskColumnKBPerTransfer
	case DiskHeaderTransfersPerSecond:
		return diskColumnTransfersPerSecond
	case DiskHeaderMegabytesPerSecond:
		return diskColumnMegabytesPerSecond
	default:
		return diskColumnInvalid
	}
}

func ParseDiskUsage(output string, sampleSeconds, deviceLimit int) (DiskUsage, error) {
	if len(output) > DiskOutputLimit {
		return DiskUsage{}, fmt.Errorf("iostat output exceeds limit")
	}
	if deviceLimit <= 0 || deviceLimit > DiskDefaultDeviceLimit {
		deviceLimit = DiskDefaultDeviceLimit
	}
	lines := strings.Split(output, "\n")
	var devices []string
	var header []diskColumn
	var sampleRows int
	var second []DiskDeviceActivity
	for _, raw := range lines {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 {
			continue
		}
		deviceRow := true
		for _, f := range fields {
			if !strings.HasPrefix(f, DiskDevicePrefix) {
				deviceRow = false
				break
			}
		}
		if deviceRow {
			if len(fields) > deviceLimit {
				return DiskUsage{}, fmt.Errorf("iostat device count exceeds requested limit")
			}
			devices = append([]string(nil), fields...)
			continue
		}
		candidate := make([]diskColumn, len(fields))
		allColumns := true
		for i, f := range fields {
			candidate[i] = parseDiskColumn(f)
			if candidate[i] == diskColumnInvalid {
				allColumns = false
			}
		}
		if allColumns {
			if len(devices) == 0 || len(candidate) != len(devices)*3 {
				return DiskUsage{}, fmt.Errorf("iostat column/device count mismatch")
			}
			for start := 0; start < len(candidate); start += 3 {
				a, b, c := candidate[start], candidate[start+1], candidate[start+2]
				if a != diskColumnKBPerTransfer || b != diskColumnTransfersPerSecond || c != diskColumnMegabytesPerSecond {
					return DiskUsage{}, fmt.Errorf("inconsistent iostat columns")
				}
			}
			header = candidate
			continue
		}
		if header == nil {
			continue
		}
		if len(fields) != len(header) {
			return DiskUsage{}, fmt.Errorf("iostat data column count mismatch")
		}
		row := make([]DiskDeviceActivity, len(devices))
		for i, name := range devices {
			row[i].Device = name
			for j := 0; j < 3; j++ {
				value, err := strconv.ParseFloat(fields[i*3+j], 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
					return DiskUsage{}, fmt.Errorf("invalid iostat measurement")
				}
				switch header[i*3+j] {
				case diskColumnKBPerTransfer:
					row[i].KilobytesPerTransfer = value
				case diskColumnTransfersPerSecond:
					row[i].TransfersPerSecond = value
				case diskColumnMegabytesPerSecond:
					row[i].MegabytesPerSecond = value
				}
			}
		}
		sampleRows++
		if sampleRows == 2 {
			second = row
		}
	}
	if sampleRows < 2 {
		return DiskUsage{}, fmt.Errorf("second iostat sample unavailable")
	}
	if len(second) == 0 {
		return DiskUsage{}, fmt.Errorf("second iostat sample has no device rows")
	}
	if len(second) > deviceLimit {
		return DiskUsage{}, fmt.Errorf("iostat device count exceeds requested limit")
	}
	return DiskUsage{Devices: second, SampleInterval: time.Duration(sampleSeconds) * time.Second, DeviceLimit: deviceLimit, CoverageNote: fmt.Sprintf("second interval sample; at most %d devices requested", deviceLimit)}, nil
}
