//go:build linux

package health

import "context"

const platformSupported = true
const useLinuxIntegrations = true

func collectPlatformCore(ctx context.Context, o Options) Report { return collectCore(ctx, o) }
func doctorPlatform(ctx context.Context, o Options) []Check     { return doctorLinux(ctx, o) }
