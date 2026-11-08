//go:build darwin

package disk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinTraversalNoFollowAndAllocation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "dir", "file"), make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(filepath.Join(root, "dir", "file"), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("/", filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err = d.OpenChild("escape"); err == nil {
		t.Fatal("opened symlink directory")
	}
	meta, err := d.ChildMetadata("hardlink")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Links < 2 || meta.AllocatedBytes == 0 {
		t.Fatalf("unexpected hardlink metadata: %+v", meta)
	}
	if _, err = d.ChildMetadata("../etc"); err == nil {
		t.Fatal("accepted non-component child")
	}
	rootInfo, err := d.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entryMetadata(rootInfo); err != nil {
		t.Fatalf("root stat metadata: %v", err)
	}
	if _, err = openScanRoot(filepath.Join(root, "escape")); err == nil {
		t.Fatal("accepted symlink root component")
	}
	if _, err = NewScanner().Scan(context.Background(), Config{}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatal("default scan should be unsupported")
	}
	for _, restricted := range []string{"/Users/..", "/System/../System", "/System/Volumes/../Volumes/Data", "/System/Volumes"} {
		if _, err = NewScanner().Scan(context.Background(), Config{Path: restricted}); !errors.Is(err, ErrUnsupportedPlatform) {
			t.Errorf("restricted normalized root %q: %v", restricted, err)
		}
	}
}

func TestDarwinFIFOIsMetadataOnly(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "pipe")
	if err = syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	meta, err := d.ChildMetadata("pipe")
	if err != nil || meta.Mode&os.ModeNamedPipe == 0 || meta.Mode.IsRegular() {
		t.Fatalf("FIFO metadata: %+v, %v", meta, err)
	}
}

func TestDarwinScannerTraversesExplicitRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "excluded"), 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8192)
	if err = os.WriteFile(filepath.Join(root, "file"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(filepath.Join(root, "file"), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "excluded", "hidden"), data, 0600); err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewScanner().Scan(context.Background(), Config{Path: root, Excludes: []string{filepath.Join(root, "excluded")}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.Files != 2 {
		t.Fatalf("hardlinks should be visited but deduplicated in totals: %+v", r.Coverage)
	}
	if r.Totals.ApparentBytes != int64(len(data))+rootInfo.Size() {
		t.Fatalf("unexpected totals: %+v", r.Totals)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = NewScanner().Scan(ctx, Config{Path: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation result: %v", err)
	}
	if err = os.Rename(filepath.Join(root, "file"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("/", filepath.Join(root, "file")); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err = d.OpenChild("file"); err == nil {
		t.Fatal("replacement symlink was followed")
	}
}

func TestDarwinRejectsCaseVariantRootAndScopesFirmlinkCapacity(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "CaseProbe")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	variant := filepath.Join(parent, "caseprobe")
	if _, err = os.Stat(variant); err == nil {
		if _, err = openScanRoot(variant); err == nil {
			t.Fatal("case-variant root spelling was accepted")
		}
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(home, "/Users/") {
		var probe unix.Statfs_t
		if statErr := unix.Statfs(home, &probe); statErr == nil {
			platform := darwinScopedPlatform{root: home}
			mounts, mountErr := platform.Mounts()
			if mountErr != nil {
				t.Fatal(mountErr)
			}
			foundScopedVolume := false
			for _, mount := range mounts {
				if mount.Path == home && mount.Filesystem == FilesystemAPFS {
					foundScopedVolume = true
				}
			}
			if darwinString(probe.Mntonname[:]) != home && !foundScopedVolume {
				t.Fatal("firmlink volume was not represented by its scoped path")
			}
			capacity, capErr := platform.Capacity(home)
			if capErr != nil {
				t.Fatal(capErr)
			}
			var direct unix.Statfs_t
			if err = unix.Statfs(home, &direct); err != nil {
				t.Fatal(err)
			}
			if capacity.TotalBytes != uint64(direct.Blocks)*uint64(direct.Bsize) {
				t.Fatalf("firmlink capacity mismatch: %+v", capacity)
			}
		}
	}
}

func TestDarwinMountSnapshotPreservesUnsupportedAndFailsGrowth(t *testing.T) {
	query := func(buf []unix.Statfs_t, _ int) (int, error) {
		if buf == nil {
			return 2, nil
		}
		copy(buf[0].Mntonname[:], []byte("/"))
		copy(buf[0].Fstypename[:], []byte("apfs"))
		copy(buf[1].Mntonname[:], []byte("/Volumes/usb"))
		copy(buf[1].Fstypename[:], []byte("msdos"))
		return 2, nil
	}
	mounts, err := mountsFromSnapshot(query)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].Filesystem != FilesystemAPFS || mounts[1].Eligible {
		t.Fatalf("snapshot mount policy: %+v", mounts)
	}
	if _, err = mountsFromSnapshot(func(buf []unix.Statfs_t, _ int) (int, error) {
		if buf == nil {
			return 1, nil
		}
		return len(buf) + 1, nil
	}); err == nil {
		t.Fatal("growing snapshot was accepted")
	}
}

func TestDarwinPhysicalRootPolicyAndScopedMountEligibility(t *testing.T) {
	for _, tc := range []struct {
		path       string
		restricted bool
	}{
		{string(darwinSystemPath), true}, {"/System/Library", true},
		{string(darwinDataPath), true}, {"/System/Volumes/Data/Users/alice", false},
		{"/Systemish", false}, {"/Users/alice", false},
	} {
		if got := restrictedDarwinPhysicalRoot(tc.path); got != tc.restricted {
			t.Errorf("physical policy %q = %v, want %v", tc.path, got, tc.restricted)
		}
	}
	var st unix.Statfs_t
	copy(st.Fstypename[:], []byte("nfs"))
	m := scopedDarwinMount("/scope", st)
	if m.Eligible || m.SkipReason != SkipUnsupportedFilesystem || m.Filesystem == FilesystemAPFS {
		t.Fatalf("unsupported scoped filesystem was eligible: %+v", m)
	}
	copy(st.Fstypename[:], []byte("apfs"))
	m = scopedDarwinMount("/scope", st)
	if !m.Eligible || m.Filesystem != FilesystemAPFS {
		t.Fatalf("APFS scoped volume policy: %+v", m)
	}
}
