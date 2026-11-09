package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const (
	pluginCommand             = "plugins"
	pluginListCommand         = "list"
	pluginRunCommand          = "run"
	pluginFlagJSON            = "json"
	pluginFlagTimeout         = "timeout"
	pluginFlagPath            = "path"
	pluginFlagTop             = "top"
	pluginFlagExclude         = "exclude"
	pluginFlagContext         = "context"
	pluginFlagContainer       = "container"
	pluginFlagProject         = "project"
	pluginFlagSamples         = "samples"
	pluginFlagInterval        = "interval"
	pluginFlagRuntimeTop      = "runtime-top"
	pluginFlagCPUThreshold    = "cpu-threshold"
	pluginFlagMemoryThreshold = "memory-threshold"
	pluginFlagDetails         = "details"
	pluginFlagVerbose         = "verbose"
	pluginDefaultTimeout      = 15 * time.Second
	pluginDefaultTop          = 20
	pluginMaximumTop          = 10000
)

const pluginUsage = `Usage: daffy plugins list [--json]
        daffy plugins run docker [disk|runtime|config] [--details] [--verbose] [--samples N] [--interval duration] [--runtime-top N] [--cpu-threshold percent] [--memory-threshold percent] [--json] [--timeout duration]
       daffy plugins run bazel [disk] --path PATH [--exclude PATH ...] [--top N] [--json] [--timeout duration]
`

const dockerAliasUsage = `Usage: daffy docker [disk|runtime|config] [--details] [--verbose] [--samples N] [--interval duration] [--runtime-top N] [--cpu-threshold percent] [--memory-threshold percent] [--json] [--timeout duration]
`
const bazelAliasUsage = `Usage: daffy bazel disk --path PATH [--exclude PATH ...] [--top N] [--json] [--timeout duration]
`

func runPlugins(ctx context.Context, args []string, out, errOut io.Writer, registry *plugins.Registry) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		if _, e := fmt.Fprint(out, pluginUsage); e != nil {
			return failed(errOut, e)
		}
		return 0
	}
	if args[0] == pluginListCommand {
		return listPlugins(args[1:], out, errOut, registry)
	}
	if args[0] != pluginRunCommand {
		fmt.Fprintf(errOut, "unknown plugins command %q\n%s", safe(args[0]), pluginUsage)
		return int(exitUsage)
	}
	return runPlugin(ctx, args[1:], out, errOut, registry)
}

func listPlugins(args []string, out, errOut io.Writer, r *plugins.Registry) int {
	fs := flag.NewFlagSet("daffy plugins list", flag.ContinueOnError)
	var diagnostics bytes.Buffer
	fs.SetOutput(&diagnostics)
	asJSON := fs.Bool(pluginFlagJSON, false, "emit JSON")
	fs.Usage = func() { fmt.Fprint(&diagnostics, pluginUsage) }
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			if _, werr := fmt.Fprint(out, pluginUsage); werr != nil {
				return failed(errOut, werr)
			}
			return 0
		}
		if _, werr := fmt.Fprint(errOut, safe(diagnostics.String())); werr != nil {
			return int(exitFailure)
		}
		return int(exitUsage)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(errOut, "unexpected arguments: %s\n", safe(strings.Join(fs.Args(), " ")))
		return int(exitUsage)
	}
	if r == nil {
		return failed(errOut, errors.New("plugin registry unavailable"))
	}
	if *asJSON {
		if e := json.NewEncoder(out).Encode(r.List()); e != nil {
			return failed(errOut, e)
		}
		return 0
	}
	for _, d := range r.List() {
		if _, e := fmt.Fprintf(out, "%s\n", safe(string(d.ID))); e != nil {
			return failed(errOut, e)
		}
		for _, op := range d.Operations {
			if _, e := fmt.Fprintf(out, "  %s: %s\n", safe(string(op.ID)), safe(op.Summary)); e != nil {
				return failed(errOut, e)
			}
		}
	}
	return 0
}

func runPlugin(ctx context.Context, args []string, out, errOut io.Writer, r *plugins.Registry) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		if _, e := fmt.Fprint(out, pluginUsage); e != nil {
			return failed(errOut, e)
		}
		return 0
	}
	name := args[0]
	args = args[1:]
	id := plugins.PluginID(name)
	var operation plugins.OperationID
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		operation = plugins.OperationID(args[0])
		args = args[1:]
	}
	fs := flag.NewFlagSet("daffy plugins run "+name, flag.ContinueOnError)
	var diagnostics bytes.Buffer
	fs.SetOutput(&diagnostics)
	asJSON := fs.Bool(pluginFlagJSON, false, "emit JSON")
	verbose := fs.Bool(pluginFlagVerbose, false, "show full text details")
	timeout := fs.Duration(pluginFlagTimeout, pluginDefaultTimeout, "per-operation timeout (bundle operations run sequentially)")
	path := fs.String(pluginFlagPath, "", "explicit output or cache directory")
	top := fs.Int(pluginFlagTop, pluginDefaultTop, "top entries")
	samples := fs.Int(pluginFlagSamples, 1, "runtime samples (1-60)")
	interval := fs.Duration(pluginFlagInterval, 500*time.Millisecond, "runtime sample interval (100ms-1m)")
	runtimeTop := fs.Int(pluginFlagRuntimeTop, 0, "number of ranked runtime containers")
	cpuThreshold := fs.Float64(pluginFlagCPUThreshold, 0, "CPU peak percentage threshold")
	memoryThreshold := fs.Float64(pluginFlagMemoryThreshold, 0, "memory peak percentage threshold")
	details := fs.Bool(pluginFlagDetails, false, "include typed Docker disk object breakdown")
	var excludes stringsFlag
	fs.Var(&excludes, pluginFlagExclude, "exclude path (repeatable)")
	var containers stringsFlag
	fs.Var(&containers, pluginFlagContainer, "select container name or ID (repeatable)")
	contextName := fs.String(pluginFlagContext, "", "Docker CLI context")
	projectName := fs.String(pluginFlagProject, "", "Compose project")
	fs.Usage = func() { fmt.Fprint(&diagnostics, pluginUsage) }
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			if _, werr := fmt.Fprint(out, pluginUsage); werr != nil {
				return failed(errOut, werr)
			}
			return 0
		}
		if _, werr := fmt.Fprint(errOut, safe(diagnostics.String())); werr != nil {
			return int(exitFailure)
		}
		return int(exitUsage)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(errOut, "unexpected arguments: %s\n", safe(strings.Join(fs.Args(), " ")))
		return int(exitUsage)
	}
	var specified struct{ Path, Top, Exclude, Context, Container, Project, Samples, Interval, RuntimeTop, CPU, Memory, Details bool }
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case pluginFlagPath:
			specified.Path = true
		case pluginFlagTop:
			specified.Top = true
		case pluginFlagExclude:
			specified.Exclude = true
		case pluginFlagContext:
			specified.Context = true
		case pluginFlagContainer:
			specified.Container = true
		case pluginFlagProject:
			specified.Project = true
		case pluginFlagSamples:
			specified.Samples = true
		case pluginFlagInterval:
			specified.Interval = true
		case pluginFlagRuntimeTop:
			specified.RuntimeTop = true
		case pluginFlagCPUThreshold:
			specified.CPU = true
		case pluginFlagMemoryThreshold:
			specified.Memory = true
		case pluginFlagDetails:
			specified.Details = true
		}
	})
	if *timeout <= 0 {
		fmt.Fprintln(errOut, "timeout must be positive")
		return int(exitUsage)
	}
	if r == nil {
		return failed(errOut, errors.New("plugin registry unavailable"))
	}
	descriptor, ok := r.Lookup(id)
	if !ok {
		fmt.Fprintf(errOut, "unknown plugin %q\n", safe(name))
		return int(exitUsage)
	}
	selected := make([]plugins.Operation, 0)
	if operation != "" {
		for _, op := range descriptor.Operations {
			if op.ID == operation {
				selected = append(selected, op)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintf(errOut, "unknown operation %q\n", safe(string(operation)))
			return int(exitUsage)
		}
	} else {
		for _, op := range descriptor.Operations {
			if op.Default {
				selected = append(selected, op)
			}
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(errOut, "plugin has no default operations")
		return int(exitUsage)
	}
	for _, op := range selected {
		trendSpecified := specified.Samples || specified.Interval || specified.RuntimeTop || specified.CPU || specified.Memory
		if specified.Path && !op.Options.Path || specified.Exclude && !op.Options.Excludes || specified.Top && !op.Options.TopN || specified.Context && (id != plugins.PluginDocker || !op.Options.Context) || specified.Container && (id != plugins.PluginDocker || !op.Options.Containers || op.ID == plugins.OperationDisk) || specified.Project && (id != plugins.PluginDocker || !op.Options.Project || op.ID == plugins.OperationDisk) || trendSpecified && !op.Options.RuntimeTrends || specified.Details && !op.Options.DiskDetails || op.Options.RequirePath && *path == "" {
			fmt.Fprintf(errOut, "invalid or unsupported options for %q\n", safe(string(op.ID)))
			return int(exitUsage)
		}
	}
	if *top < 0 || *top > pluginMaximumTop {
		fmt.Fprintf(errOut, "top must be between 0 and %d\n", pluginMaximumTop)
		return int(exitUsage)
	}
	if *samples < 1 || *samples > 60 || *interval < 100*time.Millisecond || *interval > time.Minute || *runtimeTop < 0 || *runtimeTop > 100 || math.IsNaN(*cpuThreshold) || math.IsInf(*cpuThreshold, 0) || *cpuThreshold < 0 || math.IsNaN(*memoryThreshold) || math.IsInf(*memoryThreshold, 0) || *memoryThreshold < 0 || *memoryThreshold > 100 {
		fmt.Fprintln(errOut, "invalid runtime sampling options")
		return int(exitUsage)
	}
	report, err := r.RunWithTimeout(ctx, id, plugins.Request{Operation: operation, Options: plugins.Options{Path: *path, Excludes: excludes, TopN: func() int {
		for _, op := range selected {
			if op.Options.TopN {
				return *top
			}
		}
		return 0
	}(), Context: *contextName, Containers: containers, Project: *projectName, Samples: func() int {
		if specified.Samples || specified.Interval || specified.RuntimeTop || specified.CPU || specified.Memory {
			return *samples
		}
		return 0
	}(), Interval: func() time.Duration {
		if specified.Samples || specified.Interval || specified.RuntimeTop || specified.CPU || specified.Memory {
			return *interval
		}
		return 0
	}(), RuntimeTop: *runtimeTop, CPUThreshold: *cpuThreshold, MemoryThreshold: *memoryThreshold, DiskDetails: *details}}, *timeout)
	if err != nil && (errors.Is(err, plugins.ErrInvalid) || errors.Is(err, plugins.ErrUnknownPlugin) || errors.Is(err, plugins.ErrUnknownOperation)) {
		if _, werr := fmt.Fprintf(errOut, "%s\n", safe(err.Error())); werr != nil {
			return int(exitFailure)
		}
		return int(exitUsage)
	}
	if *asJSON {
		if e := json.NewEncoder(out).Encode(report); e != nil {
			return failed(errOut, e)
		}
	} else if e := printPluginReport(out, report, *verbose); e != nil {
		return failed(errOut, e)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return int(exitInterrupted)
		}
		return failed(errOut, err)
	}
	if e := ctx.Err(); e != nil {
		if errors.Is(e, context.Canceled) {
			return int(exitInterrupted)
		}
		return failed(errOut, e)
	}
	if report.Status != plugins.StatusComplete {
		return failed(errOut, fmt.Errorf("plugin report status %q", safe(string(report.Status))))
	}
	return 0
}

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

func printPluginReport(w io.Writer, r plugins.Report, verbose bool) error {
	if r.Plugin == plugins.PluginDocker && !verbose {
		return printDockerSummary(w, r)
	}
	if _, e := fmt.Fprintf(w, "%s · %s\n", safe(string(r.Plugin)), safe(string(r.Status))); e != nil {
		return e
	}
	for _, o := range r.Outcomes {
		if _, e := fmt.Fprintf(w, "\n%s: %s\n", safe(string(o.Operation)), safe(string(o.Status))); e != nil {
			return e
		}
		for _, f := range o.Findings {
			if _, e := fmt.Fprintf(w, "  [%s] %s\n", safe(string(f.Severity)), safe(f.Summary)); e != nil {
				return e
			}
			if f.Recommendation != "" {
				if _, e := fmt.Fprintf(w, "    Recommendation: %s\n", safe(f.Recommendation)); e != nil {
					return e
				}
			}
		}
		for _, i := range o.Issues {
			if _, e := fmt.Fprintf(w, "  Issue: %s\n", safe(i.Summary)); e != nil {
				return e
			}
		}
		if d := o.Details.Docker; d != nil {
			if d.Context != "" {
				if _, e := fmt.Fprintf(w, "  Context: %s\n", safe(d.Context)); e != nil {
					return e
				}
			}
			for _, v := range d.DiskUsage {
				if _, e := fmt.Fprintf(w, "  %s: %d total, %d active, %s (reclaimable %s)\n", safe(v.Category), v.TotalCount, v.Active, safe(v.Size), safe(v.Reclaimable)); e != nil {
					return e
				}
			}
			for _, group := range d.DiskBreakdown {
				if _, e := fmt.Fprintf(w, "  %s details:\n", safe(group.Category)); e != nil {
					return e
				}
				for _, v := range group.Items {
					if _, e := fmt.Fprintf(w, "    id=%s name=%s image=%s repository=%s tag=%s cache_type=%s size=%s shared=%s unique=%s status=%s links=%s local_volumes=%s usage_count=%s in_use=%s shared_cache=%s containers=%s\n", safe(v.ID), safe(v.Name+v.Names), safe(v.Image), safe(v.Repository), safe(v.Tag), safe(v.CacheType), safe(v.Size), safe(v.SharedSize), safe(v.UniqueSize), safe(v.Status), safe(v.Links), safe(v.LocalVolumes), safe(v.UsageCount), safe(v.InUse), safe(v.Shared), safe(v.Containers)); e != nil {
						return e
					}
				}
				if group.Category == "images" || group.Category == "volumes" || group.Category == "build cache" {
					if _, e := fmt.Fprintln(w, "    Informational size only; shared data or in-use state is not a deletion-safety guarantee."); e != nil {
						return e
					}
				}
			}
			for _, v := range d.Runtime {
				if _, e := fmt.Fprintf(w, "  %s %s CPU %s memory %s (%s), network %s, block %s, PIDs %s\n", safe(v.ID), safe(v.Name), safe(v.CPUPerc), safe(v.MemUsage), safe(v.MemPerc), safe(v.NetIO), safe(v.BlockIO), safe(v.PIDs)); e != nil {
					return e
				}
			}
			if d.RuntimeTrend != nil {
				if _, e := fmt.Fprintf(w, "  Runtime peaks (%d samples):\n", d.RuntimeTrend.SampleCount); e != nil {
					return e
				}
				for _, v := range d.RuntimeTrend.Containers {
					if _, e := fmt.Fprintf(w, "    %s %s peak CPU %.2f%% memory %.2f%% cpu_threshold_exceeded=%t memory_threshold_exceeded=%t\n", safe(v.ID), safe(v.Name), v.PeakCPUPercent, v.PeakMemoryPercent, v.CPUThresholdExceeded, v.MemoryThresholdExceeded); e != nil {
						return e
					}
				}
			}
			for _, v := range d.Config {
				if _, e := fmt.Fprintf(w, "  %s %s status=%s health=%s project=%s service=%s memory=%s nano_cpus=%s pids=%s privileged=%s readonly_rootfs=%s log_driver=%s restarts=%d\n", safe(v.ID), safe(v.Name), safe(v.Status), safe(v.Health), safe(v.Project), safe(v.Service), int64Text(v.MemoryLimit), int64Text(v.NanoCPUs), int64Text(v.PIDsLimit), boolText(v.Privileged), boolText(v.ReadonlyRootfs), safe(v.LogDriver), v.RestartCount); e != nil {
					return e
				}
			}
			for _, g := range d.ComposeGroups {
				if _, e := fmt.Fprintf(w, "  Compose project %s: %d total, %d unhealthy, %d restarting\n", safe(g.Project), g.Total, g.Unhealthy, g.Restarting); e != nil {
					return e
				}
			}
		}
		if d := o.Details.Bazel; d != nil && d.Disk != nil {
			v := d.Disk
			if _, e := fmt.Fprintf(w, "  Path: %s\n  Totals: apparent %d bytes, allocated %d bytes\n  Coverage: %d/%d mounts scanned, %d directories, %d files, complete=%t\n", safe(v.Root), v.Totals.ApparentBytes, v.Totals.AllocatedBytes, v.Coverage.ScannedMounts, v.Coverage.Mounts, v.Coverage.Directories, v.Coverage.Files, v.Coverage.Complete); e != nil {
				return e
			}
			for _, issue := range v.Issues {
				if _, e := fmt.Fprintf(w, "  Scan issue: %s: %s: %s\n", safe(issue.Path), safe(string(issue.Operation)), safe(issue.Error)); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func printDockerSummary(w io.Writer, r plugins.Report) error {
	if _, err := fmt.Fprintf(w, "Docker · %s\n", safe(string(r.Status))); err != nil {
		return err
	}
	contextName := ""
	for _, outcome := range r.Outcomes {
		if d := outcome.Details.Docker; d != nil && d.Context != "" {
			contextName = d.Context
			break
		}
	}
	if contextName != "" {
		if _, err := fmt.Fprintf(w, "Context: %s\n", safe(contextName)); err != nil {
			return err
		}
	}
	// Put actionable findings and collection gaps before measurements.
	for _, outcome := range r.Outcomes {
		for _, finding := range outcome.Findings {
			if _, err := fmt.Fprintf(w, "[%s] %s\n", safe(string(finding.Severity)), safe(finding.Summary)); err != nil {
				return err
			}
			if finding.Recommendation != "" {
				if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(finding.Recommendation)); err != nil {
					return err
				}
			}
		}
		for _, issue := range outcome.Issues {
			if _, err := fmt.Fprintf(w, "Collection: %s\n", safe(issue.Summary)); err != nil {
				return err
			}
		}
	}
	for _, outcome := range r.Outcomes {
		d := outcome.Details.Docker
		if d == nil {
			continue
		}
		if outcome.Status != plugins.StatusComplete {
			if _, err := fmt.Fprintf(w, "%s: %s\n", safe(string(outcome.Operation)), safe(string(outcome.Status))); err != nil {
				return err
			}
		}
		for _, row := range d.DiskUsage {
			if _, err := fmt.Fprintf(w, "Storage · %s: %s total, %s reclaimable (%d active / %d)\n", safe(row.Category), safe(row.Size), safe(row.Reclaimable), row.Active, row.TotalCount); err != nil {
				return err
			}
		}
		for _, row := range d.Runtime {
			if _, err := fmt.Fprintf(w, "Container %s · CPU %s · memory %s (%s)\n", safe(shortContainerID(row.ID)), safe(row.CPUPerc), safe(row.MemUsage), safe(row.MemPerc)); err != nil {
				return err
			}
		}
		for _, row := range d.Config {
			if _, err := fmt.Fprintf(w, "Container %s · %s · %s\n", safe(shortContainerID(row.ID)), safe(row.Name), safe(row.Status)); err != nil {
				return err
			}
		}
		if d.RuntimeTrend != nil {
			for _, row := range d.RuntimeTrend.Containers {
				if _, err := fmt.Fprintf(w, "Peak %s · CPU %.1f%% · memory %.1f%%\n", safe(shortContainerID(row.ID)), row.PeakCPUPercent, row.PeakMemoryPercent); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func int64Text(v *int64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatInt(*v, 10)
}
func boolText(v *bool) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatBool(*v)
}
