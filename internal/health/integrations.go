package health

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const integrationOutputLimit = 256 * 1024

type integrationResult struct {
	stdout, stderr []byte
	err            error
	truncated      bool
}

type integrationExecutor func(context.Context, time.Duration, string, ...string) integrationResult

var integrationRun integrationExecutor = integrationExec

type boundedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := integrationOutputLimit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	if n > remaining {
		b.truncated = true
	}
	return n, nil
}

func integrationExec(parent context.Context, timeout time.Duration, name string, args ...string) integrationResult {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Env = append(os.Environ(), "LC_ALL=C", "SYSTEMD_PAGER=cat", "SYSTEMD_PAGERSECURE=1")
	var out, errout boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return integrationResult{stdout: out.Bytes(), stderr: errout.Bytes(), err: err, truncated: out.truncated || errout.truncated}
}

func integrationFinding(id, area, severity, summary string, evidence ...string) Finding {
	return Finding{ID: id, Area: area, Severity: severity, Summary: summary, Evidence: evidence, Confidence: "medium"}
}

// Collect augments core procfs/sysfs signals with bounded, read-only system tools.
func Collect(ctx context.Context, opts Options) Report {
	started := time.Now()
	r := CollectCore(ctx, opts)
	if r.System == nil {
		r.System = map[string]string{}
	}
	var fs []Finding
	if ctx.Err() == nil {
		fs = append(fs, integrationJournal(ctx, opts, "--boot", "current")...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationJournal(ctx, opts, "--boot=-1", "previous")...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationServices(ctx, opts)...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationBootTime(ctx, opts)...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationGPU(ctx, opts)...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationPower(ctx, opts)...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationDisks(ctx, opts)...)
	}
	r.Findings = append(r.Findings, fs...)
	r.Duration = time.Since(started)
	return r
}

func integrationJournal(ctx context.Context, o Options, bootArg, label string) []Finding {
	r := integrationRun(ctx, o.CommandTimeout, "journalctl", "--no-pager", "--quiet", "--lines=1500", "-k", bootArg, "-o", "cat")
	if r.err != nil {
		sev, detail := "unknown", "journalctl unavailable or kernel journal unreadable"
		if label == "previous" && strings.Contains(strings.ToLower(string(r.stderr)), "no journal files") {
			detail = "previous boot journal unavailable"
		}
		f := integrationFinding("journal-"+label, "logs", sev, detail)
		f.Recommendation = "Check journal permissions and availability; absence of readable logs is not evidence of health."
		return []Finding{f}
	}
	if strings.TrimSpace(string(r.stderr)) != "" || strings.TrimSpace(string(r.stdout)) == "" {
		f := integrationFinding("journal-"+label, "logs", "unknown", "Kernel journal read was empty or reported diagnostics; matching events cannot be ruled out")
		f.Recommendation = "Check journal permissions and whether this boot has retained kernel logs."
		return []Finding{f}
	}
	counts := map[string]int{"oom kills": 0, "GPU faults": 0, "I/O errors": 0, "kernel panics": 0, "thermal events": 0}
	for _, line := range strings.Split(strings.ToLower(string(r.stdout)), "\n") {
		if strings.Contains(line, "oom-kill") || strings.Contains(line, "killed process") || strings.Contains(line, "out of memory: kill process") {
			counts["oom kills"]++
		}
		if (strings.Contains(line, "nvrm") || strings.Contains(line, "amdgpu") || strings.Contains(line, "nouveau") || strings.Contains(line, "gpu")) && (strings.Contains(line, "hang detected") || strings.Contains(line, "fault") || strings.Contains(line, "xid") || strings.Contains(line, "ring timeout")) {
			counts["GPU faults"]++
		}
		if strings.Contains(line, "i/o error") || strings.Contains(line, "blk_update_request") || strings.Contains(line, "buffer i/o") {
			counts["I/O errors"]++
		}
		if strings.Contains(line, "kernel panic") || strings.Contains(line, "not syncing:") {
			counts["kernel panics"]++
		}
		if (strings.Contains(line, "thermal") && (strings.Contains(line, "critical temperature") || strings.Contains(line, "thermal trip") || strings.Contains(line, "overheat") || strings.Contains(line, "throttl"))) || strings.Contains(line, "temperature above threshold") {
			counts["thermal events"]++
		}
	}
	var ev []string
	severe := false
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > 0 {
			severe = true
			ev = append(ev, fmt.Sprintf("%s: %d", k, counts[k]))
		}
	}
	if len(ev) == 0 {
		ev = []string{"no matching events in sampled journal window (up to 1500 most recent kernel lines)"}
	}
	if r.truncated {
		ev = append(ev, "command output truncated at safety limit; only captured sample assessed")
	}
	if o.IncludeSensitive && len(r.stdout) > 0 {
		raw := strings.TrimSpace(string(r.stdout))
		if len(raw) > 2048 {
			raw = raw[:2048]
		}
		ev = append(ev, "log excerpt: "+raw)
	}
	sev := "info"
	summary := "No matching events found in sampled " + label + " boot journal window"
	if severe {
		sev = "warning"
		summary = "Kernel events require review in " + label + " boot journal"
	}
	f := integrationFinding("journal-"+label, "logs", sev, summary, ev...)
	if severe {
		f.Recommendation = "Review the matching kernel events and correlate with the affected hardware or workload."
	}
	return []Finding{f}
}

func integrationServices(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, o.CommandTimeout, "systemctl", "--failed", "--no-legend", "--plain")
	if r.err != nil {
		return []Finding{integrationFinding("failed-units", "services", "unknown", "Failed service state unavailable")}
	}
	text := strings.TrimSpace(string(r.stdout))
	if text == "" || text == "0 loaded units listed." {
		return []Finding{integrationFinding("failed-units", "services", "ok", "No failed systemd units reported")}
	}
	lines := strings.Split(text, "\n")
	if !o.IncludeSensitive {
		for i, line := range lines {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				lines[i] = fields[0]
			}
		}
	}
	if len(lines) > 20 {
		lines = lines[:20]
	}
	f := integrationFinding("failed-units", "services", "warning", "Systemd reports failed units", lines...)
	f.Recommendation = "Inspect failed units and their status before attempting recovery."
	return []Finding{f}
}

func integrationBootTime(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, o.CommandTimeout, "systemd-analyze", "time")
	if r.err != nil {
		return []Finding{integrationFinding("boot-time", "boot", "unknown", "Boot timing unavailable")}
	}
	text := strings.TrimSpace(string(r.stdout))
	ev := []string{text, "Firmware time is pre-userspace platform startup; loader and userspace timings describe subsequent boot stages."}
	f := integrationFinding("boot-time", "boot", "info", "Systemd boot timing", ev...)
	if strings.Contains(text, "firmware") && strings.Contains(text, "userspace") {
		f.Recommendation = "Compare firmware and userspace durations to identify which boot stage dominates; investigate that stage if boot is unusually slow."
	}
	return []Finding{f}
}

func integrationGPU(ctx context.Context, o Options) []Finding {
	root := o.SysRoot
	if root == "" {
		root = "/sys"
	}
	entries, _ := filepath.Glob(filepath.Join(root, "bus/pci/devices/*"))
	var found []Finding
	for _, p := range entries {
		b, _ := os.ReadFile(filepath.Join(p, "class"))
		if !strings.HasPrefix(strings.TrimSpace(string(b)), "0x03") {
			continue
		}
		vendor, _ := os.ReadFile(filepath.Join(p, "vendor"))
		device, _ := os.ReadFile(filepath.Join(p, "device"))
		driver, _ := os.Readlink(filepath.Join(p, "driver"))
		v := strings.TrimSpace(string(vendor))
		d := strings.TrimSpace(string(device))
		drv := filepath.Base(driver)
		id := strings.ReplaceAll(filepath.Base(p), ":", "-")
		if v == "0x10de" && drv == "nouveau" {
			f := integrationFinding("gpu-nouveau-"+id, "gpu", "warning", "Nouveau driver detected on NVIDIA hardware; review compatibility, not evidence of a fault", fmt.Sprintf("PCI vendor/device %s:%s; driver %s", v, d, drv))
			f.Recommendation = "Review supported driver options for this GPU if graphics instability is observed."
			found = append(found, f)
		} else {
			if drv == "." || drv == "" {
				found = append(found, integrationFinding("gpu-driver-"+id, "gpu", "unknown", "GPU identified but active driver is unavailable", fmt.Sprintf("PCI vendor/device %s:%s", v, d)))
				continue
			}
			found = append(found, integrationFinding("gpu-device-"+id, "gpu", "info", "GPU device detected", fmt.Sprintf("PCI vendor/device %s:%s; driver %s", v, d, drv)))
		}
	}
	if len(found) == 0 {
		found = append(found, integrationFinding("gpu-device", "gpu", "unknown", "No GPU devices identified from sysfs"))
	}
	return found
}

func integrationPower(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, o.CommandTimeout, "powerprofilesctl", "get")
	if r.err != nil {
		return []Finding{integrationFinding("power-profile", "power", "unknown", "Power profile unavailable")}
	}
	return []Finding{integrationFinding("power-profile", "power", "info", "Current power profile", strings.TrimSpace(string(r.stdout)))}
}

func integrationDisks(ctx context.Context, o Options) []Finding {
	root := o.SysRoot
	if root == "" {
		root = "/sys"
	}
	blocks, _ := filepath.Glob(filepath.Join(root, "block/*"))
	var disks []string
	for _, p := range blocks {
		name := filepath.Base(p)
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") {
			continue
		}
		disks = append(disks, name)
	}
	if len(disks) == 0 {
		return []Finding{integrationFinding("disk-inventory", "storage", "unknown", "No non-loop block devices identified")}
	}
	sort.Strings(disks)
	var inventory []string
	for _, d := range disks {
		vendor, _ := os.ReadFile(filepath.Join(root, "block", d, "device/vendor"))
		model, _ := os.ReadFile(filepath.Join(root, "block", d, "device/model"))
		label := d
		id := strings.TrimSpace(string(vendor)) + " " + strings.TrimSpace(string(model))
		if strings.TrimSpace(id) != "" {
			label += ": " + strings.TrimSpace(id)
		}
		inventory = append(inventory, label)
	}
	res := []Finding{integrationFinding("disk-inventory", "storage", "info", "Block devices identified (no serial numbers collected)", inventory...)}
	if !o.Privileged || os.Geteuid() != 0 {
		return append(res, integrationFinding("disk-smart", "storage", "unknown", "SMART unavailable: requires Privileged option and effective root"))
	}
	if _, err := exec.LookPath("smartctl"); err != nil {
		return append(res, integrationFinding("disk-smart", "storage", "unknown", "SMART unavailable: smartctl not installed"))
	}
	for _, d := range disks {
		r := integrationRun(ctx, o.CommandTimeout, "smartctl", "-H", "/dev/"+d)
		sev, summary, recognized := integrationSMARTStatus(string(r.stdout))
		if recognized {
			f := integrationFinding("smart-"+d, "storage", sev, summary)
			if sev == "warning" {
				f.Recommendation = "Back up important data and arrange disk diagnostics or replacement."
			}
			res = append(res, f)
			continue
		}
		if r.err != nil {
			res = append(res, integrationFinding("smart-"+d, "storage", "unknown", "SMART status unavailable"))
			continue
		}
		res = append(res, integrationFinding("smart-"+d, "storage", "unknown", "SMART status unavailable or unrecognized"))
	}
	return res
}

func integrationSMARTStatus(output string) (severity, summary string, recognized bool) {
	for _, line := range strings.Split(strings.ToLower(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "smart overall-health self-assessment test result:") || strings.HasPrefix(line, "smart health status:") {
			value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if strings.HasPrefix(value, "failed") || strings.HasPrefix(value, "failure") {
				return "warning", "SMART reports failure", true
			}
			if strings.HasPrefix(value, "passed") || strings.HasPrefix(value, "ok") {
				return "ok", "SMART reports passed", true
			}
		}
	}
	return "unknown", "SMART status unavailable or unrecognized", false
}

// Doctor reports platform, filesystem and optional-tool availability without running diagnostics.
func Doctor(ctx context.Context, o Options) []Check {
	etc := o.EtcRoot
	if etc == "" {
		etc = "/etc"
	}
	distro, _ := parseOSRelease(filepath.Join(etc, "os-release"))
	adapter := selectDistroAdapter(distro["ID"])
	checks := []Check{{Name: "linux", Available: runtime.GOOS == "linux", Detail: runtime.GOOS}, {Name: "distro", Available: true, Detail: adapter.Name()}}
	for _, item := range []struct{ name, root string }{{"procfs", o.ProcRoot}, {"sysfs", o.SysRoot}} {
		root := item.root
		if root == "" {
			root = "/" + strings.TrimSuffix(item.name, "fs")
		}
		f, err := os.Open(root)
		if err == nil {
			_ = f.Close()
		}
		checks = append(checks, Check{Name: item.name, Available: err == nil, Detail: root})
	}
	for _, tool := range []string{"journalctl", "systemctl", "systemd-analyze", "powerprofilesctl", "sensors", "lspci", "smartctl"} {
		_, err := exec.LookPath(tool)
		detail := map[bool]string{true: tool, false: "unavailable"}[err == nil]
		if err != nil {
			if pkg := adapter.Package(tool); pkg != "" {
				detail += "; optional package: " + pkg
			}
		}
		checks = append(checks, Check{Name: tool, Available: err == nil, Detail: detail})
	}
	checks = append(checks, Check{Name: "privileged-smart", Available: os.Geteuid() == 0, Detail: fmt.Sprintf("effective uid %d", os.Geteuid())})
	_ = ctx
	return checks
}
