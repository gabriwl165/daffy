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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const (
	pluginCommand                = "plugins"
	pluginListCommand            = "list"
	pluginRunCommand             = "run"
	pluginFlagJSON               = "json"
	pluginFlagTimeout            = "timeout"
	pluginFlagPath               = "path"
	pluginFlagTop                = "top"
	pluginFlagExclude            = "exclude"
	pluginFlagContext            = "context"
	pluginFlagContainer          = "container"
	pluginFlagProject            = "project"
	pluginFlagSamples            = "samples"
	pluginFlagInterval           = "interval"
	pluginFlagRuntimeTop         = "runtime-top"
	pluginFlagCPUThreshold       = "cpu-threshold"
	pluginFlagMemoryThreshold    = "memory-threshold"
	pluginFlagDetails            = "details"
	pluginFlagVerbose            = "verbose"
	pluginFlagInvestigate        = "investigate"
	pluginFlagLogTail            = "log-tail"
	pluginFlagMemoryDetails      = "memory-details"
	pluginFlagRuntimeSort        = "runtime-sort"
	pluginFlagImageFilter        = "image-filter"
	pluginFlagDiskSort           = "disk-sort"
	pluginFlagHostCapacity       = "host-capacity"
	pluginDefaultTimeout         = 15 * time.Second
	pluginDefaultTop             = 20
	pluginMaximumTop             = 10000
	dockerSummaryRuntimeLimit    = 5
	dockerFindingUnlimitedMemory = "docker_memory_unlimited"
)

const pluginUsage = `Usage: daffy plugins list [--json]
        daffy plugins run docker [disk|runtime|config] [--details] [--verbose] [--investigate] [--log-tail N] [--memory-details] [--runtime-sort cpu|memory] [--image-filter all|unused|dangling|referenced] [--disk-sort size|unique-size|name] [--host-capacity] [--samples N] [--interval duration] [--runtime-top N] [--cpu-threshold percent] [--memory-threshold percent] [--json] [--timeout duration]
       daffy plugins run bazel [disk] --path PATH [--exclude PATH ...] [--top N] [--json] [--timeout duration]
Crash investigation is opt-in and may include sensitive container logs.
`

const dockerAliasUsage = `Usage: daffy docker [disk|runtime|config] [--details] [--verbose] [--investigate] [--log-tail N] [--memory-details] [--runtime-sort cpu|memory] [--image-filter all|unused|dangling|referenced] [--disk-sort size|unique-size|name] [--host-capacity] [--samples N] [--interval duration] [--runtime-top N] [--cpu-threshold percent] [--memory-threshold percent] [--json] [--timeout duration]
Crash investigation is opt-in and may include sensitive container logs.
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
	investigate := fs.Bool(pluginFlagInvestigate, false, "inspect bounded crash evidence; may include sensitive logs")
	logTail := fs.Int(pluginFlagLogTail, 0, "include up to 200 recent log lines (requires --investigate)")
	memoryDetails := fs.Bool(pluginFlagMemoryDetails, false, "collect bounded runtime memory samples")
	runtimeSortText := fs.String(pluginFlagRuntimeSort, "", "runtime ranking: cpu or memory")
	imageFilterText := fs.String(pluginFlagImageFilter, "", "image filter: all, unused, dangling, or referenced")
	diskSortText := fs.String(pluginFlagDiskSort, "", "image ordering: size, unique-size, or name")
	hostCapacity := fs.Bool(pluginFlagHostCapacity, false, "verify local Docker host free capacity")
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
	var specified struct{ Path, Top, Exclude, Context, Container, Project, Samples, Interval, RuntimeTop, CPU, Memory, Details, Investigate, LogTail, MemoryDetails, RuntimeSort, ImageFilter, DiskSort, HostCapacity bool }
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
		case pluginFlagInvestigate:
			specified.Investigate = true
		case pluginFlagLogTail:
			specified.LogTail = true
		case pluginFlagMemoryDetails:
			specified.MemoryDetails = true
		case pluginFlagRuntimeSort:
			specified.RuntimeSort = true
		case pluginFlagImageFilter:
			specified.ImageFilter = true
		case pluginFlagDiskSort:
			specified.DiskSort = true
		case pluginFlagHostCapacity:
			specified.HostCapacity = true
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
		storageSpecified := specified.ImageFilter || specified.DiskSort || specified.HostCapacity
		if specified.Path && !op.Options.Path || specified.Exclude && !op.Options.Excludes || specified.Top && !op.Options.TopN || specified.Context && (id != plugins.PluginDocker || !op.Options.Context) || specified.Container && (id != plugins.PluginDocker || !op.Options.Containers || op.ID == plugins.OperationDisk) || specified.Project && (id != plugins.PluginDocker || !op.Options.Project || op.ID == plugins.OperationDisk) || trendSpecified && !op.Options.RuntimeTrends || specified.Details && !op.Options.DiskDetails || specified.Investigate && !op.Options.CrashInvestigation || specified.LogTail && !op.Options.CrashInvestigation || specified.MemoryDetails && !op.Options.MemoryDetails || specified.RuntimeSort && !op.Options.RuntimeSort || storageSpecified && (!op.Options.StorageAnalysis || !op.Options.DiskDetails) || op.Options.RequirePath && *path == "" {
			fmt.Fprintf(errOut, "invalid or unsupported options for %q\n", safe(string(op.ID)))
			return int(exitUsage)
		}
	}
	if specified.LogTail && (!*investigate || *logTail == 0) || *logTail < 0 || *logTail > 200 || specified.RuntimeSort && *runtimeSortText == "" || specified.ImageFilter && *imageFilterText == "" || specified.DiskSort && *diskSortText == "" {
		fmt.Fprintln(errOut, "invalid profiling option value")
		return int(exitUsage)
	}
	runtimeSort := plugins.DockerRuntimeSort(*runtimeSortText)
	if specified.RuntimeSort && runtimeSort != plugins.DockerRuntimeSortCPU && runtimeSort != plugins.DockerRuntimeSortMemory {
		fmt.Fprintln(errOut, "runtime-sort must be cpu or memory")
		return int(exitUsage)
	}
	imageFilter := plugins.DockerImageFilter(*imageFilterText)
	if specified.ImageFilter && imageFilter != plugins.DockerImageFilterAll && imageFilter != plugins.DockerImageFilterUnused && imageFilter != plugins.DockerImageFilterDangling && imageFilter != plugins.DockerImageFilterReferenced {
		fmt.Fprintln(errOut, "invalid image-filter")
		return int(exitUsage)
	}
	diskSort := plugins.DockerDiskSort(*diskSortText)
	if specified.DiskSort && diskSort != plugins.DockerDiskSortSize && diskSort != plugins.DockerDiskSortUniqueSize && diskSort != plugins.DockerDiskSortName {
		fmt.Fprintln(errOut, "invalid disk-sort")
		return int(exitUsage)
	}
	if *top < 0 || *top > pluginMaximumTop {
		fmt.Fprintf(errOut, "top must be between 0 and %d\n", pluginMaximumTop)
		return int(exitUsage)
	}
	if *samples < 1 || *samples > 60 || *interval < 100*time.Millisecond || *interval > time.Minute || *runtimeTop < 0 || *runtimeTop > 100 || math.IsNaN(*cpuThreshold) || math.IsInf(*cpuThreshold, 0) || *cpuThreshold < 0 || math.IsNaN(*memoryThreshold) || math.IsInf(*memoryThreshold, 0) || *memoryThreshold < 0 || *memoryThreshold > 100 {
		fmt.Fprintln(errOut, "invalid runtime sampling options")
		return int(exitUsage)
	}
	request := plugins.Request{Operation: operation, Options: plugins.Options{Path: *path, Excludes: excludes, TopN: func() int {
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
	}(), RuntimeTop: *runtimeTop, CPUThreshold: *cpuThreshold, MemoryThreshold: *memoryThreshold, DiskDetails: *details || specified.ImageFilter || specified.DiskSort || specified.HostCapacity, CrashInvestigation: *investigate, LogTail: *logTail, MemoryDetails: *memoryDetails, RuntimeSort: runtimeSort, ImageFilter: imageFilter, DiskSort: diskSort, HostCapacity: *hostCapacity}}
	streamDocker := id == plugins.PluginDocker && operation == "" && !*asJSON && !*verbose
	var streamErr error
	var contextPrinted bool
	var report plugins.Report
	var err error
	if streamDocker {
		report, err = r.RunWithTimeoutObserved(ctx, id, request, *timeout, func(outcome plugins.Outcome) {
			if streamErr != nil {
				return
			}
			streamErr = printDockerObservedOutcome(out, outcome, &contextPrinted, runtimeSort)
		})
	} else {
		report, err = r.RunWithTimeout(ctx, id, request, *timeout)
	}
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
	} else if streamDocker {
		if streamErr != nil {
			return failed(errOut, streamErr)
		}
		if e := printDockerCompletion(out, report); e != nil {
			return failed(errOut, e)
		}
	} else if e := printPluginReportWithRuntimeSort(out, report, *verbose, runtimeSort); e != nil {
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
	return printPluginReportWithRuntimeSort(w, r, verbose, plugins.DockerRuntimeSortCPU)
}

func printPluginReportWithRuntimeSort(w io.Writer, r plugins.Report, verbose bool, runtimeSort plugins.DockerRuntimeSort) error {
	if r.Plugin == plugins.PluginDocker && !verbose {
		return printDockerSummaryWithRuntimeSort(w, r, runtimeSort)
	}
	if _, e := fmt.Fprintf(w, "%s · %s\n", safe(string(r.Plugin)), safe(string(r.Status))); e != nil {
		return e
	}
	contextPrinted := false
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
			if d.Context != "" && !contextPrinted {
				if _, e := fmt.Fprintf(w, "  Context: %s\n", safe(d.Context)); e != nil {
					return e
				}
				contextPrinted = true
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
				if _, e := fmt.Fprintf(w, "  %s %s status=%s health=%s project=%s service=%s memory=%s nano_cpus=%s pids=%s privileged=%s readonly_rootfs=%s log_driver=%s restarts=%d oom_killed=%s exit_code=%s\n", safe(v.ID), safe(v.Name), safe(v.Status), safe(v.Health), safe(v.Project), safe(v.Service), int64Text(v.MemoryLimit), int64Text(v.NanoCPUs), int64Text(v.PIDsLimit), boolText(v.Privileged), boolText(v.ReadonlyRootfs), safe(v.LogDriver), v.RestartCount, boolText(v.OOMKilled), int64Text(v.ExitCode)); e != nil {
					return e
				}
			}
			if e := printDockerEvidence(w, d); e != nil {
				return e
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
	return printDockerSummaryWithRuntimeSort(w, r, plugins.DockerRuntimeSortCPU)
}

func printDockerSummaryWithRuntimeSort(w io.Writer, r plugins.Report, runtimeSort plugins.DockerRuntimeSort) error {
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
	var dockerIDs []string
	for _, outcome := range r.Outcomes {
		if d := outcome.Details.Docker; d != nil {
			for _, c := range d.Config {
				dockerIDs = append(dockerIDs, c.ID)
			}
			for _, c := range d.Runtime {
				dockerIDs = append(dockerIDs, c.ID)
			}
			if d.RuntimeTrend != nil {
				for _, c := range d.RuntimeTrend.Containers {
					dockerIDs = append(dockerIDs, c.ID)
				}
			}
			for _, c := range d.Crashes {
				dockerIDs = append(dockerIDs, c.ContainerID)
			}
			for _, c := range d.Memory {
				dockerIDs = append(dockerIDs, c.ID)
			}
			for _, c := range d.Images {
				dockerIDs = append(dockerIDs, c.ID)
			}
		}
	}
	// Actionable warnings lead; repeated missing-memory-limit notes become one notice.
	type missingMemoryNotice struct {
		count          int
		recommendation string
	}
	missingMemory := missingMemoryNotice{}
	for _, outcome := range r.Outcomes {
		for _, finding := range outcome.Findings {
			if finding.Code == dockerFindingUnlimitedMemory {
				missingMemory.count++
				if finding.Recommendation != "" {
					missingMemory.recommendation = finding.Recommendation
				}
				continue
			}
			if finding.Severity != plugins.SeverityWarning {
				continue
			}
			if _, err := fmt.Fprintf(w, "[warning] %s\n", safe(shortenKnownIDs(finding.Summary, dockerIDs))); err != nil {
				return err
			}
			if finding.Recommendation != "" {
				if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(shortenKnownIDs(finding.Recommendation, dockerIDs))); err != nil {
					return err
				}
			}
		}
	}
	for _, outcome := range r.Outcomes {
		for _, finding := range outcome.Findings {
			if finding.Severity == plugins.SeverityInfo && finding.Code != dockerFindingUnlimitedMemory {
				if _, err := fmt.Fprintf(w, "[info] %s\n", safe(shortenKnownIDs(finding.Summary, dockerIDs))); err != nil {
					return err
				}
				if finding.Recommendation != "" {
					if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(shortenKnownIDs(finding.Recommendation, dockerIDs))); err != nil {
						return err
					}
				}
			}
		}
	}
	if missingMemory.count > 0 {
		if _, err := fmt.Fprintf(w, "[info] %d containers have no memory limit\n", missingMemory.count); err != nil {
			return err
		}
		if missingMemory.recommendation != "" {
			if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(missingMemory.recommendation)); err != nil {
				return err
			}
		}
	}
	for _, outcome := range r.Outcomes {
		d := outcome.Details.Docker
		if d == nil {
			continue
		}
		if err := printDockerEvidence(w, d); err != nil {
			return err
		}
		runtimeRows := append([]plugins.DockerContainerStats(nil), d.Runtime...)
		sort.SliceStable(runtimeRows, func(i, j int) bool {
			if runtimeSort == plugins.DockerRuntimeSortMemory {
				return parseDockerBytes(runtimeRows[i].MemUsage) > parseDockerBytes(runtimeRows[j].MemUsage)
			}
			cpuI, cpuJ := parsePercent(runtimeRows[i].CPUPerc), parsePercent(runtimeRows[j].CPUPerc)
			if cpuI != cpuJ {
				return cpuI > cpuJ
			}
			return parseDockerBytes(runtimeRows[i].MemUsage) > parseDockerBytes(runtimeRows[j].MemUsage)
		})
		for index, row := range runtimeRows {
			if index >= dockerSummaryRuntimeLimit {
				if _, err := fmt.Fprintf(w, "%d more containers hidden; use --verbose to show all.\n", len(d.Runtime)-index); err != nil {
					return err
				}
				break
			}
			if _, err := fmt.Fprintf(w, "Container %s · %s · CPU %s · memory %s (%s)\n", safe(shortContainerID(row.ID)), safe(row.Name), safe(row.CPUPerc), safe(row.MemUsage), safe(row.MemPerc)); err != nil {
				return err
			}
		}
		if d.RuntimeTrend != nil {
			trends := append([]plugins.DockerRuntimeTrendItem(nil), d.RuntimeTrend.Containers...)
			sort.SliceStable(trends, func(i, j int) bool {
				if runtimeSort == plugins.DockerRuntimeSortMemory {
					return trends[i].PeakMemoryBytes > trends[j].PeakMemoryBytes
				}
				return trends[i].PeakCPUPercent > trends[j].PeakCPUPercent
			})
			for index, row := range trends {
				if index >= dockerSummaryRuntimeLimit {
					if _, err := fmt.Fprintf(w, "%d more runtime peaks hidden; use --verbose to show all.\n", len(d.RuntimeTrend.Containers)-index); err != nil {
						return err
					}
					break
				}
				if _, err := fmt.Fprintf(w, "Peak %s · CPU %.1f%% · memory %.1f%%\n", safe(shortContainerID(row.ID)), row.PeakCPUPercent, row.PeakMemoryPercent); err != nil {
					return err
				}
			}
		}
	}
	for _, outcome := range r.Outcomes {
		d := outcome.Details.Docker
		if d == nil {
			continue
		}
		for _, row := range d.DiskUsage {
			if _, err := fmt.Fprintf(w, "Storage · %s: %s total, %s reclaimable (%d active / %d)\n", safe(row.Category), safe(row.Size), safe(row.Reclaimable), row.Active, row.TotalCount); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	gapWritten := false
	for _, outcome := range r.Outcomes {
		if outcome.Status == plugins.StatusPartial || outcome.Status == plugins.StatusUnavailable || outcome.Status == plugins.StatusCancelled {
			if !gapWritten {
				if _, err := fmt.Fprintln(w, "Collection gaps:"); err != nil {
					return err
				}
				gapWritten = true
			}
			if _, err := fmt.Fprintf(w, "  %s: %s\n", safe(string(outcome.Operation)), safe(string(outcome.Status))); err != nil {
				return err
			}
		}
		for _, issue := range outcome.Issues {
			if !gapWritten {
				if _, err := fmt.Fprintln(w, "Collection gaps:"); err != nil {
					return err
				}
				gapWritten = true
			}
			if _, err := fmt.Fprintf(w, "  %s: %s\n", safe(string(outcome.Operation)), safe(issue.Summary)); err != nil {
				return err
			}
		}
	}
	return nil
}

func printDockerEvidence(w io.Writer, d *plugins.DockerDetails) error {
	if d == nil {
		return nil
	}
	for _, crash := range d.Crashes {
		if _, err := fmt.Fprintf(w, "Crash · %s · %s · %s / %s · restarts %d", safe(shortContainerID(crash.ContainerID)), safe(crash.Name), safe(string(crash.State)), safe(string(crash.Classification)), crash.RestartCount); err != nil {
			return err
		}
		if crash.RestartPolicy != "" {
			if _, err := fmt.Fprintf(w, " · policy %s", safe(crash.RestartPolicy)); err != nil {
				return err
			}
		}
		if crash.Project != "" || crash.Service != "" {
			if _, err := fmt.Fprintf(w, " · %s/%s", safe(crash.Project), safe(crash.Service)); err != nil {
				return err
			}
		}
		if crash.ExitCode != nil {
			if _, err := fmt.Fprintf(w, " · exit %d", *crash.ExitCode); err != nil {
				return err
			}
		}
		if crash.OOMKilled != nil && *crash.OOMKilled {
			if _, err := fmt.Fprint(w, " · OOM recorded"); err != nil {
				return err
			}
		}
		if crash.StartedAt != nil {
			if _, err := fmt.Fprintf(w, " · started %s", safe(crash.StartedAt.Format(time.RFC3339))); err != nil {
				return err
			}
		}
		if crash.FinishedAt != nil {
			if _, err := fmt.Fprintf(w, " · finished %s", safe(crash.FinishedAt.Format(time.RFC3339))); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if crash.Evidence != "" {
			if _, err := fmt.Fprintf(w, "  Evidence: %s\n", safe(crash.Evidence)); err != nil {
				return err
			}
		}
		if crash.Recommendation != "" {
			if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(crash.Recommendation)); err != nil {
				return err
			}
		}
		if crash.HistoryIncomplete {
			if _, err := fmt.Fprintln(w, "  Event history is incomplete; counts cover only the observed window."); err != nil {
				return err
			}
		}
		for _, issue := range crash.EvidenceIssues {
			if _, err := fmt.Fprintf(w, "  Evidence gap: %s\n", safe(issue)); err != nil {
				return err
			}
		}
		if crash.EvidenceTruncated {
			if _, err := fmt.Fprintln(w, "  Evidence was truncated at the configured collection limit."); err != nil {
				return err
			}
		}
		for _, entry := range crash.HealthLog {
			if _, err := fmt.Fprintf(w, "  Health · exit %d", entry.ExitCode); err != nil {
				return err
			}
			if entry.Start != nil {
				if _, err := fmt.Fprintf(w, " · start %s", safe(entry.Start.Format(time.RFC3339))); err != nil {
					return err
				}
			}
			if entry.End != nil {
				if _, err := fmt.Fprintf(w, " · end %s", safe(entry.End.Format(time.RFC3339))); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, " · %s\n", safeLogEvidence(entry.Output)); err != nil {
				return err
			}
		}
		for _, line := range crash.RecentLogs {
			if _, err := fmt.Fprintf(w, "  Log: %s\n", safeLogEvidence(line)); err != nil {
				return err
			}
		}
		if len(crash.HealthLog) > 0 || len(crash.RecentLogs) > 0 {
			if _, err := fmt.Fprintln(w, "  Warning: diagnostic logs may contain secrets or personal data."); err != nil {
				return err
			}
		}
		if crash.RecentDieEvents+crash.RecentStartEvents+crash.RecentRestartEvents > 0 {
			if _, err := fmt.Fprintf(w, "  Observed events: die %d · start %d · restart %d\n", crash.RecentDieEvents, crash.RecentStartEvents, crash.RecentRestartEvents); err != nil {
				return err
			}
		}
	}
	for _, m := range d.Memory {
		if _, err := fmt.Fprintf(w, "Memory · %s · %s", safe(shortContainerID(m.ID)), safe(m.Name)); err != nil {
			return err
		}
		if m.CurrentBytes != nil {
			if _, err := fmt.Fprintf(w, " · current %s", formatBytes(*m.CurrentBytes)); err != nil {
				return err
			}
		}
		if m.PeakBytes != nil {
			if _, err := fmt.Fprintf(w, " · peak %s", formatBytes(*m.PeakBytes)); err != nil {
				return err
			}
		}
		if m.ObservedGrowthBytes != nil {
			if _, err := fmt.Fprintf(w, " · growth %+s", formatSignedBytes(*m.ObservedGrowthBytes)); err != nil {
				return err
			}
		}
		if m.LimitBytes != nil {
			if _, err := fmt.Fprintf(w, " · limit %s", formatBytes(*m.LimitBytes)); err != nil {
				return err
			}
		}
		if m.HeadroomBytes != nil {
			if _, err := fmt.Fprintf(w, " · headroom %s", formatBytes(*m.HeadroomBytes)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, " · %s denominator · %d/%d pressure samples (%d observations)", safe(string(m.Denominator)), m.PressureSampleCount, m.SampleCount, m.PressureObservationCount); err != nil {
			return err
		}
		if m.PressureThresholdPercent > 0 {
			if _, err := fmt.Fprintf(w, " at %.1f%%", m.PressureThresholdPercent); err != nil {
				return err
			}
		}
		if m.SustainedPressure {
			if _, err := fmt.Fprint(w, " · sustained pressure"); err != nil {
				return err
			}
		}
		if m.RisingUsage {
			if _, err := fmt.Fprint(w, " · rising usage"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if m.RisingUsage {
			if _, err := fmt.Fprintln(w, "  Rising usage is an observation, not proof of a memory leak."); err != nil {
				return err
			}
		}
		if m.OOMKilled != nil && *m.OOMKilled {
			if _, err := fmt.Fprintln(w, "  OOM kill is recorded."); err != nil {
				return err
			}
		}
	}
	if len(d.Images) > 0 {
		if _, err := fmt.Fprintln(w, "Images · repository:tag · id · references · size · shared · unique"); err != nil {
			return err
		}
		for _, image := range d.Images {
			if _, err := fmt.Fprintf(w, "  %s:%s · %s · %s (%s) · %s · %s · %s\n", safe(image.Repository), safe(image.Tag), safe(shortContainerID(image.ID)), safe(string(image.ReferenceState)), optionalInt64(image.ReferenceCount), safe(image.Size), safe(image.SharedSize), safe(image.UniqueSize)); err != nil {
				return err
			}
		}
	}
	if storage := d.Storage; storage != nil {
		for _, warning := range storage.Warnings {
			if _, err := fmt.Fprintf(w, "Storage warning: %s\n", safe(warning)); err != nil {
				return err
			}
		}
		for _, limit := range storage.Limits {
			if _, err := fmt.Fprintf(w, "Storage note: %s\n", safe(limit)); err != nil {
				return err
			}
		}
		if cap := storage.HostCapacity; cap != nil {
			if !cap.Available {
				if _, err := fmt.Fprintf(w, "Host capacity unavailable: %s\n", safe(string(cap.UnavailableReason))); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprint(w, "Docker host capacity"); err != nil {
					return err
				}
				if cap.BytesAvailable != nil {
					if _, err := fmt.Fprintf(w, " · %s bytes available", formatBytes(*cap.BytesAvailable)); err != nil {
						return err
					}
				}
				if cap.BytesFree != nil {
					if _, err := fmt.Fprintf(w, " (%s free)", formatBytes(*cap.BytesFree)); err != nil {
						return err
					}
				}
				if cap.BytesTotal != nil {
					if _, err := fmt.Fprintf(w, " / %s", formatBytes(*cap.BytesTotal)); err != nil {
						return err
					}
				}
				if cap.InodesFree != nil && cap.InodesTotal != nil {
					if _, err := fmt.Fprintf(w, " · %d/%d inodes free", *cap.InodesFree, *cap.InodesTotal); err != nil {
						return err
					}
				}
				if cap.LowBytes || cap.LowInodes {
					if _, err := fmt.Fprint(w, " · LOW CAPACITY"); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
		}
		if storage.SharedLayers || !storage.SizeTotalsComparable {
			if _, err := fmt.Fprintln(w, "  Image sizes overlap through shared layers; totals are not safely additive or deletion guidance."); err != nil {
				return err
			}
		}
	}
	for _, group := range d.ComposeGroups {
		if _, err := fmt.Fprintf(w, "Compose lifecycle · %s: %d total · %d unhealthy · %d restarting\n", safe(group.Project), group.Total, group.Unhealthy, group.Restarting); err != nil {
			return err
		}
	}
	if len(d.ComposeGroups) > 0 {
		if _, err := fmt.Fprintln(w, "  Counts reflect the observed snapshot; they are not a retention or recovery guarantee."); err != nil {
			return err
		}
	}
	return nil
}

func optionalInt64(value *int64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatInt(*value, 10)
}

func safeLogEvidence(value string) string {
	return safe(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, value))
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
func formatSignedBytes(value int64) string {
	if value < 0 {
		return "-" + formatBytes(uint64(-(value+1))+1)
	}
	return formatBytes(uint64(value))
}

func printDockerObservedOutcome(w io.Writer, outcome plugins.Outcome, contextPrinted *bool, runtimeSort plugins.DockerRuntimeSort) error {
	d := outcome.Details.Docker
	if d != nil && d.Context != "" && !*contextPrinted {
		if _, err := fmt.Fprintf(w, "Docker context: %s\n", safe(d.Context)); err != nil {
			return err
		}
		*contextPrinted = true
	}
	var ids []string
	if d != nil {
		for _, c := range d.Config {
			ids = append(ids, c.ID)
		}
		for _, c := range d.Runtime {
			ids = append(ids, c.ID)
		}
		for _, c := range d.Crashes {
			ids = append(ids, c.ContainerID)
		}
		for _, c := range d.Memory {
			ids = append(ids, c.ID)
		}
		for _, c := range d.Images {
			ids = append(ids, c.ID)
		}
	}
	for _, finding := range outcome.Findings {
		if finding.Severity == plugins.SeverityWarning {
			if _, err := fmt.Fprintf(w, "[warning] %s\n", safe(shortenKnownIDs(finding.Summary, ids))); err != nil {
				return err
			}
			if finding.Recommendation != "" {
				if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(shortenKnownIDs(finding.Recommendation, ids))); err != nil {
					return err
				}
			}
		}
	}
	for _, finding := range outcome.Findings {
		if finding.Severity == plugins.SeverityInfo && finding.Code != dockerFindingUnlimitedMemory {
			if _, err := fmt.Fprintf(w, "[info] %s\n", safe(shortenKnownIDs(finding.Summary, ids))); err != nil {
				return err
			}
			if finding.Recommendation != "" {
				if _, err := fmt.Fprintf(w, "  Next: %s\n", safe(shortenKnownIDs(finding.Recommendation, ids))); err != nil {
					return err
				}
			}
		}
	}
	if d == nil {
		return nil
	}
	if outcome.Operation == plugins.OperationConfig {
		count := 0
		for _, f := range outcome.Findings {
			if f.Code == dockerFindingUnlimitedMemory {
				count++
			}
		}
		if count > 0 {
			if _, err := fmt.Fprintf(w, "[info] %d containers have no memory limit\n", count); err != nil {
				return err
			}
		}
	}
	if outcome.Operation == plugins.OperationRuntime {
		rows := append([]plugins.DockerContainerStats(nil), d.Runtime...)
		sort.SliceStable(rows, func(i, j int) bool {
			if runtimeSort == plugins.DockerRuntimeSortMemory {
				return parseDockerBytes(rows[i].MemUsage) > parseDockerBytes(rows[j].MemUsage)
			}
			if parsePercent(rows[i].CPUPerc) != parsePercent(rows[j].CPUPerc) {
				return parsePercent(rows[i].CPUPerc) > parsePercent(rows[j].CPUPerc)
			}
			return parseDockerBytes(rows[i].MemUsage) > parseDockerBytes(rows[j].MemUsage)
		})
		for index, row := range rows {
			if index == dockerSummaryRuntimeLimit {
				if _, err := fmt.Fprintf(w, "%d more containers hidden; use --verbose to show all.\n", len(rows)-index); err != nil {
					return err
				}
				break
			}
			if _, err := fmt.Fprintf(w, "Container %s · %s · CPU %s · memory %s (%s)\n", safe(shortContainerID(row.ID)), safe(row.Name), safe(row.CPUPerc), safe(row.MemUsage), safe(row.MemPerc)); err != nil {
				return err
			}
		}
		if d.RuntimeTrend != nil {
			for index, row := range d.RuntimeTrend.Containers {
				if index == dockerSummaryRuntimeLimit {
					if _, err := fmt.Fprintf(w, "%d more runtime peaks hidden; use --verbose to show all.\n", len(d.RuntimeTrend.Containers)-index); err != nil {
						return err
					}
					break
				}
				if _, err := fmt.Fprintf(w, "Peak %s · CPU %.1f%% · memory %.1f%%\n", safe(shortContainerID(row.ID)), row.PeakCPUPercent, row.PeakMemoryPercent); err != nil {
					return err
				}
			}
		}
	}
	if err := printDockerEvidence(w, d); err != nil {
		return err
	}
	if outcome.Operation == plugins.OperationDisk {
		for _, row := range d.DiskUsage {
			if _, err := fmt.Fprintf(w, "Storage · %s: %s total, %s reclaimable (%d active / %d)\n", safe(row.Category), safe(row.Size), safe(row.Reclaimable), row.Active, row.TotalCount); err != nil {
				return err
			}
		}
	}
	return nil
}

func printDockerCompletion(w io.Writer, report plugins.Report) error {
	if _, err := fmt.Fprintf(w, "Docker · %s\n", safe(string(report.Status))); err != nil {
		return err
	}
	gapWritten := false
	for _, outcome := range report.Outcomes {
		if outcome.Status == plugins.StatusPartial || outcome.Status == plugins.StatusUnavailable || outcome.Status == plugins.StatusCancelled {
			if !gapWritten {
				if _, err := fmt.Fprintln(w, "Collection gaps:"); err != nil {
					return err
				}
				gapWritten = true
			}
			if _, err := fmt.Fprintf(w, "  %s: %s\n", safe(string(outcome.Operation)), safe(string(outcome.Status))); err != nil {
				return err
			}
		}
		for _, issue := range outcome.Issues {
			if !gapWritten {
				if _, err := fmt.Fprintln(w, "Collection gaps:"); err != nil {
					return err
				}
				gapWritten = true
			}
			if _, err := fmt.Fprintf(w, "  %s: %s\n", safe(string(outcome.Operation)), safe(issue.Summary)); err != nil {
				return err
			}
		}
	}
	return nil
}

func parsePercent(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	n, _ := strconv.ParseFloat(value, 64)
	return n
}

func parseDockerBytes(usage string) float64 {
	value := strings.TrimSpace(strings.SplitN(usage, "/", 2)[0])
	if value == "" {
		return 0
	}
	index := 0
	for index < len(value) && (value[index] >= '0' && value[index] <= '9' || value[index] == '.' || value[index] == '-') {
		index++
	}
	n, err := strconv.ParseFloat(value[:index], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSuffix(value[index:], "B"))
	switch unit {
	case "K":
		return n * 1e3
	case "M":
		return n * 1e6
	case "G":
		return n * 1e9
	case "T":
		return n * 1e12
	case "KI":
		return n * (1 << 10)
	case "MI":
		return n * (1 << 20)
	case "GI":
		return n * (1 << 30)
	case "TI":
		return n * (1 << 40)
	default:
		return n
	}
}

func shortenKnownIDs(text string, ids []string) string {
	for _, id := range ids {
		if len(id) <= 12 {
			continue
		}
		start := 0
		for {
			i := strings.Index(text[start:], id)
			if i < 0 {
				break
			}
			i += start
			end := i + len(id)
			left, _ := utf8.DecodeLastRuneInString(text[:i])
			right, _ := utf8.DecodeRuneInString(text[end:])
			leftOK := i == 0 || !idTokenRune(left)
			rightOK := end == len(text) || !idTokenRune(right)
			if leftOK && rightOK {
				text = text[:i] + id[:12] + text[end:]
				start = i + 12
			} else {
				start = end
			}
		}
	}
	return text
}

func idTokenRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '-' || value == '_'
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
