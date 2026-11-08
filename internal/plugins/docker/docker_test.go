package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

type fakeRunner struct {
	outputs [][]byte
	errs    []error
	calls   int
	args    [][]string
}

type waitingRunner struct{}

func (waitingRunner) Run(ctx context.Context, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type runnerFunc func(context.Context, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, args ...string) ([]byte, error) { return f(ctx, args...) }

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	i := f.calls
	f.calls++
	f.args = append(f.args, append([]string(nil), args...))
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.outputs) {
		return f.outputs[i], nil
	}
	return nil, nil
}

func TestNativeDiskAndStats(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"3","Active":"1","Size":"1.2GB","Reclaimable":"100MB (10%)"}`), []byte(`{"ID":"` + id + `","Name":"web","CPUPerc":"0.1%","MemUsage":"1MiB / 2MiB","MemPerc":"50%","NetIO":"1kB / 2kB","BlockIO":"0B / 0B","PIDs":"1"}`)}}
	p := New(r)
	disk, e := p.Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk})
	if e != nil || disk.Details.Docker.DiskUsage[0].TotalCount != 3 {
		t.Fatalf("disk: %#v %v", disk, e)
	}
	runtime, e := p.Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime})
	if e != nil || runtime.Details.Docker.Runtime[0].CPUPerc != "0.1%" {
		t.Fatalf("runtime: %#v %v", runtime, e)
	}
	if strings.Join(r.args[0], " ") != "system df --format {{json .}}" || strings.Join(r.args[1], " ") != "stats --no-stream --no-trunc --format {{json .}}" {
		t.Fatalf("unexpected CLI args: %#v", r.args)
	}
}

func TestTargetSelectionContextAndNoUnqualifiedStats(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"ID":"` + id + `","Names":"api"}`), []byte(id + "\n"), []byte(`{"ID":"` + id + `","Name":"api","CPUPerc":"1%","MemUsage":"1/2","MemPerc":"50%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`)}}
	_, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Context: "dev", Containers: []string{"api"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArg(r.args[0], "--context") || !containsArg(r.args[0], "dev") || !containsArg(r.args[2], "--") {
		t.Fatalf("context/positional args: %#v", r.args)
	}
	r = &fakeRunner{outputs: [][]byte{[]byte(`{"ID":"` + id + `","Names":"api"}`)}}
	_, err = New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Containers: []string{"missing"}}})
	if !errors.Is(err, plugins.ErrInvalid) || r.calls != 1 {
		t.Fatalf("unmatched selector ran unsafe command: calls=%d err=%v", r.calls, err)
	}
	for _, bad := range []string{"bad;docker", "--help"} {
		if _, err = New(&fakeRunner{}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig, Options: plugins.Options{Context: bad}}); !errors.Is(err, plugins.ErrInvalid) {
			t.Fatalf("context %q: %v", bad, err)
		}
	}
}

func TestComposeProjectGroupingAndFiltering(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	inspect := `[{"Id":"` + id + `","Name":"/api","State":{"Status":"restarting","Health":{"Status":"unhealthy"}},"Config":{"Labels":{"com.docker.compose.project":"shop","com.docker.compose.service":"api","untrusted":"must-not-leak"}},"HostConfig":{"Memory":1,"Privileged":false}}]`
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"ID":"` + id + `","Names":"api"}`), []byte(inspect)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig, Options: plugins.Options{Project: "shop"}})
	if err != nil || len(o.Details.Docker.Config) != 1 {
		t.Fatalf("project result: %#v %v", o, err)
	}
	d := o.Details.Docker
	if d.Config[0].Project != "shop" || d.Config[0].Service != "api" || len(d.ComposeGroups) != 1 || d.ComposeGroups[0].Total != 1 || d.ComposeGroups[0].Unhealthy != 1 || d.ComposeGroups[0].Restarting != 1 {
		t.Fatalf("group metadata: %#v", d)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "must-not-leak") {
		t.Fatal("raw labels leaked")
	}
}

func TestRuntimeSamplesRankPeaksAndKeepChurn(t *testing.T) {
	id1 := strings.Repeat("a", 64)
	id2 := strings.Repeat("b", 64)
	row := func(id, cpu string) []byte {
		return []byte(`{"ID":"` + id + `","Name":"web","CPUPerc":"` + cpu + `%","MemUsage":"1MiB / 2MiB","MemPerc":"50%","NetIO":"1kB / 2kB","BlockIO":"0B / 0B","PIDs":"1"}`)
	}
	r := &fakeRunner{outputs: [][]byte{row(id1, "12"), row(id2, "15")}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Samples: 2, Interval: 100 * time.Millisecond, RuntimeTop: 1, CPUThreshold: 10}})
	if err != nil {
		t.Fatal(err)
	}
	trend := o.Details.Docker.RuntimeTrend
	if trend == nil || trend.SampleCount != 2 || len(trend.Containers) != 1 || trend.Containers[0].ID != id2 || !trend.Containers[0].CPUThresholdExceeded {
		t.Fatalf("trend: %#v", trend)
	}
}
func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
func TestEmptyAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		data string
		ok   bool
	}{{"", false}, {`{"Type":"Images","TotalCount":"x","Active":"0","Size":"1GB","Reclaimable":"0B"}`, false}, {"", true}} {
		r := &fakeRunner{outputs: [][]byte{[]byte(tc.data)}}
		p := New(r)
		op := plugins.OperationDisk
		if tc.ok {
			op = plugins.OperationRuntime
		}
		_, err := p.Analyze(context.Background(), plugins.Request{Operation: op})
		if (err == nil) != tc.ok {
			t.Errorf("data %q: err %v", tc.data, err)
		}
	}
}
func listing(ids []string) []byte {
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "{\"ID\":%q,\"Names\":%q}\n", id, "container")
	}
	return []byte(b.String())
}
func TestConfigEmptyAndPartialInspect(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	good := `[{"Id":"` + id + `","Name":"/app","State":{"Status":"running"},"HostConfig":{"Memory":0,"Privileged":true,"NanoCpus":0,"PidsLimit":0,"ReadonlyRootfs":false,"LogConfig":{"Type":"json-file"}},"Config":{"Env":["SUPER_SECRET"],"Cmd":["SECRET_ARG"]}}]`
	r := &fakeRunner{outputs: [][]byte{[]byte("")}}
	o, e := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if e != nil || len(o.Details.Docker.Config) != 0 {
		t.Fatalf("empty: %v %#v", e, o)
	}
	r = &fakeRunner{outputs: [][]byte{listing([]string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}), []byte(good)}}
	o, e = New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if e != nil || len(o.Details.Docker.Config) != 1 || len(o.Findings) != 2 || !strings.Contains(o.Findings[0].Summary, id) {
		t.Fatalf("inspect: %v %#v", e, o)
	}
	encoded, _ := json.Marshal(o)
	if strings.Contains(string(encoded), "SUPER_SECRET") || strings.Contains(string(encoded), "SECRET_ARG") {
		t.Fatal("secret config data leaked")
	}
	r = &fakeRunner{outputs: [][]byte{listing([]string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}), []byte("[]")}}
	o, e = New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if e == nil || o.Status != plugins.StatusPartial {
		t.Fatalf("missing batch should be partial: %v %#v", e, o)
	}
	r = &fakeRunner{outputs: [][]byte{listing([]string{id}), []byte(`[{"Id":"` + id + `","Name":"/x","State":{"Status":"running"},"HostConfig":{}}]`)}}
	o, e = New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if e == nil || o.Status != plugins.StatusPartial {
		t.Fatal("missing host config evidence must be partial")
	}
}

func TestConfigFailureDiagnostics(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		name, status, fields              string
		restarts                          int
		failure, severity, recommendation string
	}{
		{"oom", "exited", `,"OOMKilled":true,"ExitCode":137`, 0, "out-of-memory", string(plugins.SeverityWarning), "Review memory"},
		{"exit", "exited", `,"OOMKilled":false,"ExitCode":2`, 0, "exit code 2", string(plugins.SeverityWarning), "Review the container logs"},
		{"dead", "dead", `,"ExitCode":9`, 0, "exit code 9", string(plugins.SeverityWarning), "Review the container logs"},
		{"running", "running", `,"ExitCode":3`, 0, "", "", ""},
		{"absent", "running", "", 0, "", "", ""},
		{"history", "running", `,"OOMKilled":false,"ExitCode":0`, 2, "historically", string(plugins.SeverityInfo), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`[{"Id":%q,"Name":%q,"RestartCount":%d,"State":{"Status":%q%s},"HostConfig":{"Memory":1,"Privileged":false}}]`, id, "/"+tc.name, tc.restarts, tc.status, tc.fields)
			o, err := New(&fakeRunner{outputs: [][]byte{listing([]string{id}), []byte(payload)}}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
			if err != nil {
				t.Fatal(err)
			}
			cfg := o.Details.Docker.Config[0]
			if tc.fields == "" && (cfg.OOMKilled != nil || cfg.ExitCode != nil) {
				t.Fatalf("absent fields: %+v", cfg)
			}
			var f *plugins.Finding
			for i := range o.Findings {
				if o.Findings[i].Code == findingContainerFailure {
					f = &o.Findings[i]
				}
			}
			if tc.failure == "" {
				if f != nil {
					t.Fatalf("unexpected failure finding: %+v", f)
				}
				return
			}
			if f == nil || !strings.Contains(f.Summary, tc.failure) || string(f.Severity) != tc.severity {
				t.Fatalf("missing expected finding: %+v", o.Findings)
			}
			if tc.recommendation != "" && (f.Recommendation == "" || !strings.Contains(f.Recommendation, tc.recommendation)) {
				t.Fatalf("recommendation missing: %+v", f)
			}
			if tc.restarts > 0 && (!strings.Contains(f.Summary, "historically")) {
				t.Fatalf("historical wording: %+v", f)
			}
		})
	}
}

func TestAnalyzeValidationAndCommandRunner(t *testing.T) {
	r := &fakeRunner{}
	p := New(r)
	if _, e := p.Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{TopN: 1}}); !errors.Is(e, plugins.ErrInvalid) {
		t.Fatalf("options: %v", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Analyze(ctx, plugins.Request{Operation: plugins.OperationDisk}); e == nil {
		t.Fatal("expected cancellation")
	}
	runner := &CommandRunner{Binary: "/missing/docker-binary", Timeout: time.Second, OutputLimit: 8}
	if _, e := runner.Run(context.Background(), "version"); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("missing binary error=%v", e)
	}
	if _, e := (&CommandRunner{Binary: "/usr/bin/printf", Timeout: time.Second, OutputLimit: 8}).Run(context.Background(), "%s", "0123456789abcdef"); e == nil {
		t.Fatal("expected bounded output failure")
	}
}

func TestAnalyzePreservesCommandDeadline(t *testing.T) {
	r := &fakeRunner{errs: []error{context.DeadlineExceeded}}
	_, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command deadline lost: %v", err)
	}
}

func TestStderrClassificationIsAllowlistedAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		text string
		want error
	}{
		{"permission denied while connecting", ErrPermission},
		{"Cannot connect to the Docker daemon at tcp://localhost", ErrConnection},
		{"unknown flag: --new-option", ErrUnsupported},
		{"daemon failed with secret-token-123", nil},
	} {
		got := classifyStderr(tc.text)
		if !errors.Is(got, tc.want) {
			t.Fatalf("classify %q = %v, want %v", tc.text, got, tc.want)
		}
		if got != nil && strings.Contains(got.Error(), tc.text) {
			t.Fatalf("raw stderr leaked: %v", got)
		}
	}
}

func TestClassifiedCommandErrorsUseFixedIssues(t *testing.T) {
	for _, tc := range []struct {
		err        error
		code, text string
	}{
		{ErrUnavailable, "docker_cli_unavailable", "Docker CLI is unavailable"},
		{ErrPermission, "docker_permission_denied", "Docker access was denied"},
		{ErrConnection, "docker_daemon_unreachable", "Docker daemon is unreachable"},
		{ErrUnsupported, "docker_operation_unsupported", "does not support this operation"},
	} {
		o, err := New(&fakeRunner{errs: []error{tc.err}}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk})
		if err == nil || len(o.Issues) != 1 || o.Issues[0].Code != tc.code || !strings.Contains(o.Issues[0].Summary, tc.text) {
			t.Fatalf("outcome=%+v err=%v", o, err)
		}
		b, _ := json.Marshal(o)
		if strings.Contains(string(b), "private") {
			t.Fatalf("raw diagnostic leaked: %s", b)
		}
	}
	r := NewCommandRunner()
	r.Binary = "/definitely/missing/docker-cli"
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk})
	if !errors.Is(err, ErrUnavailable) || len(o.Issues) != 1 || !strings.Contains(o.Issues[0].Summary, "install Docker CLI") {
		t.Fatalf("missing CLI outcome=%+v err=%v", o, err)
	}
}

func TestDockerCommandHelper(t *testing.T) {
	if os.Getenv("DAFFY_DOCKER_COMMAND_HELPER") != "1" {
		return
	}
	args := os.Args
	for i := range args {
		if args[i] == "--" && i+1 < len(args) {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	switch args[0] {
	case "permission":
		fmt.Fprintln(os.Stderr, "permission denied")
	case "connection":
		fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon at tcp://private-host")
	case "unsupported":
		fmt.Fprintln(os.Stderr, "unknown flag: --private-flag")
	default:
		fmt.Fprintln(os.Stderr, "daemon failed secret-token-123")
	}
	os.Exit(1)
}

func TestCommandRunnerSubprocessDiagnosticsArePrivate(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode string
		want error
	}{{"permission", ErrPermission}, {"connection", ErrConnection}, {"unsupported", ErrUnsupported}, {"private", ErrCommandFailed}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("DAFFY_DOCKER_COMMAND_HELPER", "1")
			r := &CommandRunner{Binary: bin, OutputLimit: 1024}
			// Exercise the actual command runner through Plugin report construction.
			o, err := New(subprocessDiagnosticRunner{runner: r, mode: tc.mode}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			if strings.Contains(fmt.Sprint(err), "secret-token-123") || strings.Contains(fmt.Sprint(err), "private-host") || strings.Contains(fmt.Sprint(err), "private-flag") {
				t.Fatalf("stderr leaked: %v", err)
			}
			encoded, _ := json.Marshal(o)
			for _, private := range []string{"secret-token-123", "private-host", "private-flag"} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("report leaked %q: %s", private, encoded)
				}
			}
		})
	}
}

type subprocessDiagnosticRunner struct {
	runner *CommandRunner
	mode   string
}

func (r subprocessDiagnosticRunner) Run(ctx context.Context, _ ...string) ([]byte, error) {
	return r.runner.Run(ctx, "-test.run=^TestDockerCommandHelper$", "--", r.mode)
}

func TestDockerTimeoutIssueUsesSafeCommandIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	o, err := New(waitingRunner{}).Analyze(ctx, plugins.Request{Operation: plugins.OperationConfig})
	if !errors.Is(err, context.DeadlineExceeded) || len(o.Issues) != 1 || o.Issues[0].Code != plugins.IssueTimedOut || o.Issues[0].Command != "docker ps" {
		t.Fatalf("timeout outcome=%+v err=%v", o, err)
	}
	if strings.Contains(o.Issues[0].Summary, "container") || strings.Contains(o.Issues[0].Summary, "secret") || strings.Contains(o.Issues[0].Summary, "--format") {
		t.Fatalf("unsafe timeout details: %+v", o.Issues[0])
	}
}

func TestDockerConfigPsTimeoutIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	o, err := New(waitingRunner{}).Analyze(ctx, plugins.Request{Operation: plugins.OperationConfig})
	if !errors.Is(err, context.DeadlineExceeded) || len(o.Issues) != 1 || o.Issues[0].Command != "docker ps" || !strings.Contains(o.Issues[0].Summary, "docker ps timed out") || strings.Contains(o.Issues[0].Summary, "inspect") {
		t.Fatalf("config ps timeout outcome=%+v err=%v", o, err)
	}
}

func TestDockerConfigInspectTimeoutStopsLaterBatches(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("%064x", i+1)
	}
	calls := 0
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if args[0] == "ps" {
			return listing(ids), nil
		}
		if calls == 2 {
			return []byte("[]"), nil
		}
		return nil, context.DeadlineExceeded
	})
	o, err := New(runner).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	found := false
	for _, issue := range o.Issues {
		if issue.Command == "docker inspect" {
			found = true
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) || calls != 3 || !found {
		t.Fatalf("calls=%d issues=%+v err=%v", calls, o.Issues, err)
	}
}

func TestInspectFailureDoesNotDiscardLaterBatch(t *testing.T) {
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = fmt.Sprintf("%064x", i+1)
	}
	second := `[{"Id":"` + ids[50] + `","Name":"/last","State":{"Status":"running"},"HostConfig":{"Memory":1,"Privileged":false}}]`
	r := &fakeRunner{outputs: [][]byte{listing(ids), nil, []byte(second)}}
	r.errs = []error{nil, errors.New("sensitive daemon stderr"), nil}
	o, e := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if e == nil || len(o.Details.Docker.Config) != 1 || o.Details.Docker.Config[0].ID != ids[50] {
		t.Fatalf("later batch lost: %v %#v", e, o)
	}
	if strings.Contains(e.Error(), "sensitive") {
		t.Fatal("raw runner error leaked")
	}
}

func TestCommandRunnerTimeoutAndCancellation(t *testing.T) {
	r := &CommandRunner{Binary: "/bin/sleep", Timeout: 20 * time.Millisecond, OutputLimit: 64}
	if _, err := r.Run(context.Background(), "2"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&CommandRunner{Binary: "/bin/sleep", Timeout: time.Second}).Run(ctx, "2"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestRunnerTimeoutSelection(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got := runnerTimeout(parent, NewCommandRunner().Timeout, commandTimeout); got != 0 {
		t.Fatalf("default runner must inherit parent deadline, timeout=%v", got)
	}
	if got := runnerTimeout(parent, 0, commandTimeout); got != 0 {
		t.Fatalf("parent deadline should be inherited, timeout=%v", got)
	}
	if got := runnerTimeout(parent, 25*time.Millisecond, commandTimeout); got != 25*time.Millisecond {
		t.Fatalf("explicit timeout ignored: %v", got)
	}
	if got := runnerTimeout(context.Background(), 0, commandTimeout); got != commandTimeout {
		t.Fatalf("no-deadline default=%v, want %v", got, commandTimeout)
	}
}

func TestCommandRunnerExplicitTimeoutWithParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := &CommandRunner{Binary: "/bin/sleep", Timeout: 20 * time.Millisecond}
	if _, err := r.Run(parent, "2"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("explicit runner timeout was not applied: %v", err)
	}
}

func TestCommandRunnerInheritsParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// The child completes well within the parent budget. A hidden default command
	// timeout is distinguishable through runnerTimeout's selection test above.
	if _, err := (&CommandRunner{Binary: "/usr/bin/true"}).Run(parent); err != nil {
		t.Fatalf("inherited parent deadline: %v", err)
	}
}
