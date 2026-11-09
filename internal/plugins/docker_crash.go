package plugins

import "time"

type DockerCrashState string
type DockerCrashClassification string
type DockerCrashCode string

const (
	DockerCrashStateRestarting    DockerCrashState          = "restarting"
	DockerCrashStateRunning       DockerCrashState          = "running"
	DockerCrashStateStopped       DockerCrashState          = "stopped"
	DockerCrashCurrentRestart     DockerCrashClassification = "current_restart"
	DockerCrashRecordedOOM        DockerCrashClassification = "recorded_oom"
	DockerCrashUnhealthy          DockerCrashClassification = "currently_unhealthy"
	DockerCrashNonzeroExit        DockerCrashClassification = "stopped_nonzero_exit"
	DockerCrashHistoricalRestart  DockerCrashClassification = "historical_restarts"
	DockerCrashCodeRestarting     DockerCrashCode           = "container_restarting"
	DockerCrashCodeOOM            DockerCrashCode           = "oom_killed"
	DockerCrashCodeUnhealthy      DockerCrashCode           = "container_unhealthy"
	DockerCrashCodeExit           DockerCrashCode           = "nonzero_exit"
	DockerCrashCodeRestartHistory DockerCrashCode           = "restart_history"
	DockerEventDie                                          = "die"
	DockerEventStart                                        = "start"
	DockerEventRestart                                      = "restart"
)

type DockerCrashDiagnostic struct {
	ContainerID         string                    `json:"container_id"`
	Name                string                    `json:"name"`
	State               DockerCrashState          `json:"state"`
	Classification      DockerCrashClassification `json:"classification"`
	Code                DockerCrashCode           `json:"code"`
	StartedAt           *time.Time                `json:"started_at,omitempty"`
	FinishedAt          *time.Time                `json:"finished_at,omitempty"`
	RestartPolicy       string                    `json:"restart_policy,omitempty"`
	RestartCount        int64                     `json:"restart_count"`
	ExitCode            *int64                    `json:"exit_code,omitempty"`
	OOMKilled           *bool                     `json:"oom_killed,omitempty"`
	Evidence            string                    `json:"evidence,omitempty"`
	HealthLog           []DockerHealthLogEntry    `json:"health_log,omitempty"`
	RecentLogs          []string                  `json:"recent_logs,omitempty"`
	RecentFailureEvents int                       `json:"recent_failure_events,omitempty"`
	RecentDieEvents     int                       `json:"recent_die_events,omitempty"`
	RecentStartEvents   int                       `json:"recent_start_events,omitempty"`
	RecentRestartEvents int                       `json:"recent_restart_events,omitempty"`
	HistoryIncomplete   bool                      `json:"history_incomplete,omitempty"`
	EvidenceTruncated   bool                      `json:"evidence_truncated,omitempty"`
	Project             string                    `json:"project,omitempty"`
	Service             string                    `json:"service,omitempty"`
	Recommendation      string                    `json:"recommendation,omitempty"`
	EvidenceIssues      []string                  `json:"evidence_issues,omitempty"`
}
type DockerHealthLogEntry struct {
	Start    *time.Time `json:"start,omitempty"`
	End      *time.Time `json:"end,omitempty"`
	ExitCode int64      `json:"exit_code"`
	Output   string     `json:"output,omitempty"`
}
