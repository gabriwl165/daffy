package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
)

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDiskProgressWriterRateLimitAndSanitize(t *testing.T) {
	var b strings.Builder
	w := diskProgressWriter{out: &b}
	w.update(disk.Progress{})
	w.update(disk.Progress{Elapsed: time.Millisecond, CurrentPath: "/initial"})
	w.update(disk.Progress{Elapsed: time.Second, CurrentPath: "/tmp/a\nb\rc\x1b[31m\t"})
	w.update(disk.Progress{Elapsed: 1500 * time.Millisecond, CurrentPath: "/skipped"})
	w.update(disk.Progress{Elapsed: 2 * time.Second, Files: 3, Directories: 4, CurrentPath: "/ok"})
	if strings.Count(b.String(), "\n") != 2 || strings.ContainsAny(b.String(), "\r\t\x1b") || strings.Contains(b.String(), "initial") || !strings.Contains(b.String(), "files=3 dirs=4") {
		t.Fatalf("output=%q", b.String())
	}
}

func TestDiskProgressWriterError(t *testing.T) {
	want := errors.New("write failed")
	w := diskProgressWriter{out: failedWriter{want}}
	w.update(disk.Progress{Elapsed: time.Second})
	if !errors.Is(w.err, want) {
		t.Fatalf("err=%v", w.err)
	}
}
