//go:build !linux && !darwin

package health

import "context"

const platformSupported = false
const useLinuxIntegrations = false
const findingPlatformUnsupported = "platform.unsupported"

func collectPlatformCore(context.Context, Options) Report { return unsupportedPlatformReport() }
func doctorPlatform(context.Context, Options) []Check {
	return []Check{{Name: "platform", Available: false, Detail: "native health provider unsupported"}}
}
func unsupportedPlatformReport() Report {
	return Report{SchemaVersion: "1", System: SystemInfo{OS: "unsupported", DistroID: "unsupported", DistroAdapter: "unsupported"}, Findings: []Finding{{ID: findingPlatformUnsupported, Area: AreaCore, Severity: SeverityUnknown, Summary: "Health collection unsupported on this platform", Confidence: ConfidenceLow}}}
}
