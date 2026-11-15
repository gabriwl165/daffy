package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/health"
	"github.com/gabriwl165/daffy/internal/optimize"
)

type optimizeFailWriter struct{}

func (optimizeFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestOptimizeCLI(t *testing.T) {
	called := 0
	var got health.Options
	deps := Deps{Collect: func(_ context.Context, options health.Options) health.Report {
		called++
		got = options
		return health.Report{Findings: []health.Finding{{ID: "memory.capacity", Area: health.AreaMemory, Severity: health.SeverityWarning, Confidence: health.ConfidenceMedium, Summary: "Memory constrained", Evidence: []string{"used_percent=90"}}}}
	}}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: []string{"optimize", "--json", "--sample", "250ms", "--timeout", "2s"}, Out: &out, ErrOut: &errOut, Deps: deps})
	if code != 0 || called != 1 || got.SampleInterval != 250*time.Millisecond || got.CommandTimeout != 2*time.Second || got.IncludeSensitive || got.Privileged {
		t.Fatalf("code=%d called=%d options=%+v stderr=%s", code, called, got, errOut.String())
	}
	var plan optimize.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.ReadOnly {
		t.Fatal("plan must be read-only")
	}
	if len(plan.Recommendations) != 1 || plan.Recommendations[0].Evidence[0].Details[0] != "used_percent=90" || plan.Recommendations[0].Confidence != health.ConfidenceMedium {
		t.Fatalf("source evidence or confidence lost: %+v", plan.Recommendations)
	}

	for _, args := range [][]string{{"optimize", "--bogus"}, {"optimize", "stray"}, {"optimize", "--sample", "1ns"}, {"optimize", "--sample", "11s"}, {"optimize", "--sample", "-1s"}, {"optimize", "--timeout", "0"}, {"optimize", "--apply"}, {"optimize", "--history"}, {"optimize", "--include-sensitive"}, {"optimize", "--privileged"}, {"optimize", "--interval", "1s"}} {
		called = 0
		if gotCode := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &errOut, Deps: deps}); gotCode != int(exitUsage) || called != 0 {
			t.Fatalf("args=%v code=%d called=%d", args, gotCode, called)
		}
	}
	called = 0
	out.Reset()
	errOut.Reset()
	if gotCode := Run(context.Background(), RunConfig{Args: []string{"optimize", "--help"}, Out: &out, ErrOut: &errOut, Deps: deps}); gotCode != 0 || called != 0 {
		t.Fatalf("help code=%d called=%d", gotCode, called)
	}
	if out.String() != optimizeUsage || errOut.Len() != 0 {
		t.Fatalf("help must appear once on stdout: out=%q err=%q", out.String(), errOut.String())
	}
	if gotCode := Run(context.Background(), RunConfig{Args: []string{"optimize", "--help"}, Out: optimizeFailWriter{}, ErrOut: &errOut, Deps: deps}); gotCode != int(exitFailure) {
		t.Fatalf("help writer error code=%d", gotCode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called = 0
	if gotCode := Run(ctx, RunConfig{Args: []string{"optimize"}, Out: &out, ErrOut: &errOut, Deps: deps}); gotCode != int(exitInterrupted) || called != 0 {
		t.Fatalf("canceled code=%d called=%d", gotCode, called)
	}
	called = 0
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	if gotCode := Run(deadlineCtx, RunConfig{Args: []string{"optimize"}, Out: &out, ErrOut: &errOut, Deps: deps}); gotCode != int(exitFailure) || called != 0 {
		t.Fatalf("expired deadline code=%d called=%d", gotCode, called)
	}
	called = 0
	cancelDuring := Deps{Collect: func(ctx context.Context, _ health.Options) health.Report { called++; return health.Report{} }}
	ctx, cancel = context.WithCancel(context.Background())
	cancelDuring.Collect = func(ctx context.Context, _ health.Options) health.Report { called++; cancel(); return health.Report{} }
	if gotCode := Run(ctx, RunConfig{Args: []string{"optimize"}, Out: &out, ErrOut: &errOut, Deps: cancelDuring}); gotCode != int(exitInterrupted) || called != 1 {
		t.Fatalf("cancel during collection code=%d called=%d", gotCode, called)
	}
	for _, args := range [][]string{{"optimize", "--json"}, {"optimize"}} {
		if gotCode := Run(context.Background(), RunConfig{Args: args, Out: optimizeFailWriter{}, ErrOut: &errOut, Deps: deps}); gotCode != int(exitFailure) {
			t.Fatalf("writer error args=%v code=%d", args, gotCode)
		}
	}
}

func TestOptimizeHumanOutputSanitizedAndNoRecommendationsIsNotHealthy(t *testing.T) {
	var out bytes.Buffer
	p := optimize.Plan{ReadOnly: true, Capabilities: optimize.CapabilitySet{CPU: optimize.Capability{Status: optimize.CapabilityUnavailable, Reason: "bad\x1b[31m\nreason"}}, Limitations: []string{"limit\x00ation"}, Recommendations: []optimize.Recommendation{{ID: "id", Summary: "summary\x1b[31m\ncontinued", Confidence: health.ConfidenceHigh, Evidence: []optimize.SourceEvidence{{FindingID: "finding", Summary: "evidence\x1b[31m", Details: []string{"detail\x00bad"}}}, Action: optimize.SuggestedAction{Description: "action\x1b[31m", Command: "cmd\x00bad"}}}}
	if err := printOptimize(&out, p); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte("\x1b")) || bytes.Contains(out.Bytes(), []byte("\x00")) || bytes.Contains(out.Bytes(), []byte("bad\nreason")) || bytes.Contains(out.Bytes(), []byte("No actionable recommendations from available observations")) || bytes.Contains(out.Bytes(), []byte("healthy")) {
		t.Fatalf("unexpected output: %q", out.String())
	}
	if err := printOptimize(optimizeFailWriter{}, p); err == nil {
		t.Fatal("expected writer error")
	}
	out.Reset()
	if err := printOptimize(&out, optimize.Plan{ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("No actionable recommendations from available observations")) || bytes.Contains(out.Bytes(), []byte("healthy")) {
		t.Fatalf("empty plan must not imply health: %s", out.String())
	}
}
