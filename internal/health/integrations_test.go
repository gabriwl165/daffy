package health

import (
	"context"
	"errors"
	"github.com/gabriwl165/daffy-duck/internal/distribution"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntegrationExecTimeout(t *testing.T) {
	ctx := context.Background()
	r := integrationExec(ctx, commandConfig{Timeout: 50 * time.Millisecond, Tool: distribution.Tool("sh"), Args: []string{"-c", "sleep 5"}})
	if r.err == nil {
		t.Fatal("expected timeout")
	}
	if !errors.Is(r.err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", r.err)
	}
}

func TestIntegrationSMARTRequiresExplicitOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "block/sda"), 0755); err != nil {
		t.Fatal(err)
	}
	old := integrationRun
	defer func() { integrationRun = old }()
	integrationRun = func(context.Context, commandConfig) integrationResult {
		t.Fatal("SMART command ran without opt-in")
		return integrationResult{}
	}
	fs := integrationDisks(context.Background(), Options{SysRoot: root})
	if len(fs) != 2 || fs[1].Severity != "unknown" {
		t.Fatalf("expected inventory and unavailable SMART: %+v", fs)
	}
}

func TestIntegrationCanceledCollectSkipsCommands(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	calls := 0
	integrationRun = func(context.Context, commandConfig) integrationResult {
		calls++
		return integrationResult{err: errors.New("unexpected command")}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = Collect(ctx, Options{})
	if calls != 0 {
		t.Fatalf("launched %d commands after cancellation", calls)
	}
}

func TestIntegrationBoundedOutput(t *testing.T) {
	b := new(boundedBuffer)
	n, err := b.Write([]byte(strings.Repeat("x", integrationOutputLimit+100)))
	if err != nil || n != integrationOutputLimit+100 || b.Len() != integrationOutputLimit {
		t.Fatalf("write n=%d len=%d err=%v", n, b.Len(), err)
	}
}

func TestIntegrationJournalPatternsAndPrivacy(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	integrationRun = func(_ context.Context, cfg commandConfig) integrationResult {
		if cfg.Tool != distribution.Journalctl {
			t.Fatalf("unexpected command %s", cfg.Tool)
		}
		return integrationResult{stdout: []byte("oom: oom_kill_disable enabled\nOut of memory: Killed process 42 (secret-user)\nNVRM: Xid GPU fault\nnouveau 0000:01:00.0: fifo: fault 01\nCPU0: Core temperature above threshold, cpu clock throttled\n")}
	}
	fs := integrationJournal(context.Background(), Options{}, journalCurrent)
	if len(fs) != 1 || fs[0].Severity != "warning" {
		t.Fatalf("unexpected findings: %+v", fs)
	}
	joined := strings.Join(fs[0].Evidence, " ")
	if strings.Contains(joined, "secret-user") || !strings.Contains(joined, "oom kills: 1") || !strings.Contains(joined, "GPU faults: 2") || !strings.Contains(joined, "thermal events: 1") {
		t.Fatalf("privacy/pattern mismatch: %s", joined)
	}
	fs = integrationJournal(context.Background(), Options{IncludeSensitive: true}, journalCurrent)
	if !strings.Contains(strings.Join(fs[0].Evidence, " "), "secret-user") {
		t.Fatal("sensitive evidence not included")
	}
}

func TestIntegrationMissingJournalUnknown(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	integrationRun = func(context.Context, commandConfig) integrationResult {
		return integrationResult{err: errors.New("missing")}
	}
	fs := integrationJournal(context.Background(), Options{}, journalPrevious)
	if fs[0].Severity != "unknown" {
		t.Fatalf("expected unknown, got %+v", fs)
	}
}

func TestIntegrationJournalEmptyAndDiagnosticUnknown(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	for _, result := range []integrationResult{{stdout: []byte("\n")}, {stdout: []byte("some lines"), stderr: []byte("permission denied")}} {
		integrationRun = func(context.Context, commandConfig) integrationResult { return result }
		if got := integrationJournal(context.Background(), Options{}, journalCurrent)[0].Severity; got != "unknown" {
			t.Fatalf("severity %q", got)
		}
	}
}

func TestIntegrationSMARTStatus(t *testing.T) {
	if sev, _, ok := integrationSMARTStatus("Read Device Identity failed"); ok || sev != "unknown" {
		t.Fatal("arbitrary failure interpreted as SMART result")
	}
	if sev, _, ok := integrationSMARTStatus("SMART overall-health self-assessment test result: FAILED!\n"); !ok || sev != "warning" {
		t.Fatal("failed health status not recognized")
	}
	if sev, _, ok := integrationSMARTStatus("SMART Health Status: OK\n"); !ok || sev != "ok" {
		t.Fatal("NVMe SMART status not recognized")
	}
}

func TestIntegrationBootInterpretation(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	integrationRun = func(context.Context, commandConfig) integrationResult {
		return integrationResult{stdout: []byte("Startup finished in 8s (firmware) + 1s (loader) + 3s (kernel) + 4s (userspace) = 16s\n")}
	}
	f := integrationBootTime(context.Background(), Options{})[0]
	joined := strings.Join(f.Evidence, " ")
	if !strings.Contains(joined, "Firmware time") || !strings.Contains(f.Recommendation, "Compare firmware") {
		t.Fatalf("missing interpretation: %+v", f)
	}
}

func TestDoctor(t *testing.T) {
	checks := Doctor(context.Background(), Options{ProcRoot: t.TempDir(), SysRoot: filepath.Join(t.TempDir(), "missing")})
	got := map[string]Check{}
	for _, c := range checks {
		got[c.Name] = c
	}
	if !got["procfs"].Available || got["sysfs"].Available {
		t.Fatalf("filesystem checks: %+v", got)
	}
	for _, tool := range []string{"journalctl", "systemctl", "systemd-analyze", "powerprofilesctl", "sensors", "lspci", "smartctl"} {
		if strings.Contains(got[tool].Detail, "/") {
			t.Fatalf("path leaked by doctor: %+v", got[tool])
		}
	}
}

func TestIntegrationGPUNouveauReview(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "bus/pci/devices/0000:01:00.0")
	if err := os.MkdirAll(dev, 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"class": "0x030000\n", "vendor": "0x10de\n", "device": "0x1234\n"} {
		if err := os.WriteFile(filepath.Join(dev, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "drivers"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "drivers/nouveau"), filepath.Join(dev, "driver")); err != nil {
		t.Fatal(err)
	}
	fs := integrationGPU(context.Background(), Options{SysRoot: root})
	if len(fs) != 1 || fs[0].Severity != "warning" || !strings.Contains(fs[0].Summary, "not evidence") {
		t.Fatalf("unexpected Nouveau finding: %+v", fs)
	}
}
