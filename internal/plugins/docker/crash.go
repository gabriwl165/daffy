package docker

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const crashOutputLimit = 64 << 10

func addCrashDiagnostic(ctx context.Context, p *Plugin, d *plugins.DockerDetails, raw inspectContainer, cfg plugins.DockerContainerConfig) {
	state := raw.State
	if state == nil {
		return
	}
	classification := plugins.DockerCrashClassification("")
	code := plugins.DockerCrashCode("")
	switch {
	case state.Status == "restarting":
		classification, code = plugins.DockerCrashCurrentRestart, plugins.DockerCrashCodeRestarting
	case state.OOMKilled != nil && *state.OOMKilled:
		classification, code = plugins.DockerCrashRecordedOOM, plugins.DockerCrashCodeOOM
	case state.Status == "running" && state.Health != nil && state.Health.Status == "unhealthy":
		classification, code = plugins.DockerCrashUnhealthy, plugins.DockerCrashCodeUnhealthy
	case (state.Status == "exited" || state.Status == "dead") && state.ExitCode != nil && *state.ExitCode != 0:
		classification, code = plugins.DockerCrashNonzeroExit, plugins.DockerCrashCodeExit
	case cfg.RestartCount > 0:
		classification, code = plugins.DockerCrashHistoricalRestart, plugins.DockerCrashCodeRestartHistory
	default:
		return
	}
	status := plugins.DockerCrashStateStopped
	if state.Status == "running" {
		status = plugins.DockerCrashStateRunning
	}
	if state.Status == "restarting" {
		status = plugins.DockerCrashStateRestarting
	}
	diag := plugins.DockerCrashDiagnostic{ContainerID: cfg.ID, Name: cfg.Name, State: status, Classification: classification, Code: code, RestartCount: cfg.RestartCount, ExitCode: state.ExitCode, OOMKilled: state.OOMKilled}
	if !state.StartedAt.IsZero() {
		t := state.StartedAt
		diag.StartedAt = &t
	}
	if !state.FinishedAt.IsZero() {
		t := state.FinishedAt
		diag.FinishedAt = &t
	}
	if raw.HostConfig != nil {
		diag.RestartPolicy = raw.HostConfig.RestartPolicy.Name
	}
	opts, _ := ctx.Value(crashOptionsKey{}).(crashOptions)
	if opts.enabled {
		if state.Health != nil {
			for _, h := range state.Health.Log {
				output := boundedText(h.Output, 2048)
				diag.HealthLog = append(diag.HealthLog, plugins.DockerHealthLogEntry{Start: timePtr(h.Start), End: timePtr(h.End), ExitCode: h.ExitCode, Output: output})
			}
		}
		tail := opts.tail
		if tail == 0 {
			tail = 50
		}
		if tail > 200 {
			tail = 200
		}
		logs, err := p.call(ctx, "docker logs", "logs", "--timestamps", "--tail", stringInt(tail), "--since", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), "--until", time.Now().UTC().Format(time.RFC3339), cfg.ID)
		if err == nil {
			for _, line := range strings.Split(boundedText(string(logs), crashOutputLimit), "\n") {
				if line != "" {
					diag.RecentLogs = append(diag.RecentLogs, line)
				}
			}
		} else {
			diag.Evidence = "recent logs unavailable"
		}
		// Docker event stream is time-bounded; count only failure/restart lifecycle events.
		events, err := p.call(ctx, "docker events", "events", "--since", "24h", "--until", "0s", "--filter", "container="+cfg.ID, "--filter", "event=die", "--filter", "event=start", "--filter", "event=restart", "--format", "{{.Action}}")
		if err == nil {
			for _, line := range strings.Fields(string(events)) {
				if line == "die" || line == "start" || line == "restart" {
					diag.RecentFailureEvents++
				}
			}
		}
	}
	d.Crashes = append(d.Crashes, diag)
}

func boundedText(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
func stringInt(n int) string { return strconv.Itoa(n) }
