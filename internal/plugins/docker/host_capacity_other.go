//go:build !linux

package docker

import "github.com/gabriwl165/daffy/internal/plugins"

func verifyFilesystem(string, string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
	return filesystemCapacity{}, plugins.DockerHostReasonUnverified
}
