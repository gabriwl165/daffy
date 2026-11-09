package plugins

type DockerImageFilter string
type DockerDiskSort string
type DockerImageReferenceState string
type DockerImageTagState string
type DockerHostUnavailableReason string

const (
	DockerImageFilterAll             DockerImageFilter           = "all"
	DockerImageFilterUnused          DockerImageFilter           = "unused"
	DockerImageFilterDangling        DockerImageFilter           = "dangling"
	DockerImageFilterReferenced      DockerImageFilter           = "referenced"
	DockerDiskSortSize               DockerDiskSort              = "size"
	DockerDiskSortUniqueSize         DockerDiskSort              = "unique-size"
	DockerDiskSortName               DockerDiskSort              = "name"
	DockerImageReferenceUnknown      DockerImageReferenceState   = "unknown"
	DockerImageReferenceUnreferenced DockerImageReferenceState   = "unreferenced"
	DockerImageReferenceReferenced   DockerImageReferenceState   = "referenced"
	DockerImageTagUnknown            DockerImageTagState         = "unknown"
	DockerImageTagTagged             DockerImageTagState         = "tagged"
	DockerImageTagDangling           DockerImageTagState         = "dangling"
	DockerHostReasonUnverified       DockerHostUnavailableReason = "daemon-filesystem-unverified"
	DockerHostReasonRemote           DockerHostUnavailableReason = "remote-daemon"
	DockerHostReasonFilesystem       DockerHostUnavailableReason = "filesystem-unavailable"
	DockerHostReasonRootUnavailable  DockerHostUnavailableReason = "daemon-root-unavailable"
)

type DockerImageDiagnostic struct {
	Repository      string                    `json:"repository,omitempty"`
	Tag             string                    `json:"tag,omitempty"`
	ID              string                    `json:"id"`
	Size            string                    `json:"size"`
	SizeBytes       *int64                    `json:"size_bytes,omitempty"`
	SharedSize      string                    `json:"shared_size,omitempty"`
	UniqueSize      string                    `json:"unique_size,omitempty"`
	UniqueSizeBytes *int64                    `json:"unique_size_bytes,omitempty"`
	ReferenceState  DockerImageReferenceState `json:"reference_state"`
	ReferenceCount  *int64                    `json:"reference_count,omitempty"`
	TagState        DockerImageTagState       `json:"tag_state"`
}
type DockerHostCapacity struct {
	Available         bool                        `json:"available"`
	UnavailableReason DockerHostUnavailableReason `json:"unavailable_reason,omitempty"`
	BytesTotal        *uint64                     `json:"bytes_total,omitempty"`
	BytesFree         *uint64                     `json:"bytes_free,omitempty"`
	BytesAvailable    *uint64                     `json:"bytes_available,omitempty"`
	InodesTotal       *uint64                     `json:"inodes_total,omitempty"`
	InodesFree        *uint64                     `json:"inodes_free,omitempty"`
	LowBytes          bool                        `json:"low_bytes"`
	LowInodes         bool                        `json:"low_inodes"`
}
type DockerStorage struct {
	SharedLayers         bool                `json:"shared_layers"`
	SizeTotalsComparable bool                `json:"size_totals_comparable"`
	Warnings             []string            `json:"warnings,omitempty"`
	Limits               []string            `json:"limits,omitempty"`
	HostCapacity         *DockerHostCapacity `json:"host_capacity,omitempty"`
}
