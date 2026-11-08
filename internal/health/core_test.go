package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoreCounterAndCPUParsing(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "meminfo")
	if e := os.WriteFile(p, []byte("MemTotal: 1000 kB\nMemAvailable: 100 kB\nSwapTotal: 20 kB\nSwapFree: 3 kB\n"), 0600); e != nil {
		t.Fatal(e)
	}
	m, e := coreReadCounters(p)
	if e != nil || m["MemTotal:"] != 1000 {
		t.Fatalf("meminfo parse: %v %#v", e, m)
	}
	add := []Finding{}
	coreMemory(d, func(id string, a Area, s Severity, su string, ev ...string) {
		add = append(add, Finding{ID: id, Severity: s, Summary: su})
	})
	if len(add) != 2 || add[0].Severity != coreWarn {
		t.Fatalf("memory thresholds: %#v", add)
	}
	cpu := filepath.Join(d, "stat")
	os.WriteFile(cpu, []byte("cpu 10 2 3 40 5 1 1 1 500 600\n"), 0600)
	c, e := coreReadCPU(cpu)
	if e != nil {
		t.Fatal(e)
	}
	if c.total != 63 || c.idle != 45 {
		t.Fatalf("guest accounting: %#v", c)
	}
	bad := filepath.Join(d, "bad")
	os.WriteFile(bad, []byte("cpu 100 0 0 20\n"), 0600)
	old, _ := coreReadCPU(bad)
	if old.total <= c.total {
		t.Fatalf("fixture expected advancing total: %#v", old)
	}
}
func TestCoreProcStatCommAndReuse(t *testing.T) {
	p, e := coreParseProcStat("42 (java (Gradle Worker)) R 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 999")
	if e != nil {
		t.Fatal(e)
	}
	if p.name != "java (Gradle Worker)" || p.ticks != 23 || p.start != 999 {
		t.Fatalf("parsed proc stat: %#v", p)
	}
	q := p
	q.start++
	if p.start == q.start {
		t.Fatal("PID reuse identity not distinguished")
	}
}

func TestCoreProcessUtilizationSortedAndPIDReuse(t *testing.T) {
	before := []coreProc{{pid: 1, name: "colon: worker", ticks: 10, start: 100}, {pid: 2, name: "java", ticks: 20, start: 200}, {pid: 3, name: "reused", ticks: 1, start: 300}}
	after := []coreProc{{pid: 1, name: "colon: worker", ticks: 13, start: 100}, {pid: 2, name: "java", ticks: 30, start: 200}, {pid: 3, name: "replacement", ticks: 100, start: 301}}
	r := coreRankProcesses(before, after, 100)
	if len(r) != 2 || r[0].pid != 2 || r[0].percent != 10 || r[1].pid != 1 || r[1].percent != 3 {
		t.Fatalf("ranked proc sample: %#v", r)
	}
}
func TestCorePressureMalformedAndHigh(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "pressure"), 0700)
	os.WriteFile(filepath.Join(d, "pressure/cpu"), []byte("some avg10=25.00 avg60=1.00 avg300=1.00 total=42\n"), 0600)
	os.WriteFile(filepath.Join(d, "pressure/memory"), []byte("garbage\n"), 0600)
	out := map[string]Severity{}
	corePressure(d, func(id string, a Area, s Severity, su string, ev ...string) { out[id] = s })
	if out["pressure.cpu"] != coreWarn || out["pressure.memory"] != coreUnknown {
		t.Fatalf("pressure classifications: %#v", out)
	}
}

func TestCorePressureRejectsNaN(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "pressure"), 0700)
	for _, n := range []string{"cpu", "memory", "io"} {
		os.WriteFile(filepath.Join(d, "pressure", n), []byte("some avg10=NaN avg60=0 avg300=0 total=1\n"), 0600)
	}
	corePressure(d, func(id string, a Area, s Severity, su string, ev ...string) {
		if s != coreUnknown {
			t.Errorf("%s severity=%s", id, s)
		}
	})
}
func TestCoreThermalSymlinksLimitsAndFallback(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "real")
	os.MkdirAll(real, 0700)
	os.MkdirAll(filepath.Join(d, "class/hwmon"), 0700)
	os.Mkdir(filepath.Join(d, "class/hwmon/hwmon0"), 0700)
	for n, v := range map[string]string{"temp1_input": "70000", "temp1_max": "65000", "temp1_crit": "90000"} {
		os.WriteFile(filepath.Join(real, n), []byte(v), 0600)
	}
	os.Remove(filepath.Join(d, "class/hwmon/hwmon0"))
	os.Symlink(real, filepath.Join(d, "class/hwmon/hwmon0"))
	out := Severity("")
	coreThermal(d, func(id string, a Area, s Severity, su string, ev ...string) { out = s })
	if out != coreWarn {
		t.Fatalf("symlink sensor/limit severity=%s", out)
	}
	// Near the critical hardware trip threshold is critical, and thermal-zone
	// sysfs temperatures are millidegrees even when represented as temp.
	os.WriteFile(filepath.Join(real, "temp1_input"), []byte("99000"), 0600)
	out = ""
	coreThermal(d, func(id string, a Area, s Severity, su string, ev ...string) { out = s })
	if out != coreCrit {
		t.Fatalf("near-critical severity=%s", out)
	}
	zoneRoot := filepath.Join(d, "fallback")
	zone := filepath.Join(zoneRoot, "class/thermal/thermal_zone0")
	os.MkdirAll(zone, 0700)
	os.WriteFile(filepath.Join(zone, "temp"), []byte("42000"), 0600)
	summary := ""
	coreThermal(zoneRoot, func(id string, a Area, s Severity, su string, ev ...string) { summary = su })
	if !strings.Contains(summary, "42 C") {
		t.Fatalf("thermal-zone unit conversion: %s", summary)
	}
	fallback := filepath.Join(d, "class/thermal/thermal_zone0")
	os.MkdirAll(fallback, 0700)
	os.WriteFile(filepath.Join(fallback, "temp"), []byte("95000"), 0600)
	coreThermal(d, func(id string, a Area, s Severity, su string, ev ...string) { out = s })
	if out != coreCrit {
		t.Fatalf("thermal fallback severity=%s", out)
	}
}

func TestCoreCancelledSamplingUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := collectCore(ctx, Options{ProcRoot: t.TempDir(), SysRoot: t.TempDir(), EtcRoot: t.TempDir(), SampleInterval: time.Hour})
	found := false
	for _, f := range r.Findings {
		if f.ID == "process.cpu" && f.Severity == coreUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("cancelled process sample not unknown: %#v", r.Findings)
	}
}
func TestCoreThrottleAndDiskDeltas(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "devices/system/cpu/cpu0/thermal_throttle")
	os.MkdirAll(p, 0700)
	f := filepath.Join(p, "core_throttle_count")
	os.WriteFile(f, []byte("5"), 0600)
	before := coreThrottleRead(d)
	os.WriteFile(f, []byte("6"), 0600)
	sev := Severity("")
	coreThrottle(d, before, false, func(id string, a Area, s Severity, su string, ev ...string) { sev = s })
	if sev != coreWarn {
		t.Fatalf("active throttle=%s", sev)
	}
	os.WriteFile(f, []byte("2"), 0600)
	before = map[string]uint64{f: 5}
	coreThrottle(d, before, false, func(id string, a Area, s Severity, su string, ev ...string) { sev = s })
	if sev != coreUnknown {
		t.Fatalf("reset throttle=%s", sev)
	}
	disk := filepath.Join(d, "diskstats")
	line := func(n string) string { return "8 0 sda 1 0 " + n + " 0 1 0 " + n + " 0 0 0 0 0 0 0\n" }
	os.WriteFile(disk, []byte(line("10")), 0600)
	a := coreDiskRead(disk)
	os.WriteFile(disk, []byte(line("15")), 0600)
	b := coreDiskRead(disk)
	if b["sda"]-a["sda"] != 10 {
		t.Fatalf("disk delta: %#v %#v", a, b)
	}
}

func TestCoreNoComparableCounterSamplesUnknown(t *testing.T) {
	sev := Severity("")
	coreThrottle(t.TempDir(), map[string]uint64{"old": 3}, false, func(id string, a Area, s Severity, su string, ev ...string) { sev = s })
	if sev != coreUnknown {
		t.Fatalf("unmatched throttle counters severity=%s", sev)
	}
	proc := t.TempDir()
	disk := filepath.Join(proc, "diskstats")
	os.WriteFile(disk, []byte("8 0 sda 1 0 10 0 1 0 10 0 0 0 0 0 0 0\n"), 0600)
	sev = ""
	coreDisk(proc, map[string]uint64{"other": 2}, false, func(id string, a Area, s Severity, su string, ev ...string) {
		if id == "disk.activity" {
			sev = s
		}
	})
	if sev != coreUnknown {
		t.Fatalf("unmatched disk counters severity=%s", sev)
	}
}

func TestCoreRedactsOSDetailsAndSensitiveProc(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "etc")
	os.Mkdir(p, 0700)
	os.WriteFile(filepath.Join(p, "os-release"), []byte("NAME=Example\nPRETTY_NAME=\"Example Linux\"\nHOME=/private/user\n"), 0600)
	name, _ := coreOSName(filepath.Join(p, "os-release"))
	if name != "Example Linux" || strings.Contains(name, "/private") {
		t.Fatalf("os identity leaked: %q", name)
	}
	if _, e := coreParseProcStat("5 (safe process) S 0 0 0 0 0 0 0 0 0 1 2 0 0 0 0 0 0 0 77"); e != nil {
		t.Fatal(e)
	}
}
