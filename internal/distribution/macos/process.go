package macos

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	lsofExecutable        = "/usr/sbin/lsof"
	psExecutable          = "/bin/ps"
	processCommandTimeout = 5 * time.Second
	processOutputLimit    = 64 * 1024
	processTCPProtocol    = "TCP"
	processListenState    = "LISTEN"
	processLocale         = "LC_ALL=C"
)

type ProcessCommandResult struct {
	Stdout    []byte
	Stderr    []byte
	Err       error
	Truncated bool
}
type ProcessCommandRunner interface {
	Run(context.Context, string, ...string) ProcessCommandResult
}
type boundedProcessWriter struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *boundedProcessWriter) Write(p []byte) (int, error) {
	n := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = w.buffer.Write(p[:remaining])
			w.truncated = true
		} else {
			_, _ = w.buffer.Write(p)
		}
	}
	if remaining <= 0 || len(p) > remaining {
		w.truncated = true
	}
	return n, nil
}

type execProcessRunner struct{}

func (execProcessRunner) Run(ctx context.Context, name string, args ...string) ProcessCommandResult {
	ctx, cancel := context.WithTimeout(ctx, processCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = processCommandTimeout
	cmd.Env = append(os.Environ(), processLocale)
	stdout, stderr := &boundedProcessWriter{limit: processOutputLimit}, &boundedProcessWriter{limit: processOutputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	return ProcessCommandResult{Stdout: stdout.buffer.Bytes(), Stderr: stderr.buffer.Bytes(), Err: err, Truncated: stdout.truncated || stderr.truncated}
}

type darwinFinder struct{ runner ProcessCommandRunner }

func NewProcessFinder() process.Finder { return darwinFinder{execProcessRunner{}} }

func (f darwinFinder) Find(ctx context.Context, t process.ProcessTarget) ([]process.ProcessOwner, error) {
	if err := process.ValidateTarget(t); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.Kind == process.TargetPID {
		return f.owners(ctx, []int{t.PID})
	}
	r := f.runner.Run(ctx, lsofExecutable, "-nP", "-t", "-i"+processTCPProtocol+":"+strconv.Itoa(t.Port), "-sTCP:"+processListenState)
	if r.Truncated || len(r.Stderr) > 0 {
		return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("lsof output truncated or diagnostic: %s", strings.TrimSpace(string(r.Stderr)))}
	}
	if r.Err != nil {
		if exit, ok := r.Err.(*exec.ExitError); ok && exit.ExitCode() == 1 && len(r.Stdout) == 0 && len(r.Stderr) == 0 {
			return nil, nil
		}
		return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("lsof: %w", r.Err)}
	}
	out := r.Stdout
	var pids []int
	for _, line := range strings.Fields(string(out)) {
		pid, e := strconv.Atoi(line)
		if e != nil || pid <= 1 {
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("invalid lsof PID %q", line)}
		}
		duplicate := false
		for _, known := range pids {
			if known == pid {
				duplicate = true
				break
			}
		}
		if !duplicate {
			pids = append(pids, pid)
		}
	}
	return f.owners(ctx, pids)
}

func (f darwinFinder) owners(ctx context.Context, pids []int) ([]process.ProcessOwner, error) {
	owners := make([]process.ProcessOwner, 0, len(pids))
	for _, pid := range pids {
		r := f.runner.Run(ctx, psExecutable, "-p", strconv.Itoa(pid), "-o", "lstart=")
		if r.Err != nil || r.Truncated || len(r.Stderr) > 0 {
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("ps identity lookup failed: %v %s", r.Err, strings.TrimSpace(string(r.Stderr)))}
		}
		identity := strings.TrimSpace(string(r.Stdout))
		if identity == "" {
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("ps identity lookup returned empty result for PID %d", pid)}
		}
		owners = append(owners, process.ProcessOwner{PID: pid, Identity: identity})
	}
	return owners, nil
}
