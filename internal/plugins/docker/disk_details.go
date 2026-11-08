package docker

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gabriwl165/daffy/internal/plugins"
)

// Fixture compatibility is based on Docker CLI v29.1.3 formatter disk_usage.go
// and reflect.go: https://github.com/docker/cli/blob/v29.1.3/cli/command/formatter/disk_usage.go
// The formatter's verbose JSON is one envelope object with four string-row arrays.
type diskEnvelope struct {
	Images     *[]diskImage     `json:"Images"`
	Containers *[]diskContainer `json:"Containers"`
	Volumes    *[]diskVolume    `json:"Volumes"`
	BuildCache *[]diskCache     `json:"BuildCache"`
}
type diskImage struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	SharedSize string `json:"SharedSize"`
	UniqueSize string `json:"UniqueSize"`
	Containers string `json:"Containers"`
}
type diskContainer struct {
	ID           string `json:"ID"`
	Names        string `json:"Names"`
	Image        string `json:"Image"`
	Size         string `json:"Size"`
	LocalVolumes string `json:"LocalVolumes"`
	Status       string `json:"Status"`
}
type diskVolume struct {
	Name  string `json:"Name"`
	Links string `json:"Links"`
	Size  string `json:"Size"`
}
type diskCache struct {
	ID         string `json:"ID"`
	CacheType  string `json:"CacheType"`
	Size       string `json:"Size"`
	UsageCount string `json:"UsageCount"`
	InUse      string `json:"InUse"`
	Shared     string `json:"Shared"`
}

func (p *Plugin) diskDetails(ctx context.Context, d *plugins.DockerDetails, o *plugins.Outcome) error {
	b, err := p.call(ctx, "docker system df details", "system", "df", "-v", "--format", "{{json .}}")
	if err != nil {
		failCommand(o, issueDiskUnavailable, err)
		return err
	}
	var env diskEnvelope
	if len(b) > outputLimit || json.Unmarshal(b, &env) != nil || env.Images == nil || env.Containers == nil || env.Volumes == nil || env.BuildCache == nil {
		fail(o, issueDiskInvalid)
		return ErrInvalidResponse
	}
	for _, r := range *env.Images {
		if !hasValue(r.ID) || !hasValue(r.Size) {
			fail(o, issueDiskInvalid)
			return ErrInvalidResponse
		}
	}
	for _, r := range *env.Containers {
		if !hasValue(r.ID) || !hasValue(r.Size) {
			fail(o, issueDiskInvalid)
			return ErrInvalidResponse
		}
	}
	for _, r := range *env.Volumes {
		if !hasValue(r.Name) || !hasValue(r.Size) {
			fail(o, issueDiskInvalid)
			return ErrInvalidResponse
		}
	}
	for _, r := range *env.BuildCache {
		if !hasValue(r.ID) || !hasValue(r.Size) {
			fail(o, issueDiskInvalid)
			return ErrInvalidResponse
		}
	}
	d.DiskBreakdown = []plugins.DockerDiskBreakdown{
		{Category: "images", Items: mapImages(*env.Images)},
		{Category: "containers", Items: mapContainers(*env.Containers)},
		{Category: "volumes", Items: mapVolumes(*env.Volumes)},
		{Category: "build cache", Items: mapCache(*env.BuildCache)},
	}
	return nil
}
func hasValue(s string) bool { return strings.TrimSpace(s) != "" }
func mapImages(rows []diskImage) []plugins.DockerDiskItem {
	out := make([]plugins.DockerDiskItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, plugins.DockerDiskItem{ID: r.ID, Repository: r.Repository, Tag: r.Tag, Size: r.Size, SharedSize: r.SharedSize, UniqueSize: r.UniqueSize, Containers: r.Containers})
	}
	return out
}
func mapContainers(rows []diskContainer) []plugins.DockerDiskItem {
	out := make([]plugins.DockerDiskItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, plugins.DockerDiskItem{ID: r.ID, Names: r.Names, Image: r.Image, Size: r.Size, LocalVolumes: r.LocalVolumes, Status: r.Status})
	}
	return out
}
func mapVolumes(rows []diskVolume) []plugins.DockerDiskItem {
	out := make([]plugins.DockerDiskItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, plugins.DockerDiskItem{Name: r.Name, Links: r.Links, Size: r.Size})
	}
	return out
}
func mapCache(rows []diskCache) []plugins.DockerDiskItem {
	out := make([]plugins.DockerDiskItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, plugins.DockerDiskItem{ID: r.ID, CacheType: r.CacheType, Size: r.Size, UsageCount: r.UsageCount, InUse: r.InUse, Shared: r.Shared})
	}
	return out
}
