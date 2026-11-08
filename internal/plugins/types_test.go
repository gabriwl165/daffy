package plugins

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testPlugin struct {
	id      PluginID
	ops     []Operation
	calls   []OperationID
	err     error
	analyze func(context.Context, Request) (Outcome, error)
}

func (p *testPlugin) Describe() Descriptor { return Descriptor{ID: p.id, Operations: p.ops} }
func (p *testPlugin) Analyze(ctx context.Context, r Request) (Outcome, error) {
	p.calls = append(p.calls, r.Operation)
	if p.analyze != nil {
		return p.analyze(ctx, r)
	}
	return Outcome{Details: Details{Bazel: &BazelDetails{}}}, p.err
}
func op(id OperationID, def bool) Operation {
	return Operation{ID: id, Summary: string(id), Default: def, Access: AccessOffline}
}

func TestRegistryValidationAndList(t *testing.T) {
	duplicate := &testPlugin{id: "z", ops: []Operation{op(OperationDisk, true), op(OperationDisk, false)}}
	if _, err := NewRegistry(duplicate); err == nil {
		t.Fatal("expected duplicate operation rejection")
	}
	a := &testPlugin{id: "z", ops: []Operation{op(OperationDisk, true)}}
	b := &testPlugin{id: "a", ops: []Operation{op(OperationDisk, true)}}
	r, err := NewRegistry(a, b)
	if err != nil {
		t.Fatal(err)
	}
	list := r.List()
	if list[0].ID != "a" || list[1].ID != "z" {
		t.Fatal(list)
	}
	list[0].Operations[0].ID = "changed"
	if r.List()[0].Operations[0].ID != OperationDisk {
		t.Fatal("List leaked internal slice")
	}
}
func TestRegistryRejectsInvalidDescriptors(t *testing.T) {
	for name, p := range map[string]Plugin{
		"invalid access": &testPlugin{id: "p", ops: []Operation{{ID: OperationDisk, Summary: "disk", Access: "write"}}},
		"invalid id":     &testPlugin{id: "Bad ID", ops: []Operation{op(OperationDisk, true)}},
		"required path":  &testPlugin{id: "p", ops: []Operation{{ID: OperationDisk, Summary: "disk", Access: AccessFilesystem, Options: SupportedOptions{RequirePath: true}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewRegistry(p); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if _, err := NewRegistry(&testPlugin{id: "x", ops: []Operation{op(OperationDisk, true)}}, &testPlugin{id: "x", ops: []Operation{op(OperationRuntime, true)}}); err == nil {
		t.Fatal("duplicate plugin accepted")
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("nil plugin accepted")
	}
}
func TestRunDefaultsAndExplicitOperation(t *testing.T) {
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true), op(OperationRuntime, false)}}
	r, _ := NewRegistry(p)
	if _, err := r.Run(context.Background(), PluginDocker, Request{}); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 1 || p.calls[0] != OperationDisk {
		t.Fatal(p.calls)
	}
	if _, err := r.Run(context.Background(), PluginDocker, Request{Operation: OperationRuntime}); err != nil {
		t.Fatal(err)
	}
	if p.calls[1] != OperationRuntime {
		t.Fatal(p.calls)
	}
}
func TestRunPartialErrorAndCancellation(t *testing.T) {
	want := errors.New("partial")
	p := &testPlugin{id: PluginBazel, ops: []Operation{op(OperationDisk, true)}, err: want}
	r, _ := NewRegistry(p)
	report, err := r.Run(context.Background(), PluginBazel, Request{})
	if !errors.Is(err, want) || report.Outcomes[0].Details.Bazel == nil || report.Outcomes[0].Status != StatusPartial {
		t.Fatalf("%+v %v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err = r.Run(ctx, PluginBazel, Request{})
	if err == nil || report.Outcomes[0].Status != StatusCancelled {
		t.Fatalf("%+v %v", report, err)
	}
}
func TestRunRejectsOptions(t *testing.T) {
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true)}}
	r, _ := NewRegistry(p)
	if _, err := r.Run(context.Background(), PluginDocker, Request{Options: Options{Path: "/tmp"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestRunBundleContinuesAndPreservesFailure(t *testing.T) {
	want := errors.New("disk problem")
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true), op(OperationRuntime, true)}, analyze: func(_ context.Context, r Request) (Outcome, error) {
		if r.Operation == OperationDisk {
			return Outcome{Details: Details{Docker: &DockerDetails{Context: "selected"}}}, want
		}
		return Outcome{Status: StatusComplete}, nil
	}}
	r, _ := NewRegistry(p)
	report, err := r.Run(context.Background(), PluginDocker, Request{})
	if !errors.Is(err, want) || len(report.Outcomes) != 2 || report.Outcomes[0].Operation != OperationDisk || report.Outcomes[0].Status != StatusPartial || report.Outcomes[0].Details.Docker == nil || report.Outcomes[1].Status != StatusComplete || report.Status != StatusPartial || report.SchemaVersion != SchemaVersion {
		t.Fatalf("%+v %v", report, err)
	}
}
func TestRunCancellationMarksRemaining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true), op(OperationRuntime, true)}, analyze: func(_ context.Context, r Request) (Outcome, error) {
		cancel()
		return Outcome{Status: StatusComplete}, nil
	}}
	r, _ := NewRegistry(p)
	report, err := r.Run(ctx, PluginDocker, Request{})
	if err == nil || len(p.calls) != 1 || len(report.Outcomes) != 2 || report.Outcomes[0].Status != StatusCancelled || report.Outcomes[1].Status != StatusCancelled {
		t.Fatalf("calls %v report %+v err %v", p.calls, report, err)
	}
}

func TestRunWithTimeoutIsPerOperationAndParentCancellationWins(t *testing.T) {
	var seen []time.Duration
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true), op(OperationRuntime, true)}, analyze: func(ctx context.Context, r Request) (Outcome, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("operation has no deadline")
		}
		seen = append(seen, time.Until(deadline))
		if r.Operation == OperationDisk {
			<-ctx.Done()
			return Outcome{}, ctx.Err()
		}
		return Outcome{Status: StatusComplete}, nil
	}}
	r, _ := NewRegistry(p)
	report, err := r.RunWithTimeout(context.Background(), PluginDocker, Request{}, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || len(p.calls) != 2 || report.Outcomes[0].Status != StatusCancelled || report.Outcomes[1].Status != StatusComplete || len(seen) != 2 || seen[1] < 10*time.Millisecond {
		t.Fatalf("calls=%v budgets=%v report=%+v err=%v", p.calls, seen, report, err)
	}
	one := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true)}, analyze: func(ctx context.Context, _ Request) (Outcome, error) {
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}}
	or, _ := NewRegistry(one)
	if _, err := or.RunWithTimeout(context.Background(), PluginDocker, Request{Operation: OperationDisk}, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("explicit operation timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.calls = nil
	if _, err := r.RunWithTimeout(ctx, PluginDocker, Request{}, time.Second); !errors.Is(err, context.Canceled) || len(p.calls) != 0 {
		t.Fatalf("parent cancellation err=%v calls=%v", err, p.calls)
	}
}

func TestRunPreservesUnavailableAndCanonicalizesOperation(t *testing.T) {
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true)}, analyze: func(context.Context, Request) (Outcome, error) {
		return Outcome{Operation: OperationRuntime, Status: StatusUnavailable}, errors.New("tool unavailable")
	}}
	r, err := NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	report, err := r.Run(context.Background(), PluginDocker, Request{})
	if err == nil || report.Status != StatusPartial || report.Outcomes[0].Status != StatusUnavailable || report.Outcomes[0].Operation != OperationDisk {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestRunRejectsUnknownAndOptionsBeforeAnalyze(t *testing.T) {
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationDisk, true), op(OperationRuntime, true)}}
	r, _ := NewRegistry(p)
	if _, err := r.Run(context.Background(), "missing", Request{}); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), PluginDocker, Request{Operation: "missing"}); !errors.Is(err, ErrUnknownOperation) {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), PluginDocker, Request{Operation: OperationDisk, Options: Options{Path: "x"}}); !errors.Is(err, ErrInvalid) || len(p.calls) != 0 {
		t.Fatalf("err %v calls %v", err, p.calls)
	}
	noDefaults := &testPlugin{id: "empty", ops: []Operation{op(OperationDisk, false)}}
	rr, _ := NewRegistry(noDefaults)
	if _, err := rr.Run(context.Background(), "empty", Request{}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
