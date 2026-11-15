package disk

import (
	"context"
	"errors"
	"time"
)

type FileIdentity struct{ Device, Inode uint64 }

var ErrUnsupportedPlatform = errors.New("safe directory traversal is unsupported on this platform")

type Config struct {
	Path       string
	Excludes   []string
	TopN       int
	TimeBudget time.Duration
	BatchSize  int
	// OnProgress is called synchronously at most every 250ms, and at scan start/end.
	// Callbacks should be cheap and nonblocking.
	OnProgress func(Progress)
}

// Progress is a snapshot of scanner coverage at a point in time.
type Progress struct {
	Elapsed        time.Duration
	Files          int64
	Directories    int64
	SkippedEntries int64
	CurrentPath    string
}

type FileEntry struct {
	Path           string `json:"path"`
	ApparentBytes  int64  `json:"apparent_bytes"`
	AllocatedBytes int64  `json:"allocated_bytes"`
}
type DirectoryEntry struct {
	Path           string `json:"path"`
	ApparentBytes  int64  `json:"apparent_bytes"`
	AllocatedBytes int64  `json:"allocated_bytes"`
}
type MountCapacity struct {
	Path           string     `json:"path"`
	Filesystem     Filesystem `json:"filesystem"`
	TotalBytes     uint64     `json:"total_bytes"`
	AvailableBytes uint64     `json:"available_bytes"`
	TotalInodes    uint64     `json:"total_inodes"`
	FreeInodes     uint64     `json:"free_inodes"`
}
type Category string
type Risk string

const (
	CategoryPackageCache   Category = "package-cache"
	CategoryThumbnailCache Category = "thumbnail-cache"
	CategorySystemLogs     Category = "system-logs"
	RiskLow                Risk     = "low"
	RiskModerate           Risk     = "moderate"
)

type Candidate struct {
	Path                   string   `json:"path"`
	Category               Category `json:"category"`
	Risk                   Risk     `json:"risk"`
	ObservedAllocatedBytes int64    `json:"observed_allocated_bytes"`
	Explanation            string   `json:"explanation"`
	Recommendation         string   `json:"recommendation"`
}
type Issue struct {
	Path      string    `json:"path,omitempty"`
	Operation Operation `json:"operation"`
	Error     string    `json:"error"`
	// Kind classifies the failure; it is internal metadata and is not serialized.
	Kind IssueKind `json:"-"`
}
type IssueKind uint8

const (
	IssueKindUnknown IssueKind = iota
	IssueKindOther
	IssueKindPermission
)

type Coverage struct {
	Mounts         int   `json:"mounts"`
	ScannedMounts  int   `json:"scanned_mounts"`
	SkippedMounts  int   `json:"skipped_mounts"`
	Directories    int64 `json:"directories"`
	Files          int64 `json:"files"`
	SkippedEntries int64 `json:"skipped_entries"`
	OmittedErrors  int64 `json:"omitted_errors"`
	// PermissionErrors counts permission-failure events, not unique paths.
	PermissionErrors int64 `json:"-"`
	Complete         bool  `json:"complete"`
}
type Totals struct {
	ApparentBytes  int64 `json:"apparent_bytes"`
	AllocatedBytes int64 `json:"allocated_bytes"`
}
type Report struct {
	Root           string           `json:"root"`
	Totals         Totals           `json:"totals"`
	TopFiles       []FileEntry      `json:"top_files"`
	TopDirectories []DirectoryEntry `json:"top_directories"`
	Mounts         []MountCapacity  `json:"mounts"`
	Candidates     []Candidate      `json:"candidates"`
	Issues         []Issue          `json:"issues"`
	Coverage       Coverage         `json:"coverage"`
}
type Scanner interface {
	Scan(context.Context, Config) (Report, error)
}

// Platform isolates mount discovery and filesystem capacity from portable scanning.
type Platform interface {
	Mounts() ([]Mount, error)
	Capacity(string) (MountCapacity, error)
}
type Mount struct {
	Path       string
	Root       string
	Device     string
	Filesystem Filesystem
	Source     string
	Eligible   bool
	Reason     string
	SkipReason SkipReason
}

type Filesystem string
type SkipReason string
type Operation string
type Path string

const (
	SkipUnsupportedFilesystem SkipReason = "unsupported-filesystem"
	SkipBindAlias             SkipReason = "bind-alias"
	SkipRemovable             SkipReason = "removable-media"
	SkipNestedOverlay         SkipReason = "nested-overlay"
	SkipAmbiguousMount        SkipReason = "ambiguous-mountpoint"
	SkipExcludedMount         SkipReason = "excluded-mount"
	OperationMountDiscovery   Operation  = "mount-discovery"
	OperationMountSkip        Operation  = "mount-skip"
	OperationCapacity         Operation  = "capacity"
	OperationLstat            Operation  = "lstat"
	OperationReadDir          Operation  = "read-dir"
	OperationOpenDirectory    Operation  = "open-directory"
	FilesystemExt4            Filesystem = "ext4"
	FilesystemExt2            Filesystem = "ext2"
	FilesystemExt3            Filesystem = "ext3"
	FilesystemXFS             Filesystem = "xfs"
	FilesystemBtrfs           Filesystem = "btrfs"
	FilesystemF2FS            Filesystem = "f2fs"
	FilesystemVFAT            Filesystem = "vfat"
	FilesystemExFAT           Filesystem = "exfat"
	FilesystemNTFS            Filesystem = "ntfs"
	FilesystemNTFS3           Filesystem = "ntfs3"
	FilesystemFuseblk         Filesystem = "fuseblk"
	FilesystemZFS             Filesystem = "zfs"
	FilesystemBcacheFS        Filesystem = "bcachefs"
	FilesystemISO9660         Filesystem = "iso9660"
	FilesystemEROFS           Filesystem = "erofs"
	FilesystemOverlay         Filesystem = "overlay"
	FilesystemTmpfs           Filesystem = "tmpfs"
	FilesystemRamfs           Filesystem = "ramfs"
	FilesystemProc            Filesystem = "proc"
	FilesystemSysfs           Filesystem = "sysfs"
	FilesystemDevpts          Filesystem = "devpts"
	FilesystemDevtmpfs        Filesystem = "devtmpfs"
	FilesystemCgroup          Filesystem = "cgroup"
	FilesystemCgroup2         Filesystem = "cgroup2"
	FilesystemSecurityFS      Filesystem = "securityfs"
	FilesystemDebugFS         Filesystem = "debugfs"
	FilesystemTraceFS         Filesystem = "tracefs"
	FilesystemConfigFS        Filesystem = "configfs"
	FilesystemPstore          Filesystem = "pstore"
	FilesystemMqueue          Filesystem = "mqueue"
	FilesystemFusectl         Filesystem = "fusectl"
	FilesystemRPCPipeFS       Filesystem = "rpc_pipefs"
	FilesystemSquashFS        Filesystem = "squashfs"
	FilesystemNFS             Filesystem = "nfs"
	FilesystemNFS4            Filesystem = "nfs4"
	FilesystemCIFS            Filesystem = "cifs"
	FilesystemSMB3            Filesystem = "smb3"
	Filesystem9P              Filesystem = "9p"
	FilesystemAutofs          Filesystem = "autofs"
	FilesystemFuse            Filesystem = "fuse"
	FilesystemAPFS            Filesystem = "apfs"
)
