package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const (
	pluginCommand        = "plugins"
	pluginListCommand    = "list"
	pluginRunCommand     = "run"
	pluginFlagJSON       = "json"
	pluginFlagTimeout    = "timeout"
	pluginFlagPath       = "path"
	pluginFlagTop        = "top"
	pluginFlagExclude    = "exclude"
	pluginDefaultTimeout = 15 * time.Second
	pluginDefaultTop     = 20
	pluginMaximumTop     = 10000
)

const pluginUsage = `Usage: daffy plugins list [--json]
       daffy plugins run docker [disk|runtime|config] [--json] [--timeout duration]
       daffy plugins run bazel [disk] --path PATH [--exclude PATH ...] [--top N] [--json] [--timeout duration]
`

const dockerAliasUsage = `Usage: daffy docker [disk|runtime|config] [--json] [--timeout duration]
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
	timeout := fs.Duration(pluginFlagTimeout, pluginDefaultTimeout, "operation timeout")
	path := fs.String(pluginFlagPath, "", "explicit output or cache directory")
	top := fs.Int(pluginFlagTop, pluginDefaultTop, "top entries")
	var excludes stringsFlag
	fs.Var(&excludes, pluginFlagExclude, "exclude path (repeatable)")
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
	var specified struct{ Path, Top, Exclude bool }
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case pluginFlagPath:
			specified.Path = true
		case pluginFlagTop:
			specified.Top = true
		case pluginFlagExclude:
			specified.Exclude = true
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
		if specified.Path && !op.Options.Path || specified.Exclude && !op.Options.Excludes || specified.Top && !op.Options.TopN || op.Options.RequirePath && *path == "" {
			fmt.Fprintf(errOut, "invalid or unsupported options for %q\n", safe(string(op.ID)))
			return int(exitUsage)
		}
	}
	if *top < 0 || *top > pluginMaximumTop {
		fmt.Fprintf(errOut, "top must be between 0 and %d\n", pluginMaximumTop)
		return int(exitUsage)
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	report, err := r.Run(ctx, id, plugins.Request{Operation: operation, Options: plugins.Options{Path: *path, Excludes: excludes, TopN: func() int {
		for _, op := range selected {
			if op.Options.TopN {
				return *top
			}
		}
		return 0
	}()}})
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
	} else if e := printPluginReport(out, report); e != nil {
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

func printPluginReport(w io.Writer, r plugins.Report) error {
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
			for _, v := range d.Runtime {
				if _, e := fmt.Fprintf(w, "  %s %s CPU %s memory %s (%s), network %s, block %s, PIDs %s\n", safe(v.ID), safe(v.Name), safe(v.CPUPerc), safe(v.MemUsage), safe(v.MemPerc), safe(v.NetIO), safe(v.BlockIO), safe(v.PIDs)); e != nil {
					return e
				}
			}
			for _, v := range d.Config {
				if _, e := fmt.Fprintf(w, "  %s %s status=%s health=%s memory=%s nano_cpus=%s pids=%s privileged=%s readonly_rootfs=%s log_driver=%s restarts=%d\n", safe(v.ID), safe(v.Name), safe(v.Status), safe(v.Health), int64Text(v.MemoryLimit), int64Text(v.NanoCPUs), int64Text(v.PIDsLimit), boolText(v.Privileged), boolText(v.ReadonlyRootfs), safe(v.LogDriver), v.RestartCount); e != nil {
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
