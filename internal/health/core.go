package health

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	coreUnknown = SeverityUnknown
	coreOK      = SeverityOK
	coreWarn    = SeverityWarning
	coreCrit    = SeverityCritical
)

type coreCPU struct{ total, idle uint64 }
type coreProc struct {
	pid   int
	name  string
	ticks uint64
	start uint64
}
type coreProcSample struct {
	pid     int
	name    string
	ticks   uint64
	percent float64
}

func coreRankProcesses(before, after []coreProc, total uint64) []coreProcSample {
	if total == 0 {
		return nil
	}
	byPID := make(map[int]coreProc, len(after))
	for _, p := range after {
		byPID[p.pid] = p
	}
	rows := []coreProcSample{}
	for _, p := range before {
		q, ok := byPID[p.pid]
		if !ok || q.start != p.start || q.ticks < p.ticks {
			continue
		}
		delta := q.ticks - p.ticks
		rows = append(rows, coreProcSample{pid: p.pid, name: p.name, ticks: delta, percent: 100 * float64(delta) / float64(total)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ticks == rows[j].ticks {
			return rows[i].pid < rows[j].pid
		}
		return rows[i].ticks > rows[j].ticks
	})
	return rows
}

func collectCore(ctx context.Context, opts Options) Report {
	started := time.Now()
	proc, sys, etc := opts.ProcRoot, opts.SysRoot, opts.EtcRoot
	if proc == "" {
		proc = "/proc"
	}
	if sys == "" {
		sys = "/sys"
	}
	if etc == "" {
		etc = "/etc"
	}
	r := Report{SchemaVersion: "1", CollectedAt: started, System: SystemInfo{}, Findings: []Finding{}}
	add := func(id string, area Area, sev Severity, summary string, evidence ...string) {
		recommendation := map[string]string{"memory.capacity": "Review memory-heavy applications and available capacity.", "memory.swap": "Review swap configuration and workload memory needs.", "cpu.utilization": "Investigate sustained CPU saturation and competing workloads.", "process.cpu": "Review the listed process CPU consumers.", "thermal.temperature": "Check cooling, airflow, and thermal limits.", "thermal.throttle": "Check cooling and reduce sustained compute load if throttling continues.", "disk.root": "Free space or expand the root filesystem.", "disk.activity": "Correlate I/O activity with application workload.", "pressure.cpu": "Investigate CPU contention and runnable workload.", "pressure.memory": "Investigate memory pressure and reclaim activity.", "pressure.io": "Investigate storage latency and competing I/O."}[id]
		if sev == coreUnknown {
			recommendation = "Check that the relevant Linux procfs/sysfs data is mounted and readable."
		}
		if id == "workload.build" {
			recommendation = "Reduce concurrent builds or other Java workloads if contention persists."
		}
		r.Findings = append(r.Findings, Finding{ID: id, Area: area, Severity: sev, Summary: summary, Evidence: evidence, Recommendation: recommendation, Confidence: map[Severity]Confidence{coreUnknown: ConfidenceLow, coreOK: ConfidenceMedium, coreWarn: ConfidenceMedium, coreCrit: ConfidenceHigh}[sev]})
	}
	osRelease, _ := parseOSRelease(filepath.Join(etc, "os-release"))
	r.System.OS = osRelease["PRETTY_NAME"]
	adapter := selectDistroAdapter(osRelease["ID"])
	r.System.DistroID = osRelease["ID"]
	if r.System.DistroID == "" {
		r.System.DistroID = string(coreUnknown)
	}
	r.System.DistroAdapter = adapter.Name()
	if version := osRelease["VERSION_ID"]; version != "" {
		r.System.DistroVersion = version
	}
	r.System.Kernel, _ = coreReadTrim(filepath.Join(proc, "sys/kernel/osrelease"))
	if b, err := os.ReadFile(filepath.Join(proc, "cpuinfo")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if ok && (strings.TrimSpace(k) == "model name" || strings.TrimSpace(k) == "Hardware") {
				r.System.CPUModel = strings.TrimSpace(v)
				break
			}
		}
	}
	interval := opts.SampleInterval
	if interval <= 0 {
		interval = time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	cpu0, cpuErr := coreReadCPU(filepath.Join(proc, "stat"))
	procs0 := coreReadProcs(proc)
	vm0, _ := coreReadCounters(filepath.Join(proc, "vmstat"))
	throttle0 := coreThrottleRead(sys)
	disk0 := coreDiskRead(filepath.Join(proc, "diskstats"))
	validSample := false
	var totalDelta uint64
	if cpuErr == nil {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			add("cpu.utilization", "cpu", coreUnknown, "CPU sample cancelled")
		case <-timer.C:
			cpu1, e := coreReadCPU(filepath.Join(proc, "stat"))
			if e != nil {
				add("cpu.utilization", "cpu", coreUnknown, "CPU counters unavailable")
			} else if cpu1.total <= cpu0.total || cpu1.idle < cpu0.idle || cpu1.idle-cpu0.idle > cpu1.total-cpu0.total {
				add("cpu.utilization", "cpu", coreUnknown, "CPU counters did not advance")
			} else {
				totalDelta = cpu1.total - cpu0.total
				validSample = true
				used := 100 * (totalDelta - (cpu1.idle - cpu0.idle)) / totalDelta
				severity := coreOK
				if used >= 90 {
					severity = coreWarn
				}
				add("cpu.utilization", "cpu", severity, fmt.Sprintf("Sampled whole-machine CPU utilization: %d%%", used), fmt.Sprintf("utilization_percent=%d", used))
			}
		}
	} else {
		add("cpu.utilization", "cpu", coreUnknown, "CPU counters unavailable")
	}
	if ctx.Err() == nil && validSample {
		procs1 := coreReadProcs(proc)
		rows := coreRankProcesses(procs0, procs1, totalDelta)
		if len(rows) == 0 {
			add("process.cpu", "process", coreUnknown, "Per-process CPU deltas unavailable")
		} else {
			if len(rows) > 10 {
				rows = rows[:10]
			}
			evidence := make([]string, 0, len(rows))
			for _, row := range rows {
				evidence = append(evidence, fmt.Sprintf("pid=%d comm=%q cpu_percent_whole_machine=%.2f ticks=%d", row.pid, row.name, row.percent, row.ticks))
			}
			add("process.cpu", "process", coreOK, "Sampled per-process CPU utilization (whole machine)", evidence...)
			java := 0
			for _, row := range rows {
				if row.ticks > 0 && (strings.Contains(strings.ToLower(row.name), "java") || strings.Contains(strings.ToLower(row.name), "gradle")) {
					java++
				}
			}
			if java > 1 {
				add("workload.build", "workload", coreWarn, "Multiple active Java/Gradle-like processes observed; build activity is not confirmed", fmt.Sprintf("matching_processes=%d", java))
			}
		}
	} else {
		detail := "Per-process CPU sample unavailable"
		if ctx.Err() != nil {
			detail = "Per-process CPU sample cancelled"
		}
		add("process.cpu", "process", coreUnknown, detail)
	}
	coreMemory(proc, add)
	corePressure(proc, add)
	coreThermal(sys, add)
	coreThrottle(sys, throttle0, ctx.Err() != nil || !validSample, add)
	coreDisk(proc, disk0, !validSample, add)
	coreSwap(proc, vm0, !validSample, add)
	if ctx.Err() != nil {
		add("collection.cancelled", "core", coreUnknown, "Collection stopped before all samples completed")
	}
	r.Duration = time.Since(started)
	return r
}

func coreReadTrim(path string) (string, error) {
	b, e := os.ReadFile(path)
	return strings.TrimSpace(string(b)), e
}
func coreReadCPU(path string) (coreCPU, error) {
	f, e := os.Open(path)
	if e != nil {
		return coreCPU{}, e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return coreCPU{}, fmt.Errorf("missing cpu row")
	}
	fields := strings.Fields(s.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return coreCPU{}, fmt.Errorf("malformed cpu row")
	}
	var c coreCPU
	for i, v := range fields[1:] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return c, err
		}
		if i != 8 && i != 9 {
			c.total += n
		}
		if i == 3 || i == 4 {
			c.idle += n
		}
	}
	return c, nil
}
func coreReadProcs(root string) []coreProc {
	es, _ := os.ReadDir(root)
	out := []coreProc{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue
		}
		p, err := coreParseProcStat(string(b))
		if err == nil {
			p.pid = pid
			out = append(out, p)
		}
	}
	return out
}

// coreParseProcStat handles comm values containing spaces and closing parentheses.
func coreParseProcStat(line string) (coreProc, error) {
	a := strings.IndexByte(line, '(')
	b := strings.LastIndexByte(line, ')')
	if a < 1 || b <= a || b+2 >= len(line) {
		return coreProc{}, fmt.Errorf("malformed proc stat")
	}
	fields := strings.Fields(line[b+1:])
	if len(fields) < 20 {
		return coreProc{}, fmt.Errorf("short proc stat")
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return coreProc{}, err
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return coreProc{}, err
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return coreProc{}, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:a]))
	if err != nil {
		return coreProc{}, err
	}
	return coreProc{pid: pid, name: line[a+1 : b], ticks: user + system, start: start}, nil
}

func coreOSName(path string) (string, error) {
	m, err := parseOSRelease(path)
	if err != nil {
		return "", err
	}
	if m["PRETTY_NAME"] == "" {
		return "", fmt.Errorf("PRETTY_NAME unavailable")
	}
	return m["PRETTY_NAME"], nil
}
func coreReadCounters(path string) (map[string]uint64, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	m := map[string]uint64{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			n, x := strconv.ParseUint(f[1], 10, 64)
			if x == nil {
				m[f[0]] = n
			}
		}
	}
	return m, nil
}
