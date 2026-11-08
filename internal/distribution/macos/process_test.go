package macos

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gabriwl165/daffy/internal/distribution/process"
)

type fakeProcessRunner struct {
	results []ProcessCommandResult
	calls   int
}

func TestProcessFinderMissingToolAndFailedIdentityLookup(t *testing.T) {
	f := &fakeProcessRunner{results: []ProcessCommandResult{{Err: fmt.Errorf("executable missing")}}}
	_, err := (darwinFinder{f}).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 80})
	var pe *process.ProcessError
	if !errors.As(err, &pe) || pe.Kind != process.ProcessDiscoveryFailed {
		t.Fatalf("missing tool error=%v", err)
	}
	f = &fakeProcessRunner{results: []ProcessCommandResult{{Stdout: []byte("51\n")}, {Err: fmt.Errorf("ps failed")}}}
	_, err = (darwinFinder{f}).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 80})
	if !errors.As(err, &pe) || pe.Kind != process.ProcessDiscoveryFailed {
		t.Fatalf("failed ps error=%v", err)
	}
}

func (f *fakeProcessRunner) Run(context.Context, string, ...string) ProcessCommandResult {
	r := f.results[f.calls]
	f.calls++
	return r
}
func TestProcessFinderParsesDeduplicatesAndCapturesIdentity(t *testing.T) {
	f := &fakeProcessRunner{results: []ProcessCommandResult{{Stdout: []byte("51\n51\n")}, {Stdout: []byte("Mon Jan  1 00:00:00 2024\n")}}}
	owners, err := (darwinFinder{f}).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 80})
	if err != nil || len(owners) != 1 || owners[0].PID != 51 || owners[0].Identity == "" {
		t.Fatalf("owners=%v err=%v", owners, err)
	}
}
func TestProcessFinderRejectsWarningsAndTruncation(t *testing.T) {
	for _, r := range []ProcessCommandResult{{Stdout: []byte("51\n"), Stderr: []byte("warning")}, {Stdout: []byte(strings.Repeat("x", 10)), Truncated: true}, {Stdout: []byte("garbage\n")}} {
		f := &fakeProcessRunner{results: []ProcessCommandResult{r}}
		_, err := (darwinFinder{f}).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 80})
		var pe *process.ProcessError
		if !errors.As(err, &pe) || pe.Kind != process.ProcessDiscoveryFailed {
			t.Fatalf("result=%+v err=%v", r, err)
		}
	}
}
