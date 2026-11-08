//go:build linux

package disk

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTraversalMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "file"), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m, err := d.ChildMetadata("hardlink")
	if err != nil {
		t.Fatal(err)
	}
	if m.Links != 2 || m.Identity.Inode == 0 || m.ApparentBytes != 5 {
		t.Fatalf("metadata: %+v", m)
	}
	symlink, err := d.ChildMetadata("dangling")
	if err != nil {
		t.Fatal(err)
	}
	if symlink.Mode&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink metadata: %+v", symlink)
	}
	if _, err := d.OpenChild("dangling"); err == nil {
		t.Fatal("opened symlink child")
	}
}

func TestTraversalRejectsSymlinkRootComponent(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openScanRoot(link); err == nil {
		t.Fatal("opened symlink root")
	}
}

func TestTraversalRejectsReplacementSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.ChildMetadata("child"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "child"), filepath.Join(root, "old-child")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "child")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.OpenChild("child"); err == nil {
		t.Fatal("opened replacement symlink")
	}
}

func TestTraversalMetadataDoesNotOpenSpecialFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "regular"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := openScanRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, name := range []string{"regular", "fifo"} {
		if _, err := d.ChildMetadata(name); err != nil {
			t.Fatalf("metadata for %s: %v", name, err)
		}
	}
}

func TestTraversalRejectsInvalidChildNames(t *testing.T) {
	d, err := openScanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, name := range []string{"..", "a/b"} {
		if _, err := d.ChildMetadata(name); err == nil {
			t.Errorf("ChildMetadata accepted %q", name)
		}
		if _, err := d.OpenChild(name); err == nil {
			t.Errorf("OpenChild accepted %q", name)
		}
	}
}
