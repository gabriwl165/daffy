package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/gabriwl165/daffy/internal/disk"
)

type diskProgressWriter struct {
	out         io.Writer
	lastElapsed time.Duration
	err         error
}

func (w *diskProgressWriter) update(p disk.Progress) {
	if w == nil || w.out == nil || w.err != nil || p.Elapsed < time.Second {
		return
	}
	if w.lastElapsed != 0 && p.Elapsed-w.lastElapsed < time.Second {
		return
	}
	_, w.err = fmt.Fprintf(w.out, "disk scan elapsed=%s files=%d dirs=%d current=%s\n", p.Elapsed.Round(time.Second), p.Files, p.Directories, safe(p.CurrentPath))
	if w.err == nil {
		w.lastElapsed = p.Elapsed
	}
}
