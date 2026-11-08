package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
)

type diskFormat string

const (
	diskFormatText diskFormat = "text"
	diskFormatJSON diskFormat = "json"
	diskUsage                 = `Usage: daffy disk [scan] [path] [--format text|json] [--json] [--progress] [--exclude PATH]... [--time-budget duration] [--top N]
Scans eligible local mounts by default. Time budget defaults to 0 (no limit).
Use --progress for occasional status on stderr. Recommendations describe observed
data; they do not guarantee reclaimable space.`
	optionDiskFormat   = "format"
	optionDiskJSON     = "json"
	optionDiskExclude  = "exclude"
	optionDiskBudget   = "time-budget"
	optionDiskTop      = "top"
	optionDiskProgress = "progress"
)

type diskOptions struct {
	format   diskFormat
	excludes []string
	budget   time.Duration
	top      int
	progress bool
}

// diskFlags accepts options interspersed with the optional positional root.
func diskFlags(args []string) ([]string, diskOptions, error) {
	c := diskOptions{format: diskFormatText, top: 20}
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
			continue
		}
		key, val, has := strings.Cut(strings.TrimLeft(a, "-"), "=")
		need := func() (string, error) {
			if has {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", a)
			}
			i++
			return args[i], nil
		}
		switch key {
		case optionDiskProgress:
			if has {
				return nil, c, fmt.Errorf("--progress does not accept a value")
			}
			c.progress = true
		case optionDiskJSON:
			if has {
				return nil, c, fmt.Errorf("--json does not accept a value")
			}
			c.format = diskFormatJSON
		case optionDiskFormat:
			v, e := need()
			if e != nil {
				return nil, c, e
			}
			c.format = diskFormat(v)
		case optionDiskExclude:
			v, e := need()
			if e != nil {
				return nil, c, e
			}
			c.excludes = append(c.excludes, v)
		case optionDiskBudget:
			v, e := need()
			if e != nil {
				return nil, c, e
			}
			d, e := time.ParseDuration(v)
			if e != nil || d < 0 {
				return nil, c, fmt.Errorf("time-budget must be zero or a positive duration")
			}
			c.budget = d
		case optionDiskTop:
			v, e := need()
			if e != nil {
				return nil, c, e
			}
			c.top, e = strconv.Atoi(v)
			if e != nil || c.top <= 0 || c.top > disk.MaxTopN {
				return nil, c, fmt.Errorf("top must be an integer from 1 to %d", disk.MaxTopN)
			}
		default:
			return nil, c, fmt.Errorf("unknown disk option %q", a)
		}
	}
	if c.format != diskFormatText && c.format != diskFormatJSON {
		return nil, c, fmt.Errorf("format must be text or json")
	}
	if len(positionals) > 1 {
		return nil, c, fmt.Errorf("disk scan accepts one path")
	}
	return positionals, c, nil
}

func runDisk(ctx context.Context, args []string, out, errOut io.Writer, deps Deps) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(out, diskUsage)
		return int(exitSuccess)
	}
	if len(args) > 0 && command(args[0]) == commandScan {
		args = args[1:]
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" {
			fmt.Fprintln(out, diskUsage)
			return int(exitSuccess)
		}
	}
	paths, o, e := diskFlags(args)
	if e != nil {
		fmt.Fprintf(errOut, "%v\n%s\n", e, diskUsage)
		return int(exitUsage)
	}
	if deps.Disk == nil {
		fmt.Fprintln(errOut, "disk scanner unavailable")
		return int(exitFailure)
	}
	path := ""
	if len(paths) > 0 {
		path = paths[0]
	}
	if o.format == diskFormatText {
		label := path
		if label == "" {
			label = "the whole machine"
		}
		if _, e = fmt.Fprintf(errOut, "Scanning %s (read-only)…\n", safe(label)); e != nil {
			return failed(errOut, e)
		}
	}
	config := disk.Config{Path: path, Excludes: o.excludes, TopN: o.top, TimeBudget: o.budget}
	var progress diskProgressWriter
	if o.progress {
		progress.out = errOut
		config.OnProgress = progress.update
	}
	r, e := deps.Disk.Scan(ctx, config)
	if progress.err != nil {
		return failed(errOut, progress.err)
	}
	if e != nil && !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded) {
		return failed(errOut, e)
	}
	if o.format == diskFormatJSON {
		if x := json.NewEncoder(out).Encode(r); x != nil {
			return failed(errOut, x)
		}
	} else if x := printDisk(out, r); x != nil {
		return failed(errOut, x)
	}
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return int(exitInterrupted)
		}
		return failed(errOut, e)
	}
	return int(exitSuccess)
}

func printDisk(w io.Writer, r disk.Report) error {
	if _, e := fmt.Fprintf(w, "DISK SCAN — %s\nAllocated: %s   Apparent: %s\n", safe(r.Root), bytesText(r.Totals.AllocatedBytes), bytesText(r.Totals.ApparentBytes)); e != nil {
		return e
	}
	if _, e := fmt.Fprintf(w, "\nCOVERAGE\nMounts: %d/%d scanned · %d skipped; entries: %d skipped · %d errors omitted · complete: %t\n", r.Coverage.ScannedMounts, r.Coverage.Mounts, r.Coverage.SkippedMounts, r.Coverage.SkippedEntries, r.Coverage.OmittedErrors, r.Coverage.Complete); e != nil {
		return e
	}
	if !r.Coverage.Complete || r.Coverage.SkippedEntries > 0 || r.Coverage.OmittedErrors > 0 {
		if _, e := fmt.Fprintln(w, "Results are partial; unscanned locations may contain larger items."); e != nil {
			return e
		}
	}
	if _, e := fmt.Fprintln(w, "\nMOUNT CAPACITY"); e != nil {
		return e
	}
	for _, m := range r.Mounts {
		used := uint64(0)
		valid := m.TotalBytes > 0 && m.AvailableBytes <= m.TotalBytes
		if valid {
			used = m.TotalBytes - m.AvailableBytes
		}
		if _, e := fmt.Fprintf(w, "%s\n  Space: %s total, %s available (%s) %s\n  Inodes: %s\n", safe(m.Path), bytesTextU(m.TotalBytes), bytesTextU(m.AvailableBytes), percent(used, m.TotalBytes, valid), bar(used, m.TotalBytes, valid), inodeUsage(m.FreeInodes, m.TotalInodes)); e != nil {
			return e
		}
	}
	if _, e := fmt.Fprintln(w, "  Bar shows used space (#); - is remaining capacity."); e != nil {
		return e
	}
	if _, e := fmt.Fprintln(w, "Capacity is filesystem-wide, even for a scoped scan. Complete means the selected eligible scope was scanned, not the whole host."); e != nil {
		return e
	}
	if _, e := fmt.Fprintln(w, "Capacity can differ from scan totals due to snapshots, shared blocks, deleted-open files, or metadata. That alone does not prove a scan problem."); e != nil {
		return e
	}
	firstFinding := true
	for _, d := range disk.AnalyzeDiagnostics(r) {
		if d.Severity != disk.SeverityWarning && d.Severity != disk.SeverityCritical {
			continue
		}
		if firstFinding {
			if _, e := fmt.Fprintln(w, "\nDIAGNOSTICS"); e != nil {
				return e
			}
			firstFinding = false
		}
		location := d.Mount
		if location == "" {
			location = d.Path
		}
		if _, e := fmt.Fprintf(w, "  %s %s · %s: %s\n", safe(string(d.Severity)), safe(string(d.Code)), safe(location), safe(d.Message)); e != nil {
			return e
		}
		if d.Recommendation != "" {
			if _, e := fmt.Fprintf(w, "    Next: %s\n", safe(d.Recommendation)); e != nil {
				return e
			}
		}
	}
	if _, e := fmt.Fprintln(w, "\nLARGEST FILES (allocated bytes)\n  #  SIZE       PATH"); e != nil {
		return e
	}
	for n, f := range r.TopFiles {
		if _, e := fmt.Fprintf(w, "  %2d %-10s %s\n", n+1, bytesText(f.AllocatedBytes), safe(f.Path)); e != nil {
			return e
		}
	}
	if _, e := fmt.Fprintln(w, "\nLARGEST DIRECTORIES (allocated bytes; parent and child sizes overlap)\n  #  SIZE       PATH"); e != nil {
		return e
	}
	for n, d := range r.TopDirectories {
		if _, e := fmt.Fprintf(w, "  %2d %-10s %s\n", n+1, bytesText(d.AllocatedBytes), safe(d.Path)); e != nil {
			return e
		}
	}
	if _, e := fmt.Fprintln(w, "Allocated bytes are blocks attributed to scanned entries, not guaranteed reclaimable space."); e != nil {
		return e
	}
	if len(r.Candidates) > 0 {
		if _, e := fmt.Fprintln(w, "\nRECOMMENDATIONS (review before acting; no changes made)"); e != nil {
			return e
		}
	}
	for _, c := range r.Candidates {
		if _, e := fmt.Fprintf(w, "Suggestion %s [%s, %s risk] · %s observed (not guaranteed reclaimable): %s — %s\n", safe(c.Path), safe(string(c.Category)), safe(string(c.Risk)), bytesText(c.ObservedAllocatedBytes), safe(c.Recommendation), safe(c.Explanation)); e != nil {
			return e
		}
	}
	if len(r.Issues) > 0 {
		if _, e := fmt.Fprintln(w, "\nSCAN ISSUES"); e != nil {
			return e
		}
	}
	for _, i := range r.Issues {
		if _, e := fmt.Fprintf(w, "  %s %s: %s\n", safe(string(i.Operation)), safe(i.Path), safe(i.Error)); e != nil {
			return e
		}
	}
	return nil
}

func bytesTextU(v uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	n := float64(v)
	u := 0
	for n >= 1024 && u < len(units)-1 {
		n /= 1024
		u++
	}
	if u == 0 {
		return fmt.Sprintf("%d B", v)
	}
	return fmt.Sprintf("%.1f %s", n, units[u])
}
func ratioPercent(n, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(float64(n) / float64(total) * 100)
}
func percent(used, total uint64, valid bool) string {
	if !valid {
		return "unknown"
	}
	return fmt.Sprintf("%d%% used", ratioPercent(used, total))
}
func bar(used, total uint64, valid bool) string {
	if !valid {
		return "[ capacity unknown ]"
	}
	n := 0
	n = ratioPercent(used, total) * 12 / 100
	if n > 12 {
		n = 12
	}
	return "[" + strings.Repeat("#", n) + strings.Repeat("-", 12-n) + "]"
}
func inodeUsage(free, total uint64) string {
	if total == 0 || free > total {
		return "unknown"
	}
	return fmt.Sprintf("%d%% free", ratioPercent(free, total))
}
func bytesText(v int64) string {
	if v < 0 {
		return "unknown"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	n := float64(v)
	u := 0
	for n >= 1024 && u < len(units)-1 {
		n /= 1024
		u++
	}
	if u == 0 {
		return fmt.Sprintf("%d B", v)
	}
	return fmt.Sprintf("%.1f %s", n, units[u])
}
