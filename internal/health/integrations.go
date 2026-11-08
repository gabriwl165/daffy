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

	"github.com/gabriwl165/daffy-duck/internal/distribution"
)

const integrationOutputLimit = 256 * 1024

const (
	findingJournalCurrent  = "journal-current"
	findingJournalPrevious = "journal-previous"
	findingFailedUnits     = "failed-units"
	findingBootTime        = "boot-time"
	findingGPUDevice       = "gpu-device"
	findingPowerProfile    = "power-profile"
	findingDiskInventory   = "disk-inventory"
	findingDiskSMART       = "disk-smart"
)

type journalEventCounts struct{ gpuFaults, ioErrors, kernelPanics, oomKills, thermalEvents int }

var journalEventOrder = []struct {
	label string
	count func(journalEventCounts) int
}{
	{"GPU faults", func(c journalEventCounts) int { return c.gpuFaults }},
	{"I/O errors", func(c journalEventCounts) int { return c.ioErrors }},
	{"kernel panics", func(c journalEventCounts) int { return c.kernelPanics }},
	{"oom kills", func(c journalEventCounts) int { return c.oomKills }},
	{"thermal events", func(c journalEventCounts) int { return c.thermalEvents }},
}

type integrationResult struct {
	stdout, stderr []byte
	err            error
	truncated      bool
}

type integrationExecutor func(context.Context, commandConfig) integrationResult

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

func integrationExec(parent context.Context, cfg commandConfig) integrationResult {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, string(cfg.Tool), cfg.Args...)
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

func integrationFinding(f Finding) Finding {
	f.Confidence = ConfidenceMedium
	return f
}

// Collect augments core procfs/sysfs signals with bounded, read-only system tools.
func Collect(ctx context.Context, opts Options) Report {
	started := time.Now()
	r := CollectCore(ctx, opts)
	var fs []Finding
	if ctx.Err() == nil {
		fs = append(fs, integrationJournal(ctx, opts, journalCurrent)...)
	}
	if ctx.Err() == nil {
		fs = append(fs, integrationJournal(ctx, opts, journalPrevious)...)
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

func integrationJournal(ctx context.Context, o Options, boot journalBoot) []Finding {
	cfg, label := boot.config(o.CommandTimeout)
	r := integrationRun(ctx, cfg)
	if r.err != nil {
		sev, detail := SeverityUnknown, "journalctl unavailable or kernel journal unreadable"
		if boot == journalPrevious && strings.Contains(strings.ToLower(string(r.stderr)), "no journal files") {
			detail = "previous boot journal unavailable"
		}
		f := integrationFinding(Finding{ID: journalFindingID(boot), Area: AreaLogs, Severity: sev, Summary: detail})
		f.Recommendation = "Check journal permissions and availability; absence of readable logs is not evidence of health."
		return []Finding{f}
	}
	if strings.TrimSpace(string(r.stderr)) != "" || strings.TrimSpace(string(r.stdout)) == "" {
		f := integrationFinding(Finding{ID: journalFindingID(boot), Area: AreaLogs, Severity: SeverityUnknown, Summary: "Kernel journal read was empty or reported diagnostics; matching events cannot be ruled out"})
		f.Recommendation = "Check journal permissions and whether this boot has retained kernel logs."
		return []Finding{f}
	}
	var counts journalEventCounts
	for _, line := range strings.Split(strings.ToLower(string(r.stdout)), "\n") {
		if strings.Contains(line, "oom-kill") || strings.Contains(line, "killed process") || strings.Contains(line, "out of memory: kill process") {
			counts.oomKills++
		}
		if (strings.Contains(line, "nvrm") || strings.Contains(line, "amdgpu") || strings.Contains(line, "nouveau") || strings.Contains(line, "gpu")) && (strings.Contains(line, "hang detected") || strings.Contains(line, "fault") || strings.Contains(line, "xid") || strings.Contains(line, "ring timeout")) {
			counts.gpuFaults++
		}
		if strings.Contains(line, "i/o error") || strings.Contains(line, "blk_update_request") || strings.Contains(line, "buffer i/o") {
			counts.ioErrors++
		}
		if strings.Contains(line, "kernel panic") || strings.Contains(line, "not syncing:") {
			counts.kernelPanics++
		}
		if (strings.Contains(line, "thermal") && (strings.Contains(line, "critical temperature") || strings.Contains(line, "thermal trip") || strings.Contains(line, "overheat") || strings.Contains(line, "throttl"))) || strings.Contains(line, "temperature above threshold") {
			counts.thermalEvents++
		}
	}
	var ev []string
	severe := false
	for _, event := range journalEventOrder {
		if n := event.count(counts); n > 0 {
			severe = true
			ev = append(ev, fmt.Sprintf("%s: %d", event.label, n))
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
	sev := SeverityInfo
	summary := "No matching events found in sampled " + label + " boot journal window"
	if severe {
		sev = SeverityWarning
		summary = "Kernel events require review in " + label + " boot journal"
	}
	f := integrationFinding(Finding{ID: journalFindingID(boot), Area: AreaLogs, Severity: sev, Summary: summary, Evidence: ev})
	if severe {
		f.Recommendation = "Review the matching kernel events and correlate with the affected hardware or workload."
	}
	return []Finding{f}
}

func integrationServices(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, commandConfig{Timeout: o.CommandTimeout, Tool: distribution.Systemctl, Args: []string{"--failed", "--no-legend", "--plain"}})
	if r.err != nil {
		return []Finding{integrationFinding(Finding{ID: findingFailedUnits, Area: AreaServices, Severity: SeverityUnknown, Summary: "Failed service state unavailable"})}
	}
	text := strings.TrimSpace(string(r.stdout))
	if text == "" || text == "0 loaded units listed." {
		return []Finding{integrationFinding(Finding{ID: findingFailedUnits, Area: AreaServices, Severity: SeverityOK, Summary: "No failed systemd units reported"})}
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
	f := integrationFinding(Finding{ID: findingFailedUnits, Area: AreaServices, Severity: SeverityWarning, Summary: "Systemd reports failed units", Evidence: lines})
	f.Recommendation = "Inspect failed units and their status before attempting recovery."
	return []Finding{f}
}

func integrationBootTime(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, commandConfig{Timeout: o.CommandTimeout, Tool: distribution.SystemdAnalyze, Args: []string{"time"}})
	if r.err != nil {
		return []Finding{integrationFinding(Finding{ID: findingBootTime, Area: AreaBoot, Severity: SeverityUnknown, Summary: "Boot timing unavailable"})}
	}
	text := strings.TrimSpace(string(r.stdout))
	ev := []string{text, "Firmware time is pre-userspace platform startup; loader and userspace timings describe subsequent boot stages."}
	f := integrationFinding(Finding{ID: findingBootTime, Area: AreaBoot, Severity: SeverityInfo, Summary: "Systemd boot timing", Evidence: ev})
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
			f := integrationFinding(Finding{ID: "gpu-nouveau-" + id, Area: AreaGPU, Severity: SeverityWarning, Summary: "Nouveau driver detected on NVIDIA hardware; review compatibility, not evidence of a fault", Evidence: []string{fmt.Sprintf("PCI vendor/device %s:%s; driver %s", v, d, drv)}})
			f.Recommendation = "Review supported driver options for this GPU if graphics instability is observed."
			found = append(found, f)
		} else {
			if drv == "." || drv == "" {
				found = append(found, integrationFinding(Finding{ID: "gpu-driver-" + id, Area: AreaGPU, Severity: SeverityUnknown, Summary: "GPU identified but active driver is unavailable", Evidence: []string{fmt.Sprintf("PCI vendor/device %s:%s", v, d)}}))
				continue
			}
			found = append(found, integrationFinding(Finding{ID: "gpu-device-" + id, Area: AreaGPU, Severity: SeverityInfo, Summary: "GPU device detected", Evidence: []string{fmt.Sprintf("PCI vendor/device %s:%s; driver %s", v, d, drv)}}))
		}
	}
	if len(found) == 0 {
		found = append(found, integrationFinding(Finding{ID: findingGPUDevice, Area: AreaGPU, Severity: SeverityUnknown, Summary: "No GPU devices identified from sysfs"}))
	}
	return found
}

func integrationPower(ctx context.Context, o Options) []Finding {
	r := integrationRun(ctx, commandConfig{Timeout: o.CommandTimeout, Tool: distribution.Powerprofilesctl, Args: []string{"get"}})
	if r.err != nil {
		return []Finding{integrationFinding(Finding{ID: findingPowerProfile, Area: AreaPower, Severity: SeverityUnknown, Summary: "Power profile unavailable"})}
	}
	return []Finding{integrationFinding(Finding{ID: findingPowerProfile, Area: AreaPower, Severity: SeverityInfo, Summary: "Current power profile", Evidence: []string{strings.TrimSpace(string(r.stdout))}})}
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
		return []Finding{integrationFinding(Finding{ID: findingDiskInventory, Area: AreaStorage, Severity: SeverityUnknown, Summary: "No non-loop block devices identified"})}
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
	res := []Finding{integrationFinding(Finding{ID: findingDiskInventory, Area: AreaStorage, Severity: SeverityInfo, Summary: "Block devices identified (no serial numbers collected)", Evidence: inventory})}
	if !o.Privileged || os.Geteuid() != 0 {
		return append(res, integrationFinding(Finding{ID: findingDiskSMART, Area: AreaStorage, Severity: SeverityUnknown, Summary: "SMART unavailable: requires Privileged option and effective root"}))
	}
	if _, err := exec.LookPath(string(distribution.Smartctl)); err != nil {
		return append(res, integrationFinding(Finding{ID: findingDiskSMART, Area: AreaStorage, Severity: SeverityUnknown, Summary: "SMART unavailable: smartctl not installed"}))
	}
	for _, d := range disks {
		r := integrationRun(ctx, commandConfig{Timeout: o.CommandTimeout, Tool: distribution.Smartctl, Args: []string{"-H", "/dev/" + d}})
		sev, summary, recognized := integrationSMARTStatus(string(r.stdout))
		if recognized {
			f := integrationFinding(Finding{ID: "smart-" + d, Area: AreaStorage, Severity: sev, Summary: summary})
			if sev == SeverityWarning {
				f.Recommendation = "Back up important data and arrange disk diagnostics or replacement."
			}
			res = append(res, f)
			continue
		}
		if r.err != nil {
			res = append(res, integrationFinding(Finding{ID: "smart-" + d, Area: AreaStorage, Severity: SeverityUnknown, Summary: "SMART status unavailable"}))
			continue
		}
		res = append(res, integrationFinding(Finding{ID: "smart-" + d, Area: AreaStorage, Severity: SeverityUnknown, Summary: "SMART status unavailable or unrecognized"}))
	}
	return res
}

func integrationSMARTStatus(output string) (severity Severity, summary string, recognized bool) {
	for _, line := range strings.Split(strings.ToLower(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "smart overall-health self-assessment test result:") || strings.HasPrefix(line, "smart health status:") {
			value := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if strings.HasPrefix(value, "failed") || strings.HasPrefix(value, "failure") {
				return SeverityWarning, "SMART reports failure", true
			}
			if strings.HasPrefix(value, "passed") || strings.HasPrefix(value, "ok") {
				return SeverityOK, "SMART reports passed", true
			}
		}
	}
	return SeverityUnknown, "SMART status unavailable or unrecognized", false
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
	for _, tool := range []distribution.Tool{distribution.Journalctl, distribution.Systemctl, distribution.SystemdAnalyze, distribution.Powerprofilesctl, distribution.Sensors, distribution.Lspci, distribution.Smartctl} {
		toolName := string(tool)
		_, err := exec.LookPath(toolName)
		detail := "unavailable"
		if err == nil {
			detail = toolName
		}
		if err != nil {
			if pkg := adapter.Package(toolName); pkg != "" {
				detail += "; optional package: " + pkg
			}
		}
		checks = append(checks, Check{Name: toolName, Available: err == nil, Detail: detail})
	}
	checks = append(checks, Check{Name: "privileged-smart", Available: os.Geteuid() == 0, Detail: fmt.Sprintf("effective uid %d", os.Geteuid())})
	_ = ctx
	return checks
}
