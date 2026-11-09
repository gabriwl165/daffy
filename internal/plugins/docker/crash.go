package docker

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const (
	crashOutputLimit      = 64 << 10
	maxEvidenceBytes      = 1 << 20
	maxEvidenceContainers = 20
)

func addCrashDiagnostic(ctx context.Context, p *Plugin, d *plugins.DockerDetails, raw inspectContainer, cfg plugins.DockerContainerConfig) error {
	s := raw.State
	if s == nil {
		return nil
	}
	classification := plugins.DockerCrashClassification("")
	code := plugins.DockerCrashCode("")
	switch {
	case plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateRestarting:
		classification, code = plugins.DockerCrashCurrentRestart, plugins.DockerCrashCodeRestarting
	case s.OOMKilled != nil && *s.OOMKilled:
		classification, code = plugins.DockerCrashRecordedOOM, plugins.DockerCrashCodeOOM
	case plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateRunning && s.Health != nil && plugins.DockerCrashState(s.Health.Status) == plugins.DockerCrashStateUnhealthy:
		classification, code = plugins.DockerCrashUnhealthy, plugins.DockerCrashCodeUnhealthy
	case (plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateExited || plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateDead) && s.ExitCode != nil && *s.ExitCode != 0:
		classification, code = plugins.DockerCrashNonzeroExit, plugins.DockerCrashCodeExit
	case cfg.RestartCount > 0:
		classification, code = plugins.DockerCrashHistoricalRestart, plugins.DockerCrashCodeRestartHistory
	default:
		return nil
	}
	state := plugins.DockerCrashStateStopped
	if plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateRunning {
		state = plugins.DockerCrashStateRunning
	}
	if plugins.DockerCrashState(s.Status) == plugins.DockerCrashStateRestarting {
		state = plugins.DockerCrashStateRestarting
	}
	diag := plugins.DockerCrashDiagnostic{ContainerID: cfg.ID, Name: cfg.Name, State: state, Classification: classification, Code: code, RestartCount: cfg.RestartCount, ExitCode: s.ExitCode, OOMKilled: s.OOMKilled, Project: cfg.Project, Service: cfg.Service, Recommendation: "Review the recorded state and bounded evidence; confirm the cause before changing configuration or restarting."}
	if !s.StartedAt.IsZero() {
		t := s.StartedAt
		diag.StartedAt = &t
	}
	if !s.FinishedAt.IsZero() {
		t := s.FinishedAt
		diag.FinishedAt = &t
	}
	if raw.HostConfig != nil {
		diag.RestartPolicy = raw.HostConfig.RestartPolicy.Name
	}
	opts, _ := ctx.Value(crashOptionsKey{}).(crashOptions)
	var evidenceErr error
	if opts.enabled {
		if len(d.Crashes) >= maxEvidenceContainers {
			diag.EvidenceTruncated = true
			diag.EvidenceIssues = append(diag.EvidenceIssues, "evidence container budget reached")
		} else {
			var healthLogs []inspectHealthLog
			if s.Health != nil {
				healthLogs = s.Health.Log
			}
			for i, h := range healthLogs {
				if i >= 5 {
					diag.EvidenceTruncated = true
					break
				}
				output := h.Output
				if len(output) > 2048 {
					output = output[:2048]
					diag.EvidenceTruncated = true
				}
				diag.HealthLog = append(diag.HealthLog, plugins.DockerHealthLogEntry{Start: timePtr(h.Start), End: timePtr(h.End), ExitCode: h.ExitCode, Output: output})
			}
			tail := opts.tail
			if tail == 0 {
				tail = 50
			}
			if tail > 200 {
				tail = 200
			}
			now := time.Now().UTC()
			since := now.Add(-24 * time.Hour)
			logs, err := p.callLogs(ctx, "logs", "--timestamps", "--tail", strconv.Itoa(tail), "--since", since.Format(time.RFC3339), "--until", now.Format(time.RFC3339), cfg.ID)
			if err != nil {
				evidenceErr = errors.Join(evidenceErr, err)
				diag.EvidenceIssues = append(diag.EvidenceIssues, "recent logs unavailable")
			} else {
				if len(logs) > crashOutputLimit {
					logs = logs[:crashOutputLimit]
					diag.EvidenceTruncated = true
				}
				budget := maxEvidenceBytes
				for _, c := range d.Crashes {
					for _, l := range c.RecentLogs {
						budget -= len(l)
					}
				}
				if budget < 0 {
					budget = 0
				}
				if len(logs) > budget {
					logs = logs[:budget]
					diag.EvidenceTruncated = true
				}
				for _, line := range strings.Split(string(logs), "\n") {
					if line != "" {
						diag.RecentLogs = append(diag.RecentLogs, line)
					}
				}
			}
			events, err := p.call(ctx, "docker events", "events", "--since", since.Format(time.RFC3339), "--until", now.Format(time.RFC3339), "--filter", "container="+cfg.ID, "--format", "{{.Action}}")
			if err != nil {
				evidenceErr = errors.Join(evidenceErr, err)
				diag.EvidenceIssues = append(diag.EvidenceIssues, "recent lifecycle history unavailable")
			} else {
				diag.HistoryIncomplete = true
				for _, line := range strings.Fields(string(events)) {
					switch line {
					case plugins.DockerEventDie:
						diag.RecentDieEvents++
					case plugins.DockerEventStart:
						diag.RecentStartEvents++
					case plugins.DockerEventRestart:
						diag.RecentRestartEvents++
					}
				}
			}
		}
		if ctx.Err() != nil {
			d.Crashes = append(d.Crashes, diag)
			return ctx.Err()
		}
		if len(diag.EvidenceIssues) > 0 {
			diag.Evidence = "opt-in evidence collection incomplete"
		}
	}
	d.Crashes = append(d.Crashes, diag)
	return evidenceErr
}
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
