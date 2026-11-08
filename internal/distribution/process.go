package distribution

import "github.com/gabriwl165/daffy/internal/distribution/process"

type ProcessTargetKind = process.ProcessTargetKind
type ProcessTarget = process.ProcessTarget
type ProcessSignal = process.ProcessSignal
type ProcessErrorKind = process.ProcessErrorKind
type ProcessError = process.ProcessError
type ProcessResult = process.ProcessResult
type ProcessController = process.ProcessController

const (
	TargetPID              = process.TargetPID
	TargetTCPPort          = process.TargetTCPPort
	SignalTerminate        = process.SignalTerminate
	ProcessInvalidTarget   = process.ProcessInvalidTarget
	ProcessNotFound        = process.ProcessNotFound
	ProcessAmbiguous       = process.ProcessAmbiguous
	ProcessDiscoveryFailed = process.ProcessDiscoveryFailed
	ProcessIdentityChanged = process.ProcessIdentityChanged
	ProcessSignalFailed    = process.ProcessSignalFailed
)
