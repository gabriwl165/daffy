package disk

import "testing"

func TestCleanupCandidates(t *testing.T) {
	tests := []struct {
		path string
		want Category
	}{
		{aptArchiveCachePath, CategoryPackageCache},
		{journalDirectoryPath, CategorySystemLogs},
		{rootThumbnailCachePath, CategoryThumbnailCache},
		{"/home/alice/.cache/thumbnails", CategoryThumbnailCache},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := candidateForDirectory(tt.path, Totals{AllocatedBytes: 4096})
			if !ok || got.Category != tt.want || got.ObservedAllocatedBytes != 4096 {
				t.Fatalf("candidateForDirectory(%q) = (%+v, %v)", tt.path, got, ok)
			}
		})
	}
}

func TestCleanupRejectsUnrecognizedPaths(t *testing.T) {
	paths := []string{
		"/var/cache/apt/archives-old", "/var/cache/apt/archives/child",
		"/var/cache/apt/archives/../other", "/backup/var/cache/apt/archives",
		"/home/alice/.cache", "/home/alice/.cache/chrome", "/home/alice/.cache/thumbnails/child",
		"/home/alice/other/.cache/thumbnails", "/home/alice/bob/.cache/thumbnails",
		"/home/.cache/thumbnails", "/tmp/var/cache/apt/archives", "/home/alice/.cache/browser",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if got, ok := candidateForDirectory(path, Totals{AllocatedBytes: 1}); ok {
				t.Fatalf("unexpected candidate: %+v", got)
			}
		})
	}
}

func TestCleanupRequiresPositiveAllocatedBytes(t *testing.T) {
	for _, allocated := range []int64{0, -1} {
		if got, ok := candidateForDirectory(aptArchiveCachePath, Totals{ApparentBytes: 9000, AllocatedBytes: allocated}); ok {
			t.Fatalf("unexpected candidate for %d allocated bytes: %+v", allocated, got)
		}
	}
}
