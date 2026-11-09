package docker

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/gabriwl165/daffy/internal/plugins"
)

type filesystemCapacity struct{ total, free, available, inodesTotal, inodesFree uint64 }
type filesystemProbe func(root, endpoint string) (filesystemCapacity, plugins.DockerHostUnavailableReason)

var probeFilesystem filesystemProbe = verifyFilesystem

func initHostCapacity(ctx context.Context, p *Plugin, d *plugins.DockerDetails) {
	state := &plugins.DockerHostCapacity{Available: false, UnavailableReason: plugins.DockerHostReasonUnverified}
	d.Storage = &plugins.DockerStorage{SharedLayers: true, SizeTotalsComparable: false, Warnings: []string{"Docker image sizes include shared layers and are not additive."}, Limits: []string{"Reference state does not establish deletion safety."}, HostCapacity: state}
	endpoint := os.Getenv("DOCKER_HOST")
	if targetContext(ctx) != "" || endpoint == "" {
		args := []string{"context", "inspect", "--format", "{{json .Endpoints.docker.Host}}"}
		if targetContext(ctx) != "" {
			args = append(args, targetContext(ctx))
		}
		endpointOut, err := p.call(ctx, "docker context endpoint", args...)
		if err != nil {
			return
		}
		if json.Unmarshal([]byte(strings.TrimSpace(string(endpointOut))), &endpoint) != nil {
			return
		}
	}
	if !strings.HasPrefix(endpoint, "unix://") {
		state.UnavailableReason = plugins.DockerHostReasonRemote
		return
	}
	rootOut, err := p.call(ctx, "docker info", "info", "--format", "{{json .DockerRootDir}}")
	if err != nil {
		state.UnavailableReason = plugins.DockerHostReasonRootUnavailable
		return
	}
	var root string
	if json.Unmarshal([]byte(strings.TrimSpace(string(rootOut))), &root) != nil || root == "" {
		state.UnavailableReason = plugins.DockerHostReasonRootUnavailable
		return
	}
	capacity, reason := probeFilesystem(root, strings.TrimPrefix(endpoint, "unix://"))
	if reason != "" {
		state.UnavailableReason = reason
		return
	}
	state.Available = true
	state.UnavailableReason = ""
	state.BytesTotal = &capacity.total
	state.BytesFree = &capacity.free
	state.BytesAvailable = &capacity.available
	state.InodesTotal = &capacity.inodesTotal
	state.InodesFree = &capacity.inodesFree
	state.LowBytes = capacity.total > 0 && float64(capacity.available)/float64(capacity.total) < 0.10
	state.LowInodes = capacity.inodesTotal > 0 && float64(capacity.inodesFree)/float64(capacity.inodesTotal) < 0.10
	if state.LowBytes {
		d.Storage.Warnings = append(d.Storage.Warnings, "Daemon filesystem has less than 10% available bytes.")
	}
	if state.LowInodes {
		d.Storage.Warnings = append(d.Storage.Warnings, "Daemon filesystem has less than 10% available inodes.")
	}
}
