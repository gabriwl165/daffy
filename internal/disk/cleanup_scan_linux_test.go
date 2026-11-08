//go:build linux

package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupScanCandidateIndependentOfTopN(t *testing.T) {
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "cached-package.deb"), []byte("cached data"), 0600); err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{mounts: []Mount{diskMount(aptArchiveCachePath, "fixture", FilesystemExt4)}}
	s := &scanner{platform: platform, candidate: candidateForDirectory, openRoot: func(path string) (Directory, error) {
		if path != aptArchiveCachePath {
			t.Fatalf("unexpected logical root %q", path)
		}
		return openScanRoot(fixture)
	}}
	report, err := s.Scan(context.Background(), Config{Path: aptArchiveCachePath, TopN: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.TopDirectories) != 0 {
		t.Fatalf("TopN=0 returned directories: %+v", report.TopDirectories)
	}
	if len(report.Candidates) != 1 {
		t.Fatalf("candidates: %+v", report.Candidates)
	}
	if report.Candidates[0].Path != aptArchiveCachePath || report.Candidates[0].ObservedAllocatedBytes <= 0 {
		t.Fatalf("candidate did not reflect scanned directory: %+v", report.Candidates[0])
	}
}

func TestCleanupScanDoesNotSuggestExcludedOrUnselectedPaths(t *testing.T) {
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "cached-package.deb"), []byte("cached data"), 0600); err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{mounts: []Mount{
		diskMount(aptArchiveCachePath, "apt", FilesystemExt4),
		diskMount(journalDirectoryPath, "journal", FilesystemExt4),
	}}
	openCalls := 0
	s := &scanner{platform: platform, candidate: candidateForDirectory, openRoot: func(path string) (Directory, error) {
		openCalls++
		if path != aptArchiveCachePath {
			t.Fatalf("unselected path opened: %q", path)
		}
		return openScanRoot(fixture)
	}}
	report, err := s.Scan(context.Background(), Config{Path: aptArchiveCachePath, Excludes: []string{aptArchiveCachePath}})
	if err != nil {
		t.Fatal(err)
	}
	if openCalls != 0 {
		t.Fatalf("excluded root opened %d times", openCalls)
	}
	if len(report.Candidates) != 0 {
		t.Fatalf("excluded/unselected candidates: %+v", report.Candidates)
	}
}
