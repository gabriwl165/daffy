package distribution

import (
	"context"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
	"syscall"
	"testing"
)

type fixedFinder struct {
	owners []process.ProcessOwner
	err    error
	calls  int
}

func (f fixedFinder) Find(context.Context, ProcessTarget) ([]process.ProcessOwner, error) {
	return f.owners, f.err
}

type captureSignal struct {
	pid    int
	signal os.Signal
}

func (s *captureSignal) Signal(pid int, sig os.Signal) error { s.pid = pid; s.signal = sig; return nil }

func TestTerminateRequiresOneSafeOwner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owners []process.ProcessOwner
		kind   ProcessErrorKind
	}{{"none", nil, ProcessNotFound}, {"many", []process.ProcessOwner{{PID: 42, Identity: "a"}, {PID: 43, Identity: "b"}}, ProcessAmbiguous}, {"unsafe", []process.ProcessOwner{{PID: 1, Identity: "a"}}, ProcessInvalidTarget}} {
		t.Run(tc.name, func(t *testing.T) {
			sig := &captureSignal{}
			_, err := process.NewController(fixedFinder{owners: tc.owners}, sig).Terminate(context.Background(), ProcessTarget{Kind: TargetPID, PID: 42})
			if pe, ok := err.(*ProcessError); !ok || pe.Kind != tc.kind {
				t.Fatalf("error=%v", err)
			}
			if sig.pid != 0 {
				t.Fatalf("signalled unexpectedly: %d", sig.pid)
			}
		})
	}
}
func TestTerminateUsesOnlySIGTERM(t *testing.T) {
	sig := &captureSignal{}
	_, err := process.NewController(fixedFinder{owners: []process.ProcessOwner{{PID: 42, Identity: "same"}}}, sig).Terminate(context.Background(), ProcessTarget{Kind: TargetPID, PID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if sig.pid != 42 || sig.signal != syscall.SIGTERM {
		t.Fatalf("signal=%v pid=%d", sig.signal, sig.pid)
	}
}
func TestValidateTarget(t *testing.T) {
	for _, target := range []ProcessTarget{{Kind: TargetPID, PID: 1}, {Kind: TargetTCPPort, Port: 0}, {Kind: TargetTCPPort, Port: 65536}, {Kind: 0}} {
		if process.ValidateTarget(target) == nil {
			t.Errorf("accepted %#v", target)
		}
	}
}

type sequenceFinder struct {
	calls  int
	owners [][]process.ProcessOwner
	errs   []error
}

func (f *sequenceFinder) Find(context.Context, ProcessTarget) ([]process.ProcessOwner, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i >= len(f.owners) {
		i = len(f.owners) - 1
	}
	return f.owners[i], nil
}
func TestTerminateRechecksIdentityAndFailurePaths(t *testing.T) {
	initial := process.ProcessOwner{PID: 42, Identity: "first"}
	changed := process.ProcessOwner{PID: 42, Identity: "second"}
	for _, tc := range []struct {
		name   string
		next   []process.ProcessOwner
		err    error
		cancel bool
		kind   ProcessErrorKind
	}{{"changed identity", []process.ProcessOwner{changed}, nil, false, ProcessIdentityChanged}, {"missing", nil, nil, false, ProcessIdentityChanged}, {"discovery failure", nil, context.DeadlineExceeded, false, ProcessDiscoveryFailed}, {"canceled", []process.ProcessOwner{initial}, nil, true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			finder := &sequenceFinder{owners: [][]process.ProcessOwner{{initial}, tc.next}, errs: []error{nil, tc.err}}
			sig := &captureSignal{}
			ctx := context.Background()
			if tc.cancel {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			_, err := process.NewController(finder, sig).Terminate(ctx, ProcessTarget{Kind: TargetPID, PID: 42})
			if tc.kind != 0 {
				pe, ok := err.(*ProcessError)
				if !ok || pe.Kind != tc.kind {
					t.Fatalf("error=%v", err)
				}
			} else if err == nil {
				t.Fatal("expected cancellation")
			}
			if sig.pid != 0 {
				t.Fatalf("signalled %d", sig.pid)
			}
		})
	}
}
