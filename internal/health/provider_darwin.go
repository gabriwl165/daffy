//go:build darwin

package health

import "context"

const platformSupported = true
const useLinuxIntegrations = false

func collectPlatformCore(ctx context.Context, o Options) Report { return collectDarwin(ctx, o) }
func doctorPlatform(ctx context.Context, o Options) []Check {
	return []Check{{Name: "platform", Available: true, Detail: "macOS native health provider"}}
}
