package process

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

type ProcessTargetKind uint8

const (
	TargetPID ProcessTargetKind = iota + 1
	TargetTCPPort
)

type ProcessTarget struct {
	Kind ProcessTargetKind
	PID  int
	Port int
}

type ProcessSignal uint8

const SignalTerminate ProcessSignal = 1

type ProcessErrorKind uint8

const (
	ProcessInvalidTarget ProcessErrorKind = iota + 1
	ProcessNotFound
	ProcessAmbiguous
	ProcessDiscoveryFailed
	ProcessIdentityChanged
	ProcessSignalFailed
)

type ProcessError struct {
	Kind ProcessErrorKind
	PIDs []int
	Err  error
}

func (e *ProcessError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("process operation failed (%d): %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("process operation failed (%d), pids=%v", e.Kind, e.PIDs)
}
func (e *ProcessError) Unwrap() error { return e.Err }

type ProcessResult struct {
	PID    int
	Signal ProcessSignal
}

type ProcessOwner struct {
	PID      int
	Identity string
}

type Finder interface {
	Find(context.Context, ProcessTarget) ([]ProcessOwner, error)
}
type Signaller interface{ Signal(int, os.Signal) error }

type controller struct {
	finder    Finder
	signaller Signaller
}

func NewController(finder Finder, signaller Signaller) ProcessController {
	return controller{finder: finder, signaller: signaller}
}
func (c controller) Find(ctx context.Context, target ProcessTarget) ([]int, error) {
	if err := ValidateTarget(target); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owners, err := c.finder.Find(ctx, target)
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(owners))
	for _, o := range owners {
		pids = append(pids, o.PID)
	}
	return pids, nil
}

type ProcessController interface {
	Find(context.Context, ProcessTarget) ([]int, error)
	Terminate(context.Context, ProcessTarget) (ProcessResult, error)
}

func (c controller) Terminate(ctx context.Context, target ProcessTarget) (ProcessResult, error) {
	if err := ValidateTarget(target); err != nil {
		return ProcessResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	owners, err := c.finder.Find(ctx, target)
	if err != nil {
		return ProcessResult{}, err
	}
	if len(owners) == 0 {
		return ProcessResult{}, &ProcessError{Kind: ProcessNotFound}
	}
	if len(owners) != 1 {
		return ProcessResult{}, &ProcessError{Kind: ProcessAmbiguous, PIDs: ownerPIDs(owners)}
	}
	owner := owners[0]
	if owner.PID <= 1 || owner.Identity == "" {
		return ProcessResult{}, &ProcessError{Kind: ProcessInvalidTarget}
	}
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	current, e := c.finder.Find(ctx, target)
	if e != nil {
		return ProcessResult{}, &ProcessError{Kind: ProcessDiscoveryFailed, Err: e}
	}
	if len(current) != 1 || current[0] != owner {
		return ProcessResult{}, &ProcessError{Kind: ProcessIdentityChanged, PIDs: ownerPIDs(current)}
	}
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}
	if err := c.signaller.Signal(owner.PID, syscall.SIGTERM); err != nil {
		return ProcessResult{}, &ProcessError{Kind: ProcessSignalFailed, PIDs: []int{owner.PID}, Err: err}
	}
	return ProcessResult{PID: owner.PID, Signal: SignalTerminate}, nil
}

func ownerPIDs(os []ProcessOwner) []int {
	p := make([]int, 0, len(os))
	for _, o := range os {
		p = append(p, o.PID)
	}
	return p
}

func ValidateTarget(t ProcessTarget) error {
	switch t.Kind {
	case TargetPID:
		if t.PID <= 1 {
			return &ProcessError{Kind: ProcessInvalidTarget}
		}
	case TargetTCPPort:
		if t.Port < 1 || t.Port > 65535 {
			return &ProcessError{Kind: ProcessInvalidTarget}
		}
	default:
		return &ProcessError{Kind: ProcessInvalidTarget}
	}
	return nil
}
