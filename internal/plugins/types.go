// Package plugins provides a typed, read-only analysis plugin registry and runner.
package plugins

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	Code    string `json:"code"`
	Summary string `json:"summary"`
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
	for _, op := range selected {
		opts := req.Options
		if opts.Path != "" && !op.Options.Path || len(opts.Excludes) > 0 && !op.Options.Excludes || opts.TopN != 0 && !op.Options.TopN || opts.TopN < 0 || (op.Options.RequirePath && opts.Path == "") {
			return report, fmt.Errorf("%w: unsupported or invalid options for %q", ErrInvalid, op.ID)
		}
	}
	for _, op := range selected {
		if err := ctx.Err(); err != nil {
			report.Outcomes = append(report.Outcomes, Outcome{Operation: op.ID, Status: StatusCancelled, Issues: []Issue{{Code: IssueCancelled, Summary: err.Error()}}})
			failures = append(failures, err)
			continue
		}
		opCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			opCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		outcome, err := e.plugin.Analyze(opCtx, Request{Operation: op.ID, Options: req.Options})
		opErr := opCtx.Err()
		cancel()
		outcome.Operation = op.ID
		if opErr != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(err, opErr)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				outcome.Status = StatusCancelled
				outcome.Issues = append(outcome.Issues, Issue{Code: IssueCancelled, Summary: err.Error()})
			} else {
				if outcome.Status != StatusUnavailable {
					outcome.Status = StatusPartial
				}
				outcome.Issues = append(outcome.Issues, Issue{Code: IssueExecutionFailed, Summary: err.Error()})
			}
			failures = append(failures, err)
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
	return report, errors.Join(failures...)
}
