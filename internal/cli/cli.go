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

func DefaultDeps() Deps { return Deps{health.Collect, health.Doctor} }

type config struct {
	sample, timeout, interval   time.Duration
	sensitive, privileged, json bool
}

func Run(ctx context.Context, args []string, out, errOut io.Writer, deps Deps) int {
	return RunVersion(ctx, args, out, errOut, deps, version)
}
func RunVersion(ctx context.Context, args []string, out, errOut io.Writer, deps Deps, buildVersion string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Fprint(out, usage)
		return 0
	}
	if len(args) > 0 && args[0] == "--version" {
		fmt.Fprintln(out, "daffy", buildVersion)
		return 0
	}
	command := "scan"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "scan", "watch", "report", "doctor":
			command = args[0]
			args = args[1:]
		default:
			fmt.Fprintf(errOut, "unknown command %q\n%s", args[0], usage)
			return 2
		}
	}
	c := config{sample: time.Second, timeout: 3 * time.Second, interval: 5 * time.Second}
	fs := flag.NewFlagSet("daffy "+command, flag.ContinueOnError)
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
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(errOut, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	if c.sample < 100*time.Millisecond || c.sample > 10*time.Second || c.timeout <= 0 || c.interval <= 0 {
		fmt.Fprintln(errOut, "invalid duration: sample must be 100ms–10s; timeout and interval must be positive")
		return 2
	}
	if command == "report" && !c.json {
		fmt.Fprintln(errOut, "report requires --json")
		return 2
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintf(errOut, "daffy supports Linux only (current platform: %s)\n", runtime.GOOS)
		return 1
	}
	if command == "watch" {
		return watch(ctx, out, errOut, deps, c)
	}
	opts := health.Options{SampleInterval: c.sample, CommandTimeout: c.timeout, IncludeSensitive: c.sensitive, Privileged: c.privileged}
	if command == "doctor" {
		checks := deps.Doctor(ctx, opts)
		if e := ctx.Err(); e != nil {
			return interrupted(errOut, e)
		}
		if e := printDoctor(out, checks, c.json); e != nil {
			return failed(errOut, e)
		}
		return 0
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
	return 0
}
func failed(w io.Writer, e error) int { fmt.Fprintln(w, "daffy:", e); return 1 }
func interrupted(w io.Writer, e error) int {
	if errors.Is(e, context.Canceled) {
		return 130
	}
	return failed(w, e)
}
func watch(ctx context.Context, out, errOut io.Writer, d Deps, c config) int {
	for {
		started := time.Now()
		r := d.Collect(ctx, health.Options{SampleInterval: c.sample, CommandTimeout: c.timeout, IncludeSensitive: c.sensitive, Privileged: c.privileged})
		if e := ctx.Err(); e != nil {
			if errors.Is(e, context.Canceled) {
				return 130
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
				return 130
			}
			return failed(errOut, ctx.Err())
		case <-t.C:
		}
	}
}
func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	case "ok":
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
	if len(r.System) > 0 {
		keys := make([]string, 0, len(r.System))
		for k := range r.System {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, e := fmt.Fprintf(w, "%s: %s\n", safe(k), safe(r.System[k])); e != nil {
				return e
			}
		}
	}
	if len(r.Findings) == 0 {
		_, e := fmt.Fprintln(w, "No findings reported.")
		return e
	}
	for _, f := range r.Findings {
		if _, e := fmt.Fprintf(w, "\n[%s] %s — %s\n", strings.ToUpper(safe(f.Severity)), safe(f.Area), safe(f.Summary)); e != nil {
			return e
		}
		if f.Confidence != "" {
			if _, e := fmt.Fprintf(w, "Confidence: %s\n", safe(f.Confidence)); e != nil {
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
