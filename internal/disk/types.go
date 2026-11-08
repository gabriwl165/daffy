package disk

import (
	"context"
	"time"
)

type Config struct {
	Path       string
	Excludes   []string
	TopN       int
	TimeBudget time.Duration
	BatchSize  int
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
	Path           string `json:"path"`
	Filesystem     string `json:"filesystem"`
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	TotalInodes    uint64 `json:"total_inodes"`
	FreeInodes     uint64 `json:"free_inodes"`
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
	Path      string `json:"path,omitempty"`
	Operation string `json:"operation"`
	Error     string `json:"error"`
}
type Coverage struct {
	Mounts         int   `json:"mounts"`
	ScannedMounts  int   `json:"scanned_mounts"`
	SkippedMounts  int   `json:"skipped_mounts"`
	Directories    int64 `json:"directories"`
	Files          int64 `json:"files"`
	SkippedEntries int64 `json:"skipped_entries"`
	OmittedErrors  int64 `json:"omitted_errors"`
	Complete       bool  `json:"complete"`
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
	Filesystem string
	Source     string
	Eligible   bool
	Reason     string
}

func Scan(ctx context.Context, c Config) (Report, error) { return NewScanner().Scan(ctx, c) }
func NewScanner() Scanner                                { return newScanner() }
