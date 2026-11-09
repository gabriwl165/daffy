//go:build !linux

package docker

import (
	"context"
	"github.com/gabriwl165/daffy/internal/plugins"
)

func verifyFilesystem(context.Context, string, string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
	return filesystemCapacity{}, plugins.DockerHostReasonUnverified
}
