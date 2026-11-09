package docker

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
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
	d.Images = imageDiagnostics(*env.Images)
	capacity := (*plugins.DockerHostCapacity)(nil)
	warnings, limits := []string{}, []string{}
	if d.Storage != nil {
		capacity = d.Storage.HostCapacity
		warnings = append(warnings, d.Storage.Warnings...)
		limits = append(limits, d.Storage.Limits...)
	}
	warnings = append(warnings, "Image sizes include shared layers and must not be summed as reclaimable storage.")
	limits = append(limits, "Image reference counts are Docker daemon accounting and do not establish deletion safety.")
	d.Storage = &plugins.DockerStorage{SharedLayers: true, SizeTotalsComparable: false, Warnings: warnings, Limits: limits, HostCapacity: capacity}
	return nil
}

func imageDiagnostics(rows []diskImage) []plugins.DockerImageDiagnostic {
	out := make([]plugins.DockerImageDiagnostic, 0, len(rows))
	for _, r := range rows {
		state := plugins.DockerImageReferenceUnknown
		var refs *int64
		if n, err := strconv.ParseInt(strings.TrimSpace(r.Containers), 10, 64); err == nil && n >= 0 {
			refs = &n
			if n > 0 {
				state = plugins.DockerImageReferenceReferenced
			} else {
				state = plugins.DockerImageReferenceUnreferenced
			}
		}
		tagState := plugins.DockerImageTagUnknown
		if r.Repository != "" && r.Tag != "" && r.Repository != "<none>" && r.Tag != "<none>" {
			tagState = plugins.DockerImageTagTagged
		} else if r.Repository == "<none>" && r.Tag == "<none>" {
			tagState = plugins.DockerImageTagDangling
		}
		out = append(out, plugins.DockerImageDiagnostic{Repository: r.Repository, Tag: r.Tag, ID: r.ID, Size: r.Size, SizeBytes: parseDockerBytes(r.Size), SharedSize: r.SharedSize, UniqueSize: r.UniqueSize, UniqueSizeBytes: parseDockerBytes(r.UniqueSize), ReferenceState: state, ReferenceCount: refs, TagState: tagState})
	}
	return out
}

func parseDockerBytes(s string) *int64 {
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) == 1 {
		for _, unit := range []string{"KiB", "MiB", "GiB", "TiB", "kB", "KB", "MB", "GB", "TB", "B"} {
			if strings.HasSuffix(parts[0], unit) {
				parts = []string{strings.TrimSuffix(parts[0], unit), unit}
				break
			}
		}
	}
	if len(parts) != 2 {
		return nil
	}
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil
	}
	var m float64
	for _, unit := range []struct {
		name       string
		multiplier float64
	}{{"B", 1}, {"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"KiB", 1024}, {"MiB", 1024 * 1024}, {"GiB", 1024 * 1024 * 1024}, {"TiB", math.Pow(1024, 4)}} {
		if unit.name == parts[1] {
			m = unit.multiplier
			break
		}
	}
	ok := m != 0
	if !ok || v*m >= float64(math.MaxInt64) {
		return nil
	}
	n := int64(v * m)
	return &n
}

func applyImageOptions(d *plugins.DockerDetails, o plugins.Options) {
	if d.Storage == nil {
		d.Storage = &plugins.DockerStorage{}
	}
	if o.ImageFilter != "" && o.ImageFilter != plugins.DockerImageFilterAll {
		filtered := make([]plugins.DockerImageDiagnostic, 0, len(d.Images))
		for _, img := range d.Images {
			switch o.ImageFilter {
			case plugins.DockerImageFilterUnused:
				if img.ReferenceState == plugins.DockerImageReferenceUnreferenced {
					filtered = append(filtered, img)
				}
			case plugins.DockerImageFilterReferenced:
				if img.ReferenceState == plugins.DockerImageReferenceReferenced {
					filtered = append(filtered, img)
				}
			case plugins.DockerImageFilterDangling:
				if img.TagState == plugins.DockerImageTagDangling {
					filtered = append(filtered, img)
				}
			}
		}
		d.Images = filtered
		for i := range d.DiskBreakdown {
			if d.DiskBreakdown[i].Category == "images" {
				items := make([]plugins.DockerDiskItem, 0, len(filtered))
				for _, img := range filtered {
					for _, item := range d.DiskBreakdown[i].Items {
						if item.ID == img.ID {
							items = append(items, item)
							break
						}
					}
				}
				d.DiskBreakdown[i].Items = items
			}
		}
	}
	switch o.DiskSort {
	case plugins.DockerDiskSortSize:
		sort.SliceStable(d.Images, func(i, j int) bool {
			return imageSizeLess(d.Images[i].SizeBytes, d.Images[j].SizeBytes)
		})
	case plugins.DockerDiskSortUniqueSize:
		sort.SliceStable(d.Images, func(i, j int) bool {
			return imageSizeLess(d.Images[i].UniqueSizeBytes, d.Images[j].UniqueSizeBytes)
		})
	case plugins.DockerDiskSortName:
		sort.SliceStable(d.Images, func(i, j int) bool {
			return d.Images[i].Repository+":"+d.Images[i].Tag < d.Images[j].Repository+":"+d.Images[j].Tag
		})
	}
}
func imageSizeLess(a, b *int64) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a > *b
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
