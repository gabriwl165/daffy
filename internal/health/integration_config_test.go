package health

import (
	"context"
	"github.com/gabriwl165/daffy/internal/distribution"
	"strings"
	"testing"
)

func TestIntegrationFindingDefaultConfidence(t *testing.T) {
	f := integrationFinding(Finding{ID: "test", Area: AreaCore, Severity: SeverityUnknown})
	if f.Confidence != ConfidenceMedium {
		t.Fatalf("confidence = %q", f.Confidence)
	}
}

func TestJournalBootConfigurationPairing(t *testing.T) {
	for _, tc := range []struct {
		boot     journalBoot
		flag, id string
	}{
		{journalCurrent, "--boot", findingJournalCurrent},
		{journalPrevious, "--boot=-1", findingJournalPrevious},
	} {
		cfg, _ := tc.boot.config(0)
		if cfg.Tool != distribution.Journalctl {
			t.Fatalf("tool = %q", cfg.Tool)
		}
		if len(cfg.Args) < 6 || cfg.Args[4] != tc.flag || journalFindingID(tc.boot) != tc.id {
			t.Fatalf("boot config/id mismatch: %+v, %q", cfg, journalFindingID(tc.boot))
		}
	}
}

func TestJournalEvidenceOrdering(t *testing.T) {
	old := integrationRun
	defer func() { integrationRun = old }()
	integrationRun = func(_ context.Context, cfg commandConfig) integrationResult {
		return integrationResult{stdout: []byte("thermal critical temperature\nkernel panic\nI/O error\nNVRM: Xid fault\nOut of memory: Killed process 42\n")}
	}
	findings := integrationJournal(context.Background(), Options{}, journalCurrent)
	if len(findings) != 1 {
		t.Fatalf("findings: %+v", findings)
	}
	got := strings.Join(findings[0].Evidence, "|")
	want := "GPU faults: 1|I/O errors: 1|kernel panics: 1|oom kills: 1|thermal events: 1"
	if got != want {
		t.Fatalf("evidence order = %q, want %q", got, want)
	}
}
