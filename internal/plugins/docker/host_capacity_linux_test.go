//go:build linux

package docker

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gabriwl165/daffy/internal/plugins"
)

func TestFilesystemVerifierRejectsMissingSocketAndNamespaceMismatch(t *testing.T) {
	tmp := t.TempDir()
	if _, reason := verifyFilesystem(context.Background(), tmp, filepath.Join(tmp, "missing.sock")); reason != plugins.DockerHostReasonUnverified {
		t.Fatalf("missing socket reason=%q", reason)
	}
	socketPath := filepath.Join(tmp, "docker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	proc := filepath.Join(tmp, "proc")
	for _, dir := range []string{"net", "self/ns", "55/ns", "55/fd"} {
		if err := os.MkdirAll(filepath.Join(proc, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proc, "net/unix"), []byte("Num RefCount Protocol Flags Type St Inode Path\n000: 00000002 00000000 00010000 0001 01 987 "+socketPath+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"self/ns/mnt": "mnt:[1]", "self/root": root, "55/ns/mnt": "mnt:[2]", "55/root": root, "55/fd/3": "socket:[987]"} {
		if err := os.Symlink(target, filepath.Join(proc, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proc, "55/comm"), []byte("dockerd\n"), 0600); err != nil {
		t.Fatal(err)
	}
	statCalls := 0
	_, reason := verifyFilesystemAt(context.Background(), root, socketPath, proc, func(string, *syscall.Statfs_t) error { statCalls++; return nil })
	if reason != plugins.DockerHostReasonUnverified || statCalls != 0 {
		t.Fatalf("namespace mismatch reason=%q stat calls=%d", reason, statCalls)
	}
}
