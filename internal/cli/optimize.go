package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/gabriwl165/daffy/internal/health"
	"github.com/gabriwl165/daffy/internal/optimize"
)

const optimizeUsage = `Usage: daffy optimize [--json] [--sample duration] [--timeout duration]
Read-only recommendations based on sampled system health observations.
`

func runOptimize(ctx context.Context, args []string, out, errOut io.Writer, deps Deps) int {
	var asJSON bool
	sample, timeout := time.Second, 3*time.Second
	fs := flag.NewFlagSet("daffy optimize", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	fs.DurationVar(&sample, "sample", sample, "sample duration (100ms–10s)")
	fs.DurationVar(&timeout, "timeout", timeout, "timeout for optional commands")
	// Handle help explicitly so it is written once, to stdout, with errors checked.
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, e := fmt.Fprint(out, optimizeUsage); e != nil {
				return failed(errOut, e)
			}
			return int(exitSuccess)
		}
		fmt.Fprint(errOut, optimizeUsage)
		return int(exitUsage)
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "optimize accepts no positional arguments")
		return int(exitUsage)
	}
	if sample < 100*time.Millisecond || sample > 10*time.Second || timeout <= 0 {
		fmt.Fprintln(errOut, "invalid duration: sample must be 100ms–10s; timeout must be positive")
		return int(exitUsage)
	}
	if !health.SupportedPlatform() {
		fmt.Fprintf(errOut, "daffy health diagnostics are unsupported on %s\n", runtime.GOOS)
		return int(exitFailure)
	}
	if err := ctx.Err(); err != nil {
		return interrupted(errOut, err)
	}
	r := deps.Collect(ctx, health.Options{SampleInterval: sample, CommandTimeout: timeout})
	if err := ctx.Err(); err != nil {
		return interrupted(errOut, err)
	}
	p := optimize.Analyze(r)
	if asJSON {
		if err := json.NewEncoder(out).Encode(p); err != nil {
			return failed(errOut, err)
		}
	} else if err := printOptimize(out, p); err != nil {
		return failed(errOut, err)
	}
	return int(exitSuccess)
}

func printOptimize(w io.Writer, p optimize.Plan) error {
	if _, err := fmt.Fprintf(w, "Daffy optimization recommendations · %s\nRead-only: yes\n", safe(p.CollectedAt.Format(time.RFC3339))); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		value optimize.Capability
	}{
		{"CPU", p.Capabilities.CPU}, {"Memory", p.Capabilities.Memory}, {"Storage", p.Capabilities.Storage},
		{"Thermal", p.Capabilities.Thermal}, {"Processes", p.Capabilities.Processes}, {"History", p.Capabilities.History},
	} {
		if _, err := fmt.Fprintf(w, "Capability %s: %s", safe(item.name), safe(string(item.value.Status))); err != nil {
			return err
		}
		if item.value.Reason != "" {
			if _, err := fmt.Fprintf(w, " — %s", safe(item.value.Reason)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if len(p.Recommendations) == 0 {
		if _, err := fmt.Fprintln(w, "No actionable recommendations from available observations"); err != nil {
			return err
		}
	}
	for i, rec := range p.Recommendations {
		if _, err := fmt.Fprintf(w, "\n%d. [%s] %s — %s (confidence: %s)\n", i+1, safe(string(rec.Severity)), safe(string(rec.Area)), safe(rec.Summary), safe(string(rec.Confidence))); err != nil {
			return err
		}
		for _, evidence := range rec.Evidence {
			if _, err := fmt.Fprintf(w, "  Evidence (%s): %s\n", safe(evidence.FindingID), safe(evidence.Summary)); err != nil {
				return err
			}
			for _, detail := range evidence.Details {
				if _, err := fmt.Fprintf(w, "    %s\n", safe(detail)); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintf(w, "  Suggested action: %s\n", safe(rec.Action.Description)); err != nil {
			return err
		}
		if rec.Action.Command != "" {
			if _, err := fmt.Fprintf(w, "  Example command (not run): %s\n", safe(rec.Action.Command)); err != nil {
				return err
			}
		}
	}
	for _, limitation := range p.Limitations {
		if _, err := fmt.Fprintf(w, "Limitation: %s\n", safe(limitation)); err != nil {
			return err
		}
	}
	return nil
}
