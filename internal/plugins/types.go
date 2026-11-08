// Package plugins provides a typed, read-only analysis plugin registry and runner.
package plugins

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gabriwl165/daffy/internal/disk"
)

type PluginID string
type OperationID string
type Status string
type Access string

const (
	PluginDocker         PluginID    = "docker"
	PluginBazel          PluginID    = "bazel"
	OperationDisk        OperationID = "disk"
	OperationRuntime     OperationID = "runtime"
	OperationConfig      OperationID = "config"
	StatusComplete       Status      = "complete"
	StatusPartial        Status      = "partial"
	StatusUnavailable    Status      = "unavailable"
	StatusCancelled      Status      = "cancelled"
	SeverityInfo                     = "info"
	SeverityWarning                  = "warning"
	SchemaVersion                    = "1"
	IssueExecutionFailed             = "execution-failed"
	IssueCancelled                   = "cancelled"
	IssueTimedOut                    = "timed-out"
	IssueSkipped                     = "skipped-cancelled"
	AccessOffline        Access      = "offline"
	AccessFilesystem     Access      = "filesystem-read"
	AccessDaemonRead     Access      = "daemon-read"
)

var (
	ErrInvalid          = errors.New("invalid plugin request")
	ErrUnknownPlugin    = errors.New("unknown plugin")
	ErrUnknownOperation = errors.New("unknown operation")
)

type Options struct {
	Path     string   `json:"path,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	TopN     int      `json:"top_n,omitempty"`
}
type Request struct {
	Operation OperationID `json:"operation,omitempty"`
	Options   Options     `json:"options,omitempty"`
}
type SupportedOptions struct {
	Path        bool `json:"path,omitempty"`
	Excludes    bool `json:"excludes,omitempty"`
	TopN        bool `json:"top_n,omitempty"`
	RequirePath bool `json:"require_path,omitempty"`
}
type Operation struct {
	ID      OperationID      `json:"id"`
	Summary string           `json:"summary"`
	Default bool             `json:"default,omitempty"`
	Access  Access           `json:"access"`
	Options SupportedOptions `json:"options"`
}
type Descriptor struct {
	ID         PluginID    `json:"id"`
	Operations []Operation `json:"operations"`
}
type Plugin interface {
	Describe() Descriptor
	Analyze(context.Context, Request) (Outcome, error)
}

type Severity string
type Finding struct {
	Code           string   `json:"code"`
	Severity       Severity `json:"severity"`
	Summary        string   `json:"summary"`
	Recommendation string   `json:"recommendation,omitempty"`
}
type Issue struct {
	Code      string `json:"code"`
	Summary   string `json:"summary"`
	ElapsedMS int64  `json:"elapsed_ms,omitempty"`
	Command   string `json:"command,omitempty"`
}
type DockerDiskUsage struct {
	Category    string `json:"category"`
	TotalCount  int64  `json:"total_count"`
	Active      int64  `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}
type DockerContainerStats struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CPUPerc  string `json:"cpu_perc"`
	MemUsage string `json:"mem_usage"`
	MemPerc  string `json:"mem_perc"`
	NetIO    string `json:"net_io"`
	BlockIO  string `json:"block_io"`
	PIDs     string `json:"pids"`
}
type DockerContainerConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Health         string `json:"health,omitempty"`
	MemoryLimit    *int64 `json:"memory_limit,omitempty"`
	NanoCPUs       *int64 `json:"nano_cpus,omitempty"`
	PIDsLimit      *int64 `json:"pids_limit,omitempty"`
	Privileged     *bool  `json:"privileged,omitempty"`
	ReadonlyRootfs *bool  `json:"readonly_rootfs,omitempty"`
	LogDriver      string `json:"log_driver,omitempty"`
	RestartCount   int64  `json:"restart_count"`
	OOMKilled      *bool  `json:"oom_killed,omitempty"`
	ExitCode       *int64 `json:"exit_code,omitempty"`
}
type DockerDetails struct {
	DiskUsage []DockerDiskUsage       `json:"disk_usage,omitempty"`
	Runtime   []DockerContainerStats  `json:"runtime,omitempty"`
	Config    []DockerContainerConfig `json:"config,omitempty"`
	Context   string                  `json:"context,omitempty"`
}
type BazelDetails struct {
	Disk *disk.Report `json:"disk,omitempty"`
}
type Details struct {
	Docker *DockerDetails `json:"docker,omitempty"`
	Bazel  *BazelDetails  `json:"bazel,omitempty"`
}
type Outcome struct {
	Operation OperationID `json:"operation"`
	Status    Status      `json:"status"`
	Details   Details     `json:"details,omitempty"`
	Findings  []Finding   `json:"findings,omitempty"`
	Issues    []Issue     `json:"issues,omitempty"`
}
type Report struct {
	SchemaVersion string    `json:"schema_version"`
	Plugin        PluginID  `json:"plugin"`
	Status        Status    `json:"status"`
	Outcomes      []Outcome `json:"outcomes"`
}

type entry struct {
	plugin     Plugin
	descriptor Descriptor
}
type Registry struct{ entries []entry }

func validID(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !(unicode.IsLower(r) || unicode.IsDigit(r) || (i > 0 && (r == '-' || r == '_'))) {
			return false
		}
	}
	return true
}

func NewRegistry(plugins ...Plugin) (*Registry, error) {
	r := &Registry{entries: make([]entry, 0, len(plugins))}
	for _, p := range plugins {
		if p == nil {
			return nil, fmt.Errorf("%w: nil plugin", ErrInvalid)
		}
		d := p.Describe()
		if !validID(string(d.ID)) || len(d.Operations) == 0 {
			return nil, fmt.Errorf("%w: invalid or duplicate plugin descriptor", ErrInvalid)
		}
		d.Operations = append([]Operation(nil), d.Operations...)
		for _, old := range r.entries {
			if old.descriptor.ID == d.ID {
				return nil, fmt.Errorf("%w: duplicate plugin", ErrInvalid)
			}
		}
		for i, op := range d.Operations {
			if !validID(string(op.ID)) || op.Summary == "" || (op.Access != AccessOffline && op.Access != AccessFilesystem && op.Access != AccessDaemonRead) || op.Options.RequirePath && !op.Options.Path {
				return nil, fmt.Errorf("%w: invalid operation descriptor", ErrInvalid)
			}
			for j := 0; j < i; j++ {
				if d.Operations[j].ID == op.ID {
					return nil, fmt.Errorf("%w: duplicate operation", ErrInvalid)
				}
			}
		}
		r.entries = append(r.entries, entry{p, d})
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].descriptor.ID < r.entries[j].descriptor.ID })
	return r, nil
}
func (r *Registry) Lookup(id PluginID) (Descriptor, bool) {
	if r == nil {
		return Descriptor{}, false
	}
	for _, e := range r.entries {
		if e.descriptor.ID == id {
			d := e.descriptor
			d.Operations = append([]Operation(nil), d.Operations...)
			return d, true
		}
	}
	return Descriptor{}, false
}
func (r *Registry) List() []Descriptor {
	if r == nil {
		return nil
	}
	out := make([]Descriptor, 0, len(r.entries))
	for _, e := range r.entries {
		d := e.descriptor
		d.Operations = append([]Operation(nil), d.Operations...)
		out = append(out, d)
	}
	return out
}

// Run executes the selected operation, or defaults belonging only to pluginID.
func (r *Registry) Run(ctx context.Context, pluginID PluginID, req Request) (Report, error) {
	return r.RunWithTimeout(ctx, pluginID, req, 0)
}

// RunWithTimeout applies an independent timeout to each operation when timeout is positive.
// Parent cancellation and deadlines remain authoritative. A zero timeout preserves Run behavior.
func (r *Registry) RunWithTimeout(ctx context.Context, pluginID PluginID, req Request, timeout time.Duration) (Report, error) {
	var report Report
	report.SchemaVersion = SchemaVersion
	report.Plugin = pluginID
	if r == nil {
		return report, fmt.Errorf("%w: nil registry", ErrInvalid)
	}
	var e *entry
	for i := range r.entries {
		if r.entries[i].descriptor.ID == pluginID {
			e = &r.entries[i]
			break
		}
	}
	if e == nil {
		return report, fmt.Errorf("%w: %q", ErrUnknownPlugin, pluginID)
	}
	selected := []Operation{}
	if req.Operation != "" {
		for _, op := range e.descriptor.Operations {
			if op.ID == req.Operation {
				selected = append(selected, op)
			}
		}
		if len(selected) == 0 {
			return report, fmt.Errorf("%w: %q", ErrUnknownOperation, req.Operation)
		}
	} else {
		for _, op := range e.descriptor.Operations {
			if op.Default {
				selected = append(selected, op)
			}
		}
	}
	if len(selected) == 0 {
		return report, fmt.Errorf("%w: no default operations", ErrInvalid)
	}
	var failures []error
	var timedOut int
	addFailure := func(err error) {
		if err != nil {
			failures = append(failures, err)
		}
	}
	for _, op := range selected {
		opts := req.Options
		if opts.Path != "" && !op.Options.Path || len(opts.Excludes) > 0 && !op.Options.Excludes || opts.TopN != 0 && !op.Options.TopN || opts.TopN < 0 || (op.Options.RequirePath && opts.Path == "") {
			return report, fmt.Errorf("%w: unsupported or invalid options for %q", ErrInvalid, op.ID)
		}
	}
	for _, op := range selected {
		if err := ctx.Err(); err != nil {
			summary := "skipped because parent context was cancelled"
			if errors.Is(err, context.DeadlineExceeded) {
				summary = "skipped because parent deadline expired"
			}
			report.Outcomes = append(report.Outcomes, Outcome{Operation: op.ID, Status: StatusCancelled, Issues: []Issue{{Code: IssueSkipped, Summary: summary}}})
			addFailure(err)
			continue
		}
		opCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			opCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		started := time.Now()
		outcome, err := e.plugin.Analyze(opCtx, Request{Operation: op.ID, Options: req.Options})
		elapsed := time.Since(started)
		opErr := opCtx.Err()
		cancel()
		outcome.Operation = op.ID
		if opErr != nil && !errors.Is(err, opErr) {
			err = errors.Join(err, opErr)
		}
		operationTimedOut := errors.Is(opErr, context.DeadlineExceeded) && ctx.Err() == nil
		runnerTimedOut := errors.Is(err, context.DeadlineExceeded) && opErr == nil && ctx.Err() == nil
		if err != nil {
			if operationTimedOut || runnerTimedOut {
				outcome.Status = StatusCancelled
				timedOut++
				for i := range outcome.Issues {
					if outcome.Issues[i].Code == IssueTimedOut && operationTimedOut {
						outcome.Issues[i].Summary += "; increase --timeout above its current value"
					}
				}
				if !hasIssue(outcome.Issues, IssueTimedOut) {
					summary := fmt.Sprintf("operation timed out after %s", elapsed.Round(time.Millisecond))
					if operationTimedOut {
						summary += "; increase --timeout above its current value"
					}
					outcome.Issues = append(outcome.Issues, Issue{Code: IssueTimedOut, Summary: summary, ElapsedMS: elapsed.Milliseconds()})
				}
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				outcome.Status = StatusCancelled
				if ctx.Err() != nil {
					filtered := outcome.Issues[:0]
					for _, issue := range outcome.Issues {
						if issue.Code != IssueTimedOut {
							filtered = append(filtered, issue)
						}
					}
					outcome.Issues = filtered
					summary := "operation stopped because parent context was cancelled"
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						summary = "operation stopped because parent deadline expired"
					}
					outcome.Issues = append(outcome.Issues, Issue{Code: IssueCancelled, Summary: summary})
				}
				if ctx.Err() == nil && errors.Is(err, context.Canceled) && !hasIssue(outcome.Issues, IssueCancelled) {
					outcome.Issues = append(outcome.Issues, Issue{Code: IssueCancelled, Summary: "operation was cancelled"})
				}
			} else {
				if outcome.Status != StatusUnavailable {
					outcome.Status = StatusPartial
				}
				outcome.Issues = append(outcome.Issues, Issue{Code: IssueExecutionFailed, Summary: err.Error()})
			}
			addFailure(err)
		} else if outcome.Status == "" {
			outcome.Status = StatusComplete
		}
		report.Outcomes = append(report.Outcomes, outcome)
	}
	if len(report.Outcomes) > 0 {
		report.Status = StatusComplete
		for _, o := range report.Outcomes {
			if o.Status != StatusComplete {
				report.Status = StatusPartial
				break
			}
		}
	}
	if timedOut > 0 {
		return report, &runErrors{timedOut: timedOut, total: len(selected), errs: failures}
	}
	if len(failures) == 0 {
		return report, nil
	}
	return report, &runErrors{errs: failures}
}

type runErrors struct {
	timedOut, total int
	errs            []error
}

func (e *runErrors) Unwrap() []error { return e.errs }
func (e *runErrors) Error() string {
	parts := make([]string, 0, len(e.errs)+1)
	if e.timedOut > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d plugin operations timed out", e.timedOut, e.total))
	}
	seenCanceled, seenDeadline := false, false
	for _, err := range e.errs {
		if err == context.DeadlineExceeded && e.timedOut > 0 {
			continue
		}
		if err == context.Canceled {
			if seenCanceled {
				continue
			}
			seenCanceled = true
		}
		if err == context.DeadlineExceeded {
			if seenDeadline {
				continue
			}
			seenDeadline = true
		}
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, ": ")
}

func hasIssue(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
