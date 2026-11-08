package health

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type coreAdd func(Finding)

func coreMemory(proc string, add coreAdd) {
	m, e := coreReadCounters(filepath.Join(proc, "meminfo"))
	total, tok := m["MemTotal:"]
	avail, aok := m["MemAvailable:"]
	if e != nil || !tok || !aok || total == 0 || avail > total {
		add(Finding{ID: coreFindingMemoryCapacity, Area: AreaMemory, Severity: SeverityUnknown, Summary: "Memory values unavailable or inconsistent"})
		return
	}
	used := 100 * (total - avail) / total
	sev := SeverityOK
	if used >= 95 {
		sev = SeverityCritical
	} else if used >= 85 {
		sev = SeverityWarning
	}
	add(Finding{ID: coreFindingMemoryCapacity, Area: AreaMemory, Severity: sev, Summary: fmt.Sprintf("Available memory: %d kB (%d%% used)", avail, used), Evidence: []string{fmt.Sprintf("available_kb=%d", avail), fmt.Sprintf("used_percent=%d", used)}})
	st, so := m["SwapTotal:"]
	sf, sok := m["SwapFree:"]
	if !so || !sok || sf > st {
		add(Finding{ID: coreFindingMemorySwap, Area: AreaMemory, Severity: SeverityUnknown, Summary: "Swap information unavailable or inconsistent"})
	} else if st == 0 {
		add(Finding{ID: coreFindingMemorySwap, Area: AreaMemory, Severity: SeverityOK, Summary: "No swap configured"})
	} else {
		add(Finding{ID: coreFindingMemorySwap, Area: AreaMemory, Severity: SeverityOK, Summary: fmt.Sprintf("Swap used: %d kB", st-sf), Evidence: []string{fmt.Sprintf("used_kb=%d", st-sf)}})
	}
}
func corePressure(proc string, add coreAdd) {
	for _, pressure := range []struct {
		name string
		id   string
	}{{"cpu", coreFindingPressureCPU}, {"memory", coreFindingPressureMemory}, {"io", coreFindingPressureIO}} {
		name := pressure.name
		b, e := os.ReadFile(filepath.Join(proc, "pressure", name))
		if e != nil {
			add(Finding{ID: pressure.id, Area: AreaPressure, Severity: SeverityUnknown, Summary: name + " pressure unavailable"})
			continue
		}
		values := []string{}
		high := false
		valid := true
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			f := strings.Fields(line)
			if len(f) < 4 || (f[0] != "some" && f[0] != "full") {
				valid = false
				break
			}
			found := false
			for _, x := range f[1:] {
				k, v, ok := strings.Cut(x, "=")
				if !ok {
					continue
				}
				if k == "avg10" {
					n, err := strconv.ParseFloat(v, 64)
					if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
						valid = false
					} else if n >= 20 {
						high = true
					}
					found = true
				}
			}
			if !found {
				valid = false
			}
			values = append(values, strings.Join(f, " "))
		}
		if !valid || len(values) == 0 {
			add(Finding{ID: pressure.id, Area: AreaPressure, Severity: SeverityUnknown, Summary: name + " pressure data malformed"})
		} else {
			sev := SeverityOK
			if high {
				sev = SeverityWarning
			}
			add(Finding{ID: pressure.id, Area: AreaPressure, Severity: sev, Summary: name + " pressure averages available", Evidence: values})
		}
	}
}
func coreThermal(sys string, add coreAdd) {
	type sensor struct {
		input     int64
		max, crit int64
	}
	sensors := map[string]sensor{}
	addSensor := func(path string, scale bool) {
		b, e := os.ReadFile(path)
		if e != nil {
			return
		}
		v, e := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if e != nil || v < -100000 || v > 250000 {
			return
		}
		key := strings.TrimSuffix(path, "_input")
		if strings.HasSuffix(path, "/temp") {
			key = path
		}
		s := sensor{input: v}
		if scale {
			s.input = v / 1000
		}
		for _, lim := range []struct {
			name string
			dst  *int64
		}{{"_max", &s.max}, {"_crit", &s.crit}} {
			if z, e := os.ReadFile(key + lim.name); e == nil {
				n, x := strconv.ParseInt(strings.TrimSpace(string(z)), 10, 64)
				if x == nil {
					if scale {
						n /= 1000
					}
					*lim.dst = n
				}
			}
		}
		sensors[key] = s
	}
	patterns := []string{filepath.Join(sys, "class/hwmon/hwmon*", "temp*_input")}
	if matches, _ := filepath.Glob(patterns[0]); len(matches) == 0 {
		patterns = append(patterns, filepath.Join(sys, "class/thermal/thermal_zone*", "temp"))
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, p := range matches {
			addSensor(p, true)
		}
	}
	if len(sensors) == 0 {
		add(Finding{ID: coreFindingThermalTemperature, Area: AreaThermal, Severity: SeverityUnknown, Summary: "Temperature sensors unavailable"})
		return
	}
	keys := make([]string, 0, len(sensors))
	for k := range sensors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	max := int64(0)
	sev := SeverityOK
	ev := []string{}
	for _, k := range keys {
		s := sensors[k]
		if s.input > max {
			max = s.input
		}
		ev = append(ev, fmt.Sprintf("sensor=%s temp_c=%d max_c=%d critical_c=%d", filepath.Base(k), s.input, s.max, s.crit))
		if s.crit > 0 && s.input >= s.crit-5 {
			sev = SeverityCritical
		} else if sev != SeverityCritical && s.max > 0 && s.input >= s.max {
			sev = SeverityWarning
		} else if s.crit == 0 && s.max == 0 && s.input >= 90 {
			sev = SeverityCritical
		} else if s.crit == 0 && s.input >= 80 && sev == SeverityOK {
			sev = SeverityWarning
		}
	}
	add(Finding{ID: coreFindingThermalTemperature, Area: AreaThermal, Severity: sev, Summary: fmt.Sprintf("Highest reported temperature: %d C", max), Evidence: ev})
}
func coreThrottleRead(sys string) map[string]uint64 {
	m := map[string]uint64{}
	matches, _ := filepath.Glob(filepath.Join(sys, "devices/system/cpu/cpu*", "thermal_throttle", "*throttle_count"))
	for _, p := range matches {
		if b, e := os.ReadFile(p); e == nil {
			if n, x := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); x == nil {
				m[p] = n
			}
		}
	}
	return m
}
func coreThrottle(sys string, before map[string]uint64, cancelled bool, add coreAdd) {
	after := coreThrottleRead(sys)
	if len(before) == 0 || len(after) == 0 {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityUnknown, Summary: "Thermal throttle counters unavailable"})
		return
	}
	if cancelled {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityUnknown, Summary: "Thermal throttle sample cancelled"})
		return
	}
	comparable := 0
	inc := false
	reset := false
	historical := false
	for p, a := range after {
		b, ok := before[p]
		if !ok {
			continue
		}
		comparable++
		if a > 0 || b > 0 {
			historical = true
		}
		if a < b {
			reset = true
		} else if a > b {
			inc = true
		}
	}
	if comparable == 0 {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityUnknown, Summary: "No comparable thermal throttle counters"})
		return
	}
	if reset {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityUnknown, Summary: "Thermal throttle counter reset during sample"})
	} else if inc {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityWarning, Summary: "Thermal throttling increased during sample"})
	} else if historical {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityOK, Summary: "Historical thermal throttling recorded; no increase during sample"})
	} else {
		add(Finding{ID: coreFindingThermalThrottle, Area: AreaThermal, Severity: SeverityOK, Summary: "No thermal throttling recorded during sample"})
	}
}
func coreDiskRead(path string) map[string]uint64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	m := map[string]uint64{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 14 {
			continue
		}
		dev := f[2]
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "ram") {
			continue
		}
		rd, e1 := strconv.ParseUint(f[5], 10, 64)
		wr, e2 := strconv.ParseUint(f[9], 10, 64)
		if e1 == nil && e2 == nil {
			m[dev] = rd + wr
		}
	}
	return m
}
func coreDisk(proc string, before map[string]uint64, unsampled bool, add coreAdd) {
	var st syscall.Statfs_t
	if e := syscall.Statfs("/", &st); e != nil {
		add(Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity unavailable"})
	} else {
		total := st.Blocks * uint64(st.Bsize)
		avail := st.Bavail * uint64(st.Bsize)
		if avail > total {
			add(Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Root filesystem capacity inconsistent"})
		} else {
			used := uint64(0)
			if total > 0 {
				used = 100 * (total - avail) / total
			}
			sev := SeverityOK
			if used >= 95 {
				sev = SeverityCritical
			} else if used >= 85 {
				sev = SeverityWarning
			}
			add(Finding{ID: coreFindingDiskRoot, Area: AreaDisk, Severity: sev, Summary: fmt.Sprintf("Root filesystem used: %d%%", used), Evidence: []string{fmt.Sprintf("used_percent=%d", used), fmt.Sprintf("available_bytes=%d", avail)}})
		}
	}
	if unsampled {
		add(Finding{ID: coreFindingDiskActivity, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Disk I/O sample unavailable"})
		return
	}
	after := coreDiskRead(filepath.Join(proc, "diskstats"))
	if len(before) == 0 || len(after) == 0 {
		add(Finding{ID: coreFindingDiskActivity, Area: AreaDisk, Severity: SeverityUnknown, Summary: "Disk activity counters unavailable or malformed"})
		return
	}
	comparable := 0
	rows := []string{}
	for dev, n := range after {
		if old, ok := before[dev]; ok && n >= old {
			rows = append(rows, fmt.Sprintf("device=%s sectors_delta=%d", dev, n-old))
			comparable++
		}
	}
	if comparable == 0 {
		add(Finding{ID: coreFindingDiskActivity, Area: AreaDisk, Severity: SeverityUnknown, Summary: "No comparable disk activity counters"})
		return
	}
	sort.Strings(rows)
	if len(rows) > 10 {
		rows = rows[:10]
	}
	add(Finding{ID: coreFindingDiskActivity, Area: AreaDisk, Severity: SeverityOK, Summary: "Per-device disk I/O activity; devices may include partitions; not a drive health assessment", Evidence: rows})
}
func coreSwap(proc string, before map[string]uint64, unsampled bool, add coreAdd) {
	if unsampled {
		add(Finding{ID: coreFindingSwapActivity, Area: Area("swap"), Severity: SeverityUnknown, Summary: "Swap activity sample unavailable"})
		return
	}
	after, e := coreReadCounters(filepath.Join(proc, "vmstat"))
	if e != nil {
		add(Finding{ID: coreFindingSwapActivity, Area: Area("swap"), Severity: SeverityUnknown, Summary: "Swap activity counters unavailable"})
		return
	}
	a, aok := before["pswpin"]
	b, bok := before["pswpout"]
	c, cok := after["pswpin"]
	d, dok := after["pswpout"]
	if !aok || !bok || !cok || !dok {
		add(Finding{ID: coreFindingSwapActivity, Area: Area("swap"), Severity: SeverityUnknown, Summary: "Swap activity counters unavailable"})
		return
	}
	if c < a || d < b {
		add(Finding{ID: coreFindingSwapActivity, Area: Area("swap"), Severity: SeverityUnknown, Summary: "Swap activity counters reset"})
		return
	}
	add(Finding{ID: coreFindingSwapActivity, Area: Area("swap"), Severity: SeverityOK, Summary: "Swap activity counters sampled", Evidence: []string{fmt.Sprintf("pages_in_delta=%d", c-a), fmt.Sprintf("pages_out_delta=%d", d-b)}})
}
