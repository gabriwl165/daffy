package docker

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gabriwl165/daffy/internal/plugins"
)

func TestCrashClassificationOOMExitAndStoppedUnhealthy(t *testing.T) {
	plugin := New(&fakeRunner{})
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, status string
		code         int64
		oom          bool
		want         plugins.DockerCrashClassification
	}{{"exit137", "exited", 137, false, plugins.DockerCrashNonzeroExit}, {"oom", "exited", 137, true, plugins.DockerCrashRecordedOOM}, {"stopped unhealthy", "exited", 0, false, ""}} {
		d := &plugins.DockerDetails{}
		c := inspectContainer{State: &inspectState{Status: tc.status, ExitCode: &tc.code, OOMKilled: &tc.oom, Health: &inspectHealth{Status: "unhealthy"}}}
		if err := addCrashDiagnostic(context.Background(), plugin, d, c, plugins.DockerContainerConfig{ID: id, Name: tc.name}); err != nil {
			t.Fatal(err)
		}
		if tc.want == "" && len(d.Crashes) != 0 || tc.want != "" && (len(d.Crashes) != 1 || d.Crashes[0].Classification != tc.want) {
			t.Fatalf("%s: %#v", tc.name, d.Crashes)
		}
	}
}

func TestCrashEvidenceOptInEventsAndFailure(t *testing.T) {
	id := strings.Repeat("b", 64)
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"ID":"` + id + `","Names":"x"}`), []byte(`[ {"Id":"` + id + `","Name":"/x","RestartCount":2,"State":{"Status":"exited","ExitCode":137,"OOMKilled":false},"HostConfig":{"Memory":1,"Privileged":false}} ]`), []byte("secret log\n"), []byte("die\nstart\nrestart\n")}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig, Options: plugins.Options{CrashInvestigation: true}})
	if err != nil || o.Status != plugins.StatusComplete {
		t.Fatalf("opt-in evidence: %v %#v", err, o)
	}
	d := o.Details.Docker.Crashes[0]
	if d.Classification != plugins.DockerCrashNonzeroExit || d.RecentDieEvents != 1 || d.RecentStartEvents != 1 || d.RecentRestartEvents != 1 || !d.HistoryIncomplete || !strings.Contains(strings.Join(d.RecentLogs, " "), "secret log") {
		t.Fatalf("diagnostic: %#v", d)
	}
	for _, args := range r.args {
		if args[0] == "events" && (!containsArg(args, "--since") || !containsArg(args, "--until")) {
			t.Fatalf("unbounded events args: %v", args)
		}
	}
	// Default configuration does not expose logs or health output.
	clean := &plugins.DockerDetails{}
	_ = addCrashDiagnostic(context.Background(), New(&fakeRunner{}), clean, inspectContainer{State: &inspectState{Status: "exited", ExitCode: ptrInt(2), Health: &inspectHealth{Status: "unhealthy", Log: []inspectHealthLog{{Output: "private"}}}}}, plugins.DockerContainerConfig{ID: id})
	if len(clean.Crashes) != 1 || len(clean.Crashes[0].RecentLogs) != 0 || len(clean.Crashes[0].HealthLog) != 0 {
		t.Fatal("private output exposed without opt-in")
	}
}

func ptrInt(i int64) *int64 { return &i }

func TestCommandRunnerOptInMergesLogStderrOnly(t *testing.T) {
	path := t.TempDir() + "/fake-docker"
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'stdout\\n'; printf 'stderr\\n' >&2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := &CommandRunner{Binary: path}
	plain, err := r.Run(context.Background(), "logs")
	if err != nil || strings.Contains(string(plain), "stderr") {
		t.Fatalf("default stderr leaked: %q %v", plain, err)
	}
	merged, err := r.RunWithStderr(context.Background(), "logs")
	if err != nil || !strings.Contains(string(merged), "stdout") || !strings.Contains(string(merged), "stderr") {
		t.Fatalf("opt-in stderr absent: %q %v", merged, err)
	}
}

func TestEvidenceFailureIsPartialAndConfigPreserved(t *testing.T) {
	id := strings.Repeat("c", 64)
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"ID":"` + id + `","Names":"x"}`), []byte(`[{"Id":"` + id + `","Name":"/x","State":{"Status":"exited","ExitCode":2},"HostConfig":{"Memory":1,"Privileged":false}}]`)}, errs: []error{nil, nil, ErrCommandFailed}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig, Options: plugins.Options{CrashInvestigation: true}})
	if err == nil || o.Status != plugins.StatusPartial || len(o.Details.Docker.Config) != 1 || len(o.Details.Docker.Crashes) != 1 || len(o.Details.Docker.Crashes[0].EvidenceIssues) == 0 {
		t.Fatalf("partial collection lost data: %#v %v", o, err)
	}
}

func TestLogTailRequiresOptIn(t *testing.T) {
	p := New(&fakeRunner{})
	if _, err := p.Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig, Options: plugins.Options{LogTail: 10}}); err == nil {
		t.Fatal("log tail without investigation accepted")
	}
}
