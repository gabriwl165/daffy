package disk

import "strings"

const (
	aptArchiveCachePath    = "/var/cache/apt/archives"
	journalDirectoryPath   = "/var/log/journal"
	rootThumbnailCachePath = "/root/.cache/thumbnails"
	userHomePrefix         = "/home/"
	thumbnailCacheSuffix   = "/.cache/thumbnails"
)

// candidateForDirectory suggests review targets only; it never deletes data.
func candidateForDirectory(path string, totals Totals) (Candidate, bool) {
	if totals.AllocatedBytes <= 0 {
		return Candidate{}, false
	}
	var candidate Candidate
	switch {
	case path == aptArchiveCachePath:
		candidate = Candidate{Path: path, Category: CategoryPackageCache, Risk: RiskLow,
			Explanation:    "Observed allocated bytes in the APT package archive cache are not guaranteed reclaimable; cached packages may be useful offline.",
			Recommendation: "Inspect this host's APT cache and use apt-get clean only for an APT-managed host; this removes cached packages, not installed packages."}
	case path == journalDirectoryPath:
		candidate = Candidate{Path: path, Category: CategorySystemLogs, Risk: RiskModerate,
			Explanation:    "Observed allocated bytes in the native system journal are not guaranteed reclaimable; logs may be needed for troubleshooting or audits.",
			Recommendation: "Inspect usage with journalctl --disk-usage, then choose an appropriate retention policy or journalctl vacuum operation; do not remove automatically."}
	case path == rootThumbnailCachePath || isSingleUserThumbnailCache(path):
		candidate = Candidate{Path: path, Category: CategoryThumbnailCache, Risk: RiskLow,
			Explanation:    "Observed allocated bytes in a thumbnail cache are not guaranteed reclaimable; thumbnails are generally regenerable, but review the owning user's data and applications.",
			Recommendation: "Review with the user and use application-owned cleanup if appropriate; no files are removed automatically."}
	default:
		return Candidate{}, false
	}
	candidate.ObservedAllocatedBytes = totals.AllocatedBytes
	return candidate, true
}

func isSingleUserThumbnailCache(path string) bool {
	if !strings.HasPrefix(path, userHomePrefix) || !strings.HasSuffix(path, thumbnailCacheSuffix) {
		return false
	}
	username := strings.TrimSuffix(strings.TrimPrefix(path, userHomePrefix), thumbnailCacheSuffix)
	return username != "" && !strings.Contains(username, "/") && username != "." && username != ".."
}
