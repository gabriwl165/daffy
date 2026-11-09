// Package docker implements read-only Docker daemon analysis.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const (
	outputLimit             = 4 << 20
	inspectBatch            = 50
	commandTimeout          = 30 * time.Second
	waitDelay               = 2 * time.Second
	maxContainers           = 10000
	issueDiskInvalid        = "docker_disk_invalid"
	issueDiskUnavailable    = "docker_disk_unavailable"
	issueRuntimeInvalid     = "docker_runtime_invalid"
	issueRuntimeUnavailable = "docker_runtime_unavailable"
	issueListUnavailable    = "docker_list_unavailable"
	issueContainerLimit     = "docker_container_limit"
	issueIDInvalid          = "docker_id_invalid"
	issueInspectUnavailable = "docker_inspect_unavailable"
	issueInspectInvalid     = "docker_inspect_invalid"
	issueInspectIncomplete  = "docker_inspect_incomplete"
	issueHostConfigUnknown  = "docker_host_config_unknown"
	issueInspectMissing     = "docker_inspect_missing"
	findingPrivileged       = "docker_privileged"
	findingUnlimitedMemory  = "docker_memory_unlimited"
	findingUnhealthy        = "docker_container_unhealthy"
	findingContainerFailure = "docker_container_failure"
	stateUnhealthy          = "unhealthy"
	stateRestarting         = "restarting"
)

var (
	ErrCommandFailed   = errors.New("Docker command failed")
	ErrOutputLimit     = errors.New("Docker command output exceeded limit")
	ErrUnavailable     = errors.New("Docker executable unavailable")
	ErrInvalidResponse = errors.New("invalid Docker response")
)

// Runner executes a Docker CLI invocation without a shell.
type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

// CommandRunner is the production Docker CLI runner.
type CommandRunner struct {
	Binary      string
	Timeout     time.Duration
	OutputLimit int
}

func NewCommandRunner() *CommandRunner {
	return &CommandRunner{Binary: "docker", OutputLimit: outputLimit}
}

func runnerTimeout(ctx context.Context, configured, fallback time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	if _, ok := ctx.Deadline(); ok {
		return 0
	}
	return fallback
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.exceeded {
		return len(p), nil
	}
	if b.buffer.Len()+len(p) > b.limit {
		n := b.limit - b.buffer.Len()
		if n > 0 {
			_, _ = b.buffer.Write(p[:n])
		}
		b.exceeded = true
		return len(p), nil
	}
	return b.buffer.Write(p)
}
func (r *CommandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	if r == nil {
		return nil, ErrUnavailable
	}
	bin := r.Binary
	if bin == "" {
		bin = "docker"
	}
	limit := r.OutputLimit
	if limit <= 0 {
		limit = outputLimit
	}
	timeout := runnerTimeout(ctx, r.Timeout, commandTimeout)
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = waitDelay
	out := &cappedBuffer{limit: limit}
	errOut := &cappedBuffer{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = errOut
	err := cmd.Run()
	if out.exceeded || errOut.exceeded {
		return nil, ErrOutputLimit
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var execErr *exec.Error
		if errors.Is(err, os.ErrNotExist) || errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, ErrCommandFailed
	}
	return out.buffer.Bytes(), nil
}

type Plugin struct{ runner Runner }

type commandTimeoutError struct {
	command string
	err     error
}

func (e *commandTimeoutError) Error() string { return e.command + " timed out" }
func (e *commandTimeoutError) Unwrap() error { return e.err }

func New(r Runner) *Plugin {
	if r == nil {
		r = NewCommandRunner()
	}
	return &Plugin{runner: r}
}
func (p *Plugin) Describe() plugins.Descriptor {
	return plugins.Descriptor{ID: plugins.PluginDocker, Operations: []plugins.Operation{
		{ID: plugins.OperationDisk, Summary: "Docker disk usage", Default: true, Access: plugins.AccessDaemonRead},
		{ID: plugins.OperationRuntime, Summary: "Docker container runtime", Default: true, Access: plugins.AccessDaemonRead},
		{ID: plugins.OperationConfig, Summary: "Docker container configuration", Default: true, Access: plugins.AccessDaemonRead},
	}}
}

func (p *Plugin) Analyze(ctx context.Context, req plugins.Request) (plugins.Outcome, error) {
	started := time.Now()
	o := plugins.Outcome{Operation: req.Operation, Status: plugins.StatusComplete}
	if p == nil || p.runner == nil {
		return plugins.Outcome{Operation: req.Operation, Status: plugins.StatusUnavailable}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return plugins.Outcome{Operation: req.Operation, Status: plugins.StatusCancelled}, err
	}
	if req.Options.Path != "" || len(req.Options.Excludes) != 0 || req.Options.TopN != 0 {
		return plugins.Outcome{}, plugins.ErrInvalid
	}
	if req.Operation != plugins.OperationDisk && req.Operation != plugins.OperationRuntime && req.Operation != plugins.OperationConfig {
		return plugins.Outcome{}, plugins.ErrUnknownOperation
	}
	d := &plugins.DockerDetails{Context: "CLI-selected Docker daemon; snapshot is non-atomic"}
	o.Details.Docker = d
	var err error
	switch req.Operation {
	case plugins.OperationDisk:
		err = p.disk(ctx, d, &o)
	case plugins.OperationRuntime:
		err = p.runtime(ctx, d, &o)
	case plugins.OperationConfig:
		err = p.config(ctx, d, &o)
	}
	if err != nil {
		var timeoutErr *commandTimeoutError
		if errors.As(err, &timeoutErr) {
			elapsed := time.Since(started)
			o.Issues = append(o.Issues, plugins.Issue{Code: plugins.IssueTimedOut, Summary: fmt.Sprintf("%s timed out after %s", timeoutErr.command, elapsed.Round(time.Millisecond)), ElapsedMS: elapsed.Milliseconds(), Command: timeoutErr.command})
		}
		if ctx.Err() != nil {
			o.Status = plugins.StatusCancelled
		} else if o.Status != plugins.StatusUnavailable {
			o.Status = plugins.StatusPartial
		}
		return o, err
	}
	return o, nil
}

func (p *Plugin) call(ctx context.Context, command string, args ...string) ([]byte, error) {
	b, e := p.runner.Run(ctx, args...)
	if e != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, &commandTimeoutError{command: command, err: ctx.Err()}
			}
			return nil, ctx.Err()
		}
		if errors.Is(e, context.DeadlineExceeded) {
			return nil, &commandTimeoutError{command: command, err: e}
		}
		if errors.Is(e, context.Canceled) {
			return nil, e
		}
		if errors.Is(e, ErrCommandFailed) || errors.Is(e, ErrOutputLimit) || errors.Is(e, ErrUnavailable) {
			return nil, e
		}
		return nil, ErrCommandFailed
	}
	return b, nil
}
func fail(o *plugins.Outcome, code string) {
	o.Issues = append(o.Issues, plugins.Issue{Code: code, Summary: "Docker data could not be read or validated"})
	o.Status = plugins.StatusPartial
}
func failCommand(o *plugins.Outcome, code string, err error) {
	var timeoutErr *commandTimeoutError
	if !errors.As(err, &timeoutErr) {
		fail(o, code)
	}
}
func (p *Plugin) disk(ctx context.Context, d *plugins.DockerDetails, o *plugins.Outcome) error {
	b, e := p.call(ctx, "docker system df", "system", "df", "--format", "{{json .}}")
	if e != nil {
		failCommand(o, issueDiskUnavailable, e)
		return e
	}
	rows, e := decodeLines[diskRow](b)
	if e != nil {
		fail(o, issueDiskInvalid)
		return e
	}
	if len(rows) == 0 {
		fail(o, issueDiskInvalid)
		return errors.New("empty Docker disk response")
	}
	d.DiskUsage = make([]plugins.DockerDiskUsage, 0, len(rows))
	for i := range rows {
		if rows[i].Category == "" || rows[i].TotalCount == nil || rows[i].Active == nil || rows[i].Size == "" || rows[i].Reclaimable == "" {
			fail(o, issueDiskInvalid)
			return errors.New("invalid Docker disk response")
		}
		total, e1 := strconv.ParseInt(*rows[i].TotalCount, 10, 64)
		active, e2 := strconv.ParseInt(*rows[i].Active, 10, 64)
		if e1 != nil || e2 != nil || total < 0 || active < 0 || active > total {
			fail(o, issueDiskInvalid)
			return errors.New("invalid Docker disk counts")
		}
		d.DiskUsage = append(d.DiskUsage, plugins.DockerDiskUsage{Category: rows[i].Category, TotalCount: total, Active: active, Size: rows[i].Size, Reclaimable: rows[i].Reclaimable})
	}
	return nil
}

type diskRow struct {
	Category    string  `json:"Type"`
	TotalCount  *string `json:"TotalCount"`
	Active      *string `json:"Active"`
	Size        string  `json:"Size"`
	Reclaimable string  `json:"Reclaimable"`
}

func (p *Plugin) runtime(ctx context.Context, d *plugins.DockerDetails, o *plugins.Outcome) error {
	b, e := p.call(ctx, "docker stats", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if e != nil {
		failCommand(o, issueRuntimeUnavailable, e)
		return e
	}
	rows, e := decodeLines[statsRow](b)
	if e != nil {
		fail(o, issueRuntimeInvalid)
		return e
	}
	d.Runtime = make([]plugins.DockerContainerStats, 0, len(rows))
	for _, r := range rows {
		if !validID(r.ID) || r.Name == "" || r.CPUPerc == "" || r.MemUsage == "" || r.MemPerc == "" || r.NetIO == "" || r.BlockIO == "" || r.PIDs == "" {
			fail(o, issueRuntimeInvalid)
			return errors.New("invalid Docker stats response")
		}
		d.Runtime = append(d.Runtime, plugins.DockerContainerStats{ID: r.ID, Name: r.Name, CPUPerc: r.CPUPerc, MemUsage: r.MemUsage, MemPerc: r.MemPerc, NetIO: r.NetIO, BlockIO: r.BlockIO, PIDs: r.PIDs})
	}
	return nil
}

type statsRow struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

var fullID = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func validID(s string) bool { return fullID.MatchString(s) }
func (p *Plugin) config(ctx context.Context, d *plugins.DockerDetails, o *plugins.Outcome) error {
	b, e := p.call(ctx, "docker ps", "ps", "--all", "--quiet", "--no-trunc")
	if e != nil {
		failCommand(o, issueListUnavailable, e)
		return e
	}
	ids := strings.Fields(string(b))
	if len(ids) > maxContainers {
		fail(o, issueContainerLimit)
		return errors.New("Docker container count exceeded limit")
	}
	for _, id := range ids {
		if !validID(id) {
			fail(o, issueIDInvalid)
			return errors.New("invalid Docker container ID")
		}
	}
	var aggregate error
	for i := 0; i < len(ids); i += inspectBatch {
		if err := ctx.Err(); err != nil {
			return errors.Join(aggregate, err)
		}
		end := i + inspectBatch
		if end > len(ids) {
			end = len(ids)
		}
		out, err := p.call(ctx, "docker inspect", append([]string{"container", "inspect"}, ids[i:end]...)...)
		if err != nil {
			failCommand(o, issueInspectUnavailable, err)
			aggregate = errors.Join(aggregate, err)
			if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return errors.Join(aggregate, ctx.Err())
			}
			continue
		}
		var raw []inspectContainer
		if json.Unmarshal(out, &raw) != nil {
			fail(o, issueInspectInvalid)
			aggregate = errors.Join(aggregate, ErrInvalidResponse)
			continue
		}
		seen := make([]string, 0, end-i)
		for _, c := range raw {
			id := strings.TrimPrefix(c.ID, "sha256:")
			if !validID(id) || !contains(ids[i:end], id) || contains(seen, id) {
				fail(o, issueInspectInvalid)
				continue
			}
			seen = append(seen, id)
			if c.State == nil || c.State.Status == "" || c.Name == "" {
				fail(o, issueInspectIncomplete)
				continue
			}
			cfg := plugins.DockerContainerConfig{ID: id, Name: strings.TrimPrefix(c.Name, "/"), Status: c.State.Status, RestartCount: c.RestartCount, Health: "", OOMKilled: c.State.OOMKilled, ExitCode: c.State.ExitCode}
			if c.State.Health != nil {
				cfg.Health = c.State.Health.Status
			}
			if c.HostConfig != nil {
				cfg.MemoryLimit = c.HostConfig.Memory
				cfg.NanoCPUs = c.HostConfig.NanoCPUs
				cfg.PIDsLimit = c.HostConfig.PidsLimit
				cfg.Privileged = c.HostConfig.Privileged
				cfg.ReadonlyRootfs = c.HostConfig.ReadonlyRootfs
				cfg.LogDriver = c.HostConfig.LogConfig.Type
			}
			if c.HostConfig == nil {
				fail(o, issueHostConfigUnknown)
			} else if c.HostConfig.Memory == nil || c.HostConfig.Privileged == nil {
				fail(o, issueHostConfigUnknown)
			}
			d.Config = append(d.Config, cfg)
			if cfg.Privileged != nil && *cfg.Privileged {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingPrivileged, Severity: plugins.SeverityWarning, Summary: fmt.Sprintf("Container %s (%s) runs in privileged mode", cfg.Name, id)})
			}
			if cfg.MemoryLimit != nil && *cfg.MemoryLimit == 0 {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingUnlimitedMemory, Severity: plugins.SeverityInfo, Summary: fmt.Sprintf("Container %s (%s) has no memory limit", cfg.Name, id)})
			}
			if cfg.Health == stateUnhealthy || cfg.Status == stateRestarting {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingUnhealthy, Severity: plugins.SeverityWarning, Summary: fmt.Sprintf("Container %s (%s) is unhealthy or restarting", cfg.Name, id), Recommendation: "Inspect the health check and recent logs; verify the service before restarting."})
			}
			if cfg.OOMKilled != nil && *cfg.OOMKilled {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingContainerFailure, Severity: plugins.SeverityWarning, Summary: fmt.Sprintf("Container %s (%s) was killed after an out-of-memory event", cfg.Name, id), Recommendation: "Review memory usage and limits, then verify the workload before restarting."})
			} else if (cfg.Status == "exited" || cfg.Status == "dead") && cfg.ExitCode != nil && *cfg.ExitCode != 0 {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingContainerFailure, Severity: plugins.SeverityWarning, Summary: fmt.Sprintf("Container %s (%s) stopped with exit code %d", cfg.Name, id, *cfg.ExitCode), Recommendation: "Review the container logs and exit code before changing configuration or restarting."})
			} else if cfg.RestartCount > 0 {
				o.Findings = append(o.Findings, plugins.Finding{Code: findingContainerFailure, Severity: plugins.SeverityInfo, Summary: fmt.Sprintf("Container %s (%s) has restarted %d time(s) historically; this does not indicate a current failure", cfg.Name, id, cfg.RestartCount)})
			}
		}
		if len(seen) != len(ids[i:end]) {
			fail(o, issueInspectMissing)
			aggregate = errors.Join(aggregate, ErrInvalidResponse)
		}
	}
	if o.Status == plugins.StatusPartial {
		if aggregate == nil {
			aggregate = ErrInvalidResponse
		}
		return aggregate
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

type inspectContainer struct {
	ID           string             `json:"Id"`
	Name         string             `json:"Name"`
	RestartCount int64              `json:"RestartCount"`
	State        *inspectState      `json:"State"`
	HostConfig   *inspectHostConfig `json:"HostConfig"`
}
type inspectState struct {
	Status    string         `json:"Status"`
	Health    *inspectHealth `json:"Health"`
	OOMKilled *bool          `json:"OOMKilled"`
	ExitCode  *int64         `json:"ExitCode"`
}
type inspectHealth struct {
	Status string `json:"Status"`
}
type inspectLogConfig struct {
	Type string `json:"Type"`
}
type inspectHostConfig struct {
	Memory         *int64           `json:"Memory"`
	NanoCPUs       *int64           `json:"NanoCpus"`
	PidsLimit      *int64           `json:"PidsLimit"`
	Privileged     *bool            `json:"Privileged"`
	ReadonlyRootfs *bool            `json:"ReadonlyRootfs"`
	LogConfig      inspectLogConfig `json:"LogConfig"`
}

func decodeLines[T any](b []byte) ([]T, error) {
	if len(b) > outputLimit {
		return nil, ErrOutputLimit
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 4096), outputLimit)
	var rows []T
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		var v T
		if e := json.Unmarshal(line, &v); e != nil {
			return nil, ErrInvalidResponse
		}
		rows = append(rows, v)
	}
	if e := s.Err(); e != nil {
		return nil, ErrInvalidResponse
	}
	if len(rows) == 0 && !bytes.Equal(bytes.TrimSpace(b), []byte("")) {
		return nil, ErrInvalidResponse
	}
	return rows, nil
}
