package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
	"github.com/gabriwl165/daffy/internal/plugins"
)

func TestPrintPluginReportRecommendationEscaping(t *testing.T) {
	var out bytes.Buffer
	r := plugins.Report{Plugin: plugins.PluginDocker, Status: plugins.StatusComplete, Outcomes: []plugins.Outcome{{Operation: plugins.OperationConfig, Status: plugins.StatusComplete, Findings: []plugins.Finding{{Severity: plugins.SeverityWarning, Summary: "failure", Recommendation: "safe\x1b[2J\nrecommendation"}}}}}
	if err := printPluginReport(&out, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Recommendation: safe[2Jrecommendation") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("recommendation not escaped: %q", out.String())
	}
}

type recommendationErrorWriter struct{}

func (recommendationErrorWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Recommendation:")) {
		return 0, errors.New("writer failed")
	}
	return len(p), nil
}

func TestPrintPluginReportRecommendationWriteError(t *testing.T) {
	r := plugins.Report{Plugin: plugins.PluginDocker, Status: plugins.StatusComplete, Outcomes: []plugins.Outcome{{Operation: plugins.OperationConfig, Status: plugins.StatusComplete, Findings: []plugins.Finding{{Recommendation: "action"}}}}}
	if err := printPluginReport(recommendationErrorWriter{}, r); err == nil || err.Error() != "writer failed" {
		t.Fatalf("recommendation write error = %v", err)
	}
}

func TestDefaultDepsHasBuiltinPluginRegistry(t *testing.T) {
	r := DefaultDeps().Plugins
	if r == nil {
		t.Fatal("default plugin registry is nil")
	}
	if _, ok := r.Lookup(plugins.PluginDocker); !ok {
		t.Fatal("docker plugin missing")
	}
	if _, ok := r.Lookup(plugins.PluginBazel); !ok {
		t.Fatal("bazel plugin missing")
	}
}

type fakePlugin struct {
	descriptor plugins.Descriptor
	calls      []plugins.Request
	outcome    plugins.Outcome
	err        error
	analyze    func(context.Context, plugins.Request) (plugins.Outcome, error)
}

func (f *fakePlugin) Describe() plugins.Descriptor { return f.descriptor }
func (f *fakePlugin) Analyze(ctx context.Context, r plugins.Request) (plugins.Outcome, error) {
	f.calls = append(f.calls, r)
	if f.analyze != nil {
		return f.analyze(ctx, r)
	}
	return f.outcome, f.err
}
func fakeRegistry(t *testing.T, p *fakePlugin) *plugins.Registry {
	t.Helper()
	r, e := plugins.NewRegistry(p)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func op(id plugins.OperationID, def bool, opts plugins.SupportedOptions) plugins.Operation {
	return plugins.Operation{ID: id, Summary: "test operation", Default: def, Access: plugins.AccessOffline, Options: opts}
}
func runFake(t *testing.T, p *fakePlugin, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: append([]string{"plugins", "run", string(p.descriptor.ID)}, args...), Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
	return code, out.String(), errOut.String()
}

func TestDockerContextContainerFlagsReachPluginsRunAndAlias(t *testing.T) {
	for _, args := range [][]string{{"plugins", "run", "docker", "runtime", "--context", "local-dev", "--container", "api", "--container", "worker"}, {"docker", "runtime", "--context", "local-dev", "--container", "api", "--container", "worker"}} {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{op(plugins.OperationRuntime, true, plugins.SupportedOptions{Context: true, Containers: true})}}}
		var out, errOut bytes.Buffer
		code := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
		if code != 0 || len(p.calls) != 1 || p.calls[0].Options.Context != "local-dev" || strings.Join(p.calls[0].Options.Containers, ",") != "api,worker" {
			t.Fatalf("args=%v code=%d request=%#v err=%s", args, code, p.calls, errOut.String())
		}
	}
}

func TestDockerContainerFlagRejectedForDiskAndBundle(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Context: true}), op(plugins.OperationRuntime, true, plugins.SupportedOptions{Context: true, Containers: true})}}}
	for _, args := range [][]string{{"plugins", "run", "docker", "disk", "--container", "api"}, {"plugins", "run", "docker", "--container", "api"}} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), RunConfig{Args: args, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
		if code == 0 || len(p.calls) != 0 {
			t.Fatalf("accepted %v code=%d calls=%v", args, code, p.calls)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func TestPluginCancellationDeadlineAndWriters(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		args []string
		want int
	}{
		{name: "canceled", ctx: cancelledContext(), want: 130},
		{name: "deadline", ctx: context.Background(), args: []string{"--timeout=1ms"}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("run", true, plugins.SupportedOptions{})}}, analyze: func(ctx context.Context, _ plugins.Request) (plugins.Outcome, error) {
				<-ctx.Done()
				return plugins.Outcome{Status: plugins.StatusCancelled, Issues: []plugins.Issue{{Summary: "stopped"}}}, ctx.Err()
			}}
			var out, errOut bytes.Buffer
			args := append([]string{"plugins", "run", "third"}, tt.args...)
			code := Run(tt.ctx, RunConfig{Args: args, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
			if code != tt.want || !strings.Contains(out.String(), "third ·") {
				t.Fatalf("code=%d report=%q", code, out.String())
			}
		})
	}
	t.Run("report writer", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("run", true, plugins.SupportedOptions{})}}}
		code := Run(context.Background(), RunConfig{Args: []string{"plugins", "run", "third"}, Out: brokenWriter{}, ErrOut: io.Discard, Deps: Deps{Plugins: fakeRegistry(t, p)}})
		if code != 1 {
			t.Fatalf("code=%d", code)
		}
	})
	t.Run("help writer", func(t *testing.T) {
		code := Run(context.Background(), RunConfig{Args: []string{"plugins", "run", "--help"}, Out: brokenWriter{}, ErrOut: io.Discard})
		if code != 1 {
			t.Fatalf("code=%d", code)
		}
	})
}

func TestPluginTimeoutIsPerBundleOperation(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("first", true, plugins.SupportedOptions{}), op("second", true, plugins.SupportedOptions{})}}, analyze: func(ctx context.Context, req plugins.Request) (plugins.Outcome, error) {
		if req.Operation == "first" {
			<-ctx.Done()
			return plugins.Outcome{}, ctx.Err()
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 10*time.Millisecond {
			return plugins.Outcome{}, errors.New("second operation did not receive a fresh timeout")
		}
		return plugins.Outcome{Status: plugins.StatusComplete}, nil
	}}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: []string{"plugins", "run", "third", "--timeout=20ms"}, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
	if code != 1 || len(p.calls) != 2 || !strings.Contains(out.String(), "second") {
		t.Fatalf("code=%d calls=%v output=%q stderr=%q", code, p.calls, out.String(), errOut.String())
	}
}

func TestPluginTimeoutDiagnosticsTextJSONAndAlias(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"explicit text", []string{"plugins", "run", "docker", "disk", "--timeout=2ms"}},
		{"default alias JSON", []string{"docker", "--timeout=2ms", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{}), op(plugins.OperationRuntime, true, plugins.SupportedOptions{}), op(plugins.OperationConfig, true, plugins.SupportedOptions{})}
			p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: ops}, analyze: func(ctx context.Context, req plugins.Request) (plugins.Outcome, error) {
				if req.Operation == plugins.OperationDisk {
					<-ctx.Done()
					return plugins.Outcome{}, ctx.Err()
				}
				return plugins.Outcome{Status: plugins.StatusComplete}, nil
			}}
			var out, stderr bytes.Buffer
			code := Run(context.Background(), RunConfig{Args: tc.args, Out: &out, ErrOut: &stderr, Deps: Deps{Plugins: fakeRegistry(t, p)}})
			wantCalls, summary := 1, "1 of 1 plugin operations timed out"
			if strings.HasPrefix(tc.name, "default") {
				wantCalls, summary = 3, "1 of 3 plugin operations timed out"
			}
			if code != 1 || len(p.calls) != wantCalls || !strings.Contains(out.String(), "timed out") || strings.Contains(out.String(), "skipped") || !strings.Contains(stderr.String(), summary) {
				t.Fatalf("code=%d calls=%v out=%q stderr=%q", code, p.calls, out.String(), stderr.String())
			}
			if strings.Contains(strings.Join(tc.args, " "), "--json") && !strings.Contains(out.String(), `"code":"timed-out"`) {
				t.Fatalf("JSON missing typed timeout: %s", out.String())
			}
		})
	}
}

func TestParentDeadlineWithSkippedOperationExitsFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("first", true, plugins.SupportedOptions{}), op("second", true, plugins.SupportedOptions{})}}, analyze: func(ctx context.Context, _ plugins.Request) (plugins.Outcome, error) {
		<-ctx.Done()
		return plugins.Outcome{}, ctx.Err()
	}}
	var out, stderr bytes.Buffer
	code := Run(ctx, RunConfig{Args: []string{"plugins", "run", "third"}, Out: &out, ErrOut: &stderr, Deps: Deps{Plugins: fakeRegistry(t, p)}})
	if code != 1 || !strings.Contains(out.String(), "skipped because parent deadline expired") {
		t.Fatalf("code=%d calls=%v out=%q stderr=%q", code, p.calls, out.String(), stderr.String())
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestPluginTypedTextDetailsAndSanitization(t *testing.T) {
	one, two := int64(12), int64(34)
	yes, no := true, false
	diskReport := &disk.Report{Root: "/bad\x1b[31mroot", Totals: disk.Totals{ApparentBytes: 20, AllocatedBytes: 10}, Coverage: disk.Coverage{Mounts: 2, ScannedMounts: 1, Directories: 3, Files: 4, Complete: false}}
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{})}}, outcome: plugins.Outcome{Details: plugins.Details{Docker: &plugins.DockerDetails{Context: "ctx\x1b[2J", DiskUsage: []plugins.DockerDiskUsage{{Category: "images\x1b", TotalCount: 5, Active: 2, Size: "4GB", Reclaimable: "3GB"}}, Runtime: []plugins.DockerContainerStats{{ID: "id\x1b", Name: "web\n", CPUPerc: "1%", MemUsage: "2MB", MemPerc: "3%", NetIO: "4/5", BlockIO: "6/7", PIDs: "8"}}, Config: []plugins.DockerContainerConfig{{ID: "cid", Name: "name", Status: "running", Health: "healthy", MemoryLimit: &one, NanoCPUs: &two, PIDsLimit: &one, Privileged: &yes, ReadonlyRootfs: &no, LogDriver: "json", RestartCount: 2}}}}}}
	code, text, _ := runFake(t, p)
	for _, s := range []string{"4GB", "CPU 1%", "memory=12", "Context: ctx", "readonly_rootfs=false"} {
		if code != 0 || !strings.Contains(text, s) {
			t.Fatalf("missing %q in %q", s, text)
		}
	}
	if strings.Contains(text, "\x1b") || strings.Contains(text, "web\n") {
		t.Fatalf("unsafe text %q", text)
	}
	b := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginBazel, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{})}}, outcome: plugins.Outcome{Details: plugins.Details{Bazel: &plugins.BazelDetails{Disk: diskReport}}}}
	code, text, _ = runFake(t, b)
	for _, s := range []string{"Path: /bad[31mroot", "apparent 20 bytes", "1/2 mounts scanned", "3 directories, 4 files"} {
		if code != 0 || !strings.Contains(text, s) {
			t.Fatalf("missing %q in %q", s, text)
		}
	}
	if strings.Contains(text, "\x1b") {
		t.Fatalf("unsafe text %q", text)
	}
}

func TestPluginRunSelectionAndValidation(t *testing.T) {
	t.Run("default bundle", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{}), op(plugins.OperationRuntime, true, plugins.SupportedOptions{}), op(plugins.OperationConfig, true, plugins.SupportedOptions{})}}}
		_, _, _ = runFake(t, p)
		if len(p.calls) != 3 || p.calls[0].Operation != plugins.OperationDisk || p.calls[2].Operation != plugins.OperationConfig {
			t.Fatalf("calls=%+v", p.calls)
		}
	})
	t.Run("explicit operation", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("only", true, plugins.SupportedOptions{})}}}
		code, _, _ := runFake(t, p, "only")
		if code != 0 || len(p.calls) != 1 {
			t.Fatalf("code %d calls %d", code, len(p.calls))
		}
	})
	t.Run("unknown operation", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("known", true, plugins.SupportedOptions{})}}}
		code, _, _ := runFake(t, p, "missing")
		if code != 2 || len(p.calls) != 0 {
			t.Fatalf("code %d calls %d", code, len(p.calls))
		}
	})
	t.Run("unknown plugin", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("known", true, plugins.SupportedOptions{})}}}
		var out, errOut bytes.Buffer
		code := Run(context.Background(), RunConfig{Args: []string{"plugins", "run", "missing"}, Out: &out, ErrOut: &errOut, Deps: Deps{Plugins: fakeRegistry(t, p)}})
		if code != 2 || len(p.calls) != 0 {
			t.Fatalf("code=%d calls=%d", code, len(p.calls))
		}
	})
	t.Run("required path", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("disk", true, plugins.SupportedOptions{Path: true, RequirePath: true})}}}
		code, _, _ := runFake(t, p)
		if code != 2 || len(p.calls) != 0 {
			t.Fatalf("code %d calls %d", code, len(p.calls))
		}
	})
	t.Run("unsupported top", func(t *testing.T) {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{op("disk", true, plugins.SupportedOptions{})}}}
		code, _, _ := runFake(t, p, "--top=0")
		if code != 2 || len(p.calls) != 0 {
			t.Fatalf("code %d calls %d", code, len(p.calls))
		}
	})
}

func TestPluginInvalidOptionsAndFindingsExit(t *testing.T) {
	for _, args := range [][]string{{"--timeout=0"}, {"--top=10001"}} {
		p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginBazel, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Path: true, TopN: true})}}}
		all := append([]string{"--path=/tmp"}, args...)
		code, _, _ := runFake(t, p, all...)
		if code != 2 || len(p.calls) != 0 {
			t.Fatalf("args %v code=%d calls=%d", all, code, len(p.calls))
		}
	}
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("run", true, plugins.SupportedOptions{})}}, outcome: plugins.Outcome{Findings: []plugins.Finding{{Summary: "finding"}}}}
	code, text, _ := runFake(t, p)
	if code != 0 || !strings.Contains(text, "finding") {
		t.Fatalf("code=%d text=%q", code, text)
	}
}

func TestBazelDefaultTop(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginBazel, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Path: true, TopN: true})}}}
	code, _, _ := runFake(t, p, "--path=/tmp")
	if code != 0 || p.calls[0].Options.TopN != 20 {
		t.Fatalf("code=%d calls=%+v", code, p.calls)
	}
}

func TestPluginFailureDetailsAndSanitization(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("analyze", true, plugins.SupportedOptions{})}}, outcome: plugins.Outcome{Status: plugins.StatusPartial, Findings: []plugins.Finding{{Summary: "bad\ntext"}}}, err: errors.New("partial")}
	code, text, _ := runFake(t, p)
	if code != 1 || strings.Contains(text, "bad\ntext") {
		t.Fatalf("code=%d text=%q", code, text)
	}
	p.err = plugins.ErrInvalid
	code, text, _ = runFake(t, p)
	if code != 2 || text != "" {
		t.Fatalf("code=%d text=%q", code, text)
	}
}

func TestBazelTopZeroIsPassed(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: plugins.PluginBazel, Operations: []plugins.Operation{op(plugins.OperationDisk, true, plugins.SupportedOptions{Path: true, TopN: true})}}}
	code, _, _ := runFake(t, p, "--path=/tmp", "--top=0")
	if code != 0 || len(p.calls) != 1 || p.calls[0].Options.TopN != 0 {
		t.Fatalf("code=%d calls=%+v", code, p.calls)
	}
}

func TestPartialWithoutErrorFailsAfterPrinting(t *testing.T) {
	p := &fakePlugin{descriptor: plugins.Descriptor{ID: "third", Operations: []plugins.Operation{op("analyze", true, plugins.SupportedOptions{})}}, outcome: plugins.Outcome{Status: plugins.StatusPartial, Findings: []plugins.Finding{{Summary: "partial details"}}}}
	code, text, _ := runFake(t, p)
	if code != 1 || !strings.Contains(text, "partial details") {
		t.Fatalf("code=%d text=%q", code, text)
	}
}

func TestPluginListJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(context.Background(), RunConfig{Args: []string{"plugins", "list", "--json"}, Out: &out, ErrOut: &errOut, Deps: DefaultDeps()})
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"id":"docker"`)) || !bytes.Contains(out.Bytes(), []byte(`"id":"bazel"`)) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}
