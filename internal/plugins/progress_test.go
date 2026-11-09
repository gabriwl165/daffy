package plugins

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestObservedOutcomesArriveBeforeNextOperation(t *testing.T) {
	var observed []Outcome
	want := errors.New("unavailable fixture")
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationConfig, true), op(OperationRuntime, true), op(OperationDisk, true)}}
	p.analyze = func(_ context.Context, req Request) (Outcome, error) {
		if len(observed) != len(p.calls)-1 {
			t.Fatal("next operation started before prior observation")
		}
		if req.Operation == OperationDisk {
			return Outcome{}, want
		}
		return Outcome{}, nil
	}
	r, _ := NewRegistry(p)
	report, err := r.RunWithTimeoutObserved(context.Background(), PluginDocker, Request{}, time.Second, func(o Outcome) {
		observed = append(observed, o)
	})
	if !errors.Is(err, want) || len(observed) != 3 || report.Status != StatusPartial {
		t.Fatalf("observed=%+v report=%+v err=%v", observed, report, err)
	}
	for i, o := range observed {
		if o.Operation != report.Outcomes[i].Operation || o.Status != report.Outcomes[i].Status {
			t.Fatal("observer did not receive finalized outcome")
		}
	}
}

func TestObserverReceivesSkippedOutcomes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &testPlugin{id: PluginDocker, ops: []Operation{op(OperationConfig, true), op(OperationDisk, true)}}
	r, _ := NewRegistry(p)
	var observed []Outcome
	_, err := r.RunWithTimeoutObserved(ctx, PluginDocker, Request{}, time.Second, func(o Outcome) {
		observed = append(observed, o)
		cancel()
	})
	if !errors.Is(err, context.Canceled) || len(observed) != 2 || len(p.calls) != 1 || observed[1].Issues[0].Code != IssueSkipped {
		t.Fatalf("observed=%+v calls=%v err=%v", observed, p.calls, err)
	}
}
