package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/distribution"
)

type fakeProcess struct {
	target distribution.ProcessTarget
	calls  int
	result distribution.ProcessResult
	err    error
}

func (f *fakeProcess) Find(context.Context, distribution.ProcessTarget) ([]int, error) {
	return nil, nil
}
func (f *fakeProcess) Terminate(_ context.Context, t distribution.ProcessTarget) (distribution.ProcessResult, error) {
	f.calls++
	f.target = t
	return f.result, f.err
}
func TestKillCLI(t *testing.T) {
	for _, tc := range []struct {
		args []string
		kind distribution.ProcessTargetKind
	}{{[]string{"pid", "42"}, distribution.TargetPID}, {[]string{"port", "80"}, distribution.TargetTCPPort}} {
		f := &fakeProcess{result: distribution.ProcessResult{PID: 42, Signal: distribution.SignalTerminate}}
		var out, errOut bytes.Buffer
		if c := Run(context.Background(), RunConfig{Args: append([]string{"kill"}, tc.args...), Out: &out, ErrOut: &errOut, Deps: Deps{Process: f}}); c != 0 || f.calls != 1 || f.target.Kind != tc.kind {
			t.Fatalf("%v: %d %+v", tc.args, c, f.target)
		}
	}
	for _, a := range [][]string{{"pid", "+2"}, {"pid", "1"}, {"pid", "9223372036854775808"}, {"pid", "999999999999999999999"}, {"port", "65536"}, {"port", "2x"}, {"bad", "4"}, {"pid"}, {"pid", "2", "extra"}} {
		f := &fakeProcess{}
		var o, e bytes.Buffer
		if c := Run(context.Background(), RunConfig{Args: append([]string{"kill"}, a...), Out: &o, ErrOut: &e, Deps: Deps{Process: f}}); c != 2 || f.calls != 0 {
			t.Errorf("%v: %d calls %d", a, c, f.calls)
		}
	}
	fmax := &fakeProcess{result: distribution.ProcessResult{PID: 9223372036854775807, Signal: distribution.SignalTerminate}}
	var mo, me bytes.Buffer
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "9223372036854775807"}, Out: &mo, ErrOut: &me, Deps: Deps{Process: fmax}}); c != 0 {
		t.Fatalf("max int rejected: %d", c)
	}
	var o, e bytes.Buffer
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "--help"}, Out: &o, ErrOut: &e}); c != 0 {
		t.Fatal(c)
	}
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: &o, ErrOut: &e}); c != 1 {
		t.Fatal(c)
	}
	f := &fakeProcess{err: context.Canceled}
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: &o, ErrOut: &e, Deps: Deps{Process: f}}); c != 130 {
		t.Fatal(c)
	}
	f = &fakeProcess{err: errors.New("secret")}
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: &o, ErrOut: &e, Deps: Deps{Process: f}}); c != 1 {
		t.Fatal(c)
	}
	for _, tc := range []struct {
		kind     distribution.ProcessErrorKind
		err      error
		contains string
	}{
		{distribution.ProcessAmbiguous, nil, "PIDs: 4, 7"}, {distribution.ProcessDiscoveryFailed, errors.New("lsof unavailable"), "lsof unavailable"},
		{distribution.ProcessNotFound, nil, "no matching"}, {distribution.ProcessIdentityChanged, nil, "identity changed"},
		{distribution.ProcessInvalidTarget, nil, "invalid process"}, {distribution.ProcessSignalFailed, os.ErrPermission, "permission denied"},
	} {
		var eo bytes.Buffer
		f := &fakeProcess{err: &distribution.ProcessError{Kind: tc.kind, PIDs: []int{4, 7}, Err: tc.err}}
		if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: &bytes.Buffer{}, ErrOut: &eo, Deps: Deps{Process: f}}); c != 1 || !strings.Contains(eo.String(), tc.contains) {
			t.Errorf("kind %d: %d %q", tc.kind, c, eo.String())
		}
	}
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	f = &fakeProcess{}
	if c := Run(deadline, RunConfig{Args: []string{"kill", "pid", "2"}, Out: &o, ErrOut: &e, Deps: Deps{Process: f}}); c != 1 || f.calls != 0 {
		t.Fatalf("deadline: %d calls=%d", c, f.calls)
	}
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: failWriter{}, ErrOut: ioDiscard{}, Deps: Deps{Process: &fakeProcess{result: distribution.ProcessResult{PID: 3, Signal: distribution.SignalTerminate}}}}); c != 1 {
		t.Fatalf("writer error: %d", c)
	}
	if c := Run(context.Background(), RunConfig{Args: []string{"kill", "pid", "2"}, Out: &bytes.Buffer{}, ErrOut: ioDiscard{}, Deps: Deps{Process: &fakeProcess{result: distribution.ProcessResult{PID: 1, Signal: distribution.SignalTerminate}}}}); c != 1 {
		t.Fatalf("invalid result pid: %d", c)
	}
}
