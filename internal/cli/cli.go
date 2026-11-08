package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gabriwl165/daffy-duck/internal/health"
)

var version = "dev"

const usage = `daffy — read-only system diagnostics

Usage: daffy [scan] [options]
       daffy report --json [options]
       daffy watch [options]
       daffy doctor [options]

Commands: scan (default), watch, report, doctor
Options: --sample duration --timeout duration --include-sensitive --privileged
         --json (scan/report; watch emits NDJSON) --interval duration (watch)
Exit codes: 0 completed (findings do not fail), 1 execution failure, 2 usage error.
No files are written. Sensitive log collection and root checks require explicit opt-in.
`

type Deps struct {
	Collect func(context.Context, health.Options) health.Report
	Doctor  func(context.Context, health.Options) []health.Check
}

type RunConfig struct {
	Args        []string
	Out, ErrOut io.Writer
	Deps        Deps
	Version     string
}

type command string

const (
	commandScan   command = "scan"
	commandWatch  command = "watch"
	commandReport command = "report"
	commandDoctor command = "doctor"
)

type exitCode int

const (
	exitSuccess     exitCode = 0
	exitFailure     exitCode = 1
	exitUsage       exitCode = 2
	exitInterrupted exitCode = 130
)

func DefaultDeps() Deps { return Deps{health.Collect, health.Doctor} }

type config struct {
	sample, timeout, interval   time.Duration
	sensitive, privileged, json bool
}

func (c config) healthOptions() health.Options {
	return health.Options{SampleInterval: c.sample, CommandTimeout: c.timeout, IncludeSensitive: c.sensitive, Privileged: c.privileged}
}

type watchConfig struct {
	out, errOut io.Writer
	deps        Deps
	options     health.Options
	display     config
}

func Run(ctx context.Context, rc RunConfig) int {
	args, out, errOut, deps := rc.Args, rc.Out, rc.ErrOut, rc.Deps
	buildVersion := rc.Version
	if buildVersion == "" {
		buildVersion = version
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Fprint(out, usage)
		return int(exitSuccess)
	}
	if len(args) > 0 && args[0] == "--version" {
		fmt.Fprintln(out, "daffy", buildVersion)
		return int(exitSuccess)
	}
	cmd := commandScan
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case string(commandScan), string(commandWatch), string(commandReport), string(commandDoctor):
			cmd = command(args[0])
			args = args[1:]
		default:
			fmt.Fprintf(errOut, "unknown command %q\n%s", args[0], usage)
			return int(exitUsage)
		}
	}
	c := config{sample: time.Second, timeout: 3 * time.Second, interval: 5 * time.Second}
	fs := flag.NewFlagSet("daffy "+string(cmd), flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.DurationVar(&c.sample, "sample", c.sample, "sample duration (100ms–10s)")
	fs.DurationVar(&c.timeout, "timeout", c.timeout, "timeout for optional commands")
	fs.DurationVar(&c.interval, "interval", c.interval, "watch interval")
	fs.BoolVar(&c.sensitive, "include-sensitive", false, "include sensitive logs (explicit opt-in)")
	fs.BoolVar(&c.privileged, "privileged", false, "include root-only checks; does not invoke sudo")
	fs.BoolVar(&c.json, "json", false, "emit JSON (watch emits newline-delimited JSON)")
	fs.Usage = func() { fmt.Fprint(errOut, usage) }
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			fmt.Fprint(out, usage)
			return int(exitSuccess)
		}
		return int(exitUsage)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(errOut, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return int(exitUsage)
	}
	if c.sample < 100*time.Millisecond || c.sample > 10*time.Second || c.timeout <= 0 || c.interval <= 0 {
		fmt.Fprintln(errOut, "invalid duration: sample must be 100ms–10s; timeout and interval must be positive")
		return int(exitUsage)
	}
	if cmd == commandReport && !c.json {
		fmt.Fprintln(errOut, "report requires --json")
		return int(exitUsage)
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintf(errOut, "daffy supports Linux only (current platform: %s)\n", runtime.GOOS)
		return int(exitFailure)
	}
	if cmd == commandWatch {
		return watch(ctx, watchConfig{out: out, errOut: errOut, deps: deps, options: c.healthOptions(), display: c})
	}
	opts := c.healthOptions()
	if cmd == commandDoctor {
		checks := deps.Doctor(ctx, opts)
		if e := ctx.Err(); e != nil {
			return interrupted(errOut, e)
		}
		if e := printDoctor(out, checks, c.json); e != nil {
			return failed(errOut, e)
		}
		return int(exitSuccess)
	}
	r := deps.Collect(ctx, opts)
	if e := ctx.Err(); e != nil {
		return interrupted(errOut, e)
	}
	sortReport(&r)
	if c.json {
		if e := json.NewEncoder(out).Encode(r); e != nil {
			return failed(errOut, e)
		}
	} else {
		if e := printReport(out, r); e != nil {
			return failed(errOut, e)
		}
	}
	return int(exitSuccess)
}
func failed(w io.Writer, e error) int { fmt.Fprintln(w, "daffy:", e); return int(exitFailure) }
func interrupted(w io.Writer, e error) int {
	if errors.Is(e, context.Canceled) {
		return int(exitInterrupted)
	}
	return failed(w, e)
}
func watch(ctx context.Context, wc watchConfig) int {
	out, errOut, d, c := wc.out, wc.errOut, wc.deps, wc.display
	for {
		started := time.Now()
		r := d.Collect(ctx, wc.options)
		if e := ctx.Err(); e != nil {
			if errors.Is(e, context.Canceled) {
				return int(exitInterrupted)
			}
			return failed(errOut, e)
		}
		sortReport(&r)
		if c.json {
			if e := json.NewEncoder(out).Encode(r); e != nil {
				return failed(errOut, e)
			}
		} else {
			if _, e := fmt.Fprintf(out, "\n=== %s ===\n", r.CollectedAt.Format(time.RFC3339)); e != nil {
				return failed(errOut, e)
			}
			if e := printReport(out, r); e != nil {
				return failed(errOut, e)
			}
		}
		wait := c.interval - time.Since(started)
		if wait < 10*time.Millisecond {
			wait = 10 * time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			if errors.Is(ctx.Err(), context.Canceled) {
				return int(exitInterrupted)
			}
			return failed(errOut, ctx.Err())
		case <-t.C:
		}
	}
}
func severityRank(s health.Severity) int {
	switch health.Severity(strings.ToLower(string(s))) {
	case health.SeverityCritical:
		return 0
	case health.SeverityWarning:
		return 1
	case health.SeverityInfo:
		return 2
	case health.SeverityOK:
		return 3
	default:
		return 4
	}
}
func sortReport(r *health.Report) {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if severityRank(a.Severity) != severityRank(b.Severity) {
			return severityRank(a.Severity) < severityRank(b.Severity)
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Area < b.Area
	})
}

// safe removes terminal controls and other invisible control characters from human-readable external data.
func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
func printReport(w io.Writer, r health.Report) error {
	if _, e := fmt.Fprintf(w, "Daffy system report · %s\n", safe(r.CollectedAt.Format(time.RFC3339))); e != nil {
		return e
	}
	for _, item := range []struct {
		key, value string
		optional   bool
	}{
		{key: "cpu_model", value: r.System.CPUModel, optional: true},
		{key: "distro_adapter", value: r.System.DistroAdapter},
		{key: "distro_id", value: r.System.DistroID},
		{key: "distro_version", value: r.System.DistroVersion, optional: true},
		{key: "kernel", value: r.System.Kernel}, {key: "os", value: r.System.OS},
	} {
		if item.value == "" && (item.optional || r.System == (health.SystemInfo{})) {
			continue
		}
		if _, e := fmt.Fprintf(w, "%s: %s\n", safe(item.key), safe(item.value)); e != nil {
			return e
		}
	}
	if len(r.Findings) == 0 {
		_, e := fmt.Fprintln(w, "No findings reported.")
		return e
	}
	for _, f := range r.Findings {
		if _, e := fmt.Fprintf(w, "\n[%s] %s — %s\n", strings.ToUpper(safe(string(f.Severity))), safe(string(f.Area)), safe(f.Summary)); e != nil {
			return e
		}
		if f.Confidence != "" {
			if _, e := fmt.Fprintf(w, "Confidence: %s\n", safe(string(f.Confidence))); e != nil {
				return e
			}
		}
		for _, e := range f.Evidence {
			if _, e := fmt.Fprintf(w, "  Evidence: %s\n", safe(e)); e != nil {
				return e
			}
		}
		if f.Recommendation != "" {
			if _, e := fmt.Fprintf(w, "  Recommendation: %s\n", safe(f.Recommendation)); e != nil {
				return e
			}
		}
	}
	return nil
}
func printDoctor(w io.Writer, c []health.Check, j bool) error {
	if j {
		return json.NewEncoder(w).Encode(c)
	}
	for _, v := range c {
		state := "unavailable"
		if v.Available {
			state = "available"
		}
		if _, e := fmt.Fprintf(w, "%s: %s — %s\n", safe(v.Name), state, safe(v.Detail)); e != nil {
			return e
		}
	}
	return nil
}
