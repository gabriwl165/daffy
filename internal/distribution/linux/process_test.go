//go:build linux

package linux

import (
	"context"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseStartTimeWithParenthesesInComm(t *testing.T) {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "x"
	}
	fields[19] = "98765"
	input := "123 (comm with ) parens) " + join(fields, " ")
	got, err := parseStartTime(input)
	if err != nil || got != "98765" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestFinderProcFixturesIPv4IPv6AndMultipleOwners(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"net", "123/fd", "123", "124/fd", "124"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	row := func(port, inode string) string {
		return "  0: 00000000:" + port + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 " + inode + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, "net/tcp"), []byte(header+row("1F90", "9001")), 0600); err != nil {
		t.Fatal(err)
	}
	ipv6Header := strings.Replace(header, "rem_address", "remote_address", 1)
	if err := os.WriteFile(filepath.Join(root, "net/tcp6"), []byte(ipv6Header+row("1F90", "9002")), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ pid, ino string }{{"123", "9001"}, {"124", "9002"}} {
		stat := item.pid + " (proc) S" + strings.Repeat(" x", 18) + " 77\n"
		if err := os.WriteFile(filepath.Join(root, item.pid, "stat"), []byte(stat), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("socket:["+item.ino+"]", filepath.Join(root, item.pid, "fd/3")); err != nil {
			t.Fatal(err)
		}
	}
	owners, err := NewProcessFinder(root).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 8080})
	if err != nil || len(owners) != 2 {
		t.Fatalf("owners=%v err=%v", owners, err)
	}
}

func TestOwnedChildHelper(t *testing.T) {
	if os.Getenv("DAFFY_PROCESS_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
}
func TestTerminateOwnedChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedChildHelper$")
	cmd.Env = append(os.Environ(), "DAFFY_PROCESS_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGKILL)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("child cleanup timed out")
			}
		}
	}()
	controller := process.NewController(NewProcessFinder("/proc"), ownedChildSignaller{})
	result, err := controller.Terminate(context.Background(), process.ProcessTarget{Kind: process.TargetPID, PID: cmd.Process.Pid})
	if err != nil {
		t.Fatal(err)
	}
	if result.PID != cmd.Process.Pid {
		t.Fatalf("signalled unexpected pid %d", result.PID)
	}
	select {
	case err := <-done:
		waited = true
		if err == nil {
			t.Fatal("child exited without signal")
		}
		if !strings.Contains(err.Error(), "signal: terminated") {
			t.Fatalf("child result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned child did not terminate")
	}
}

type ownedChildSignaller struct{}

func (ownedChildSignaller) Signal(pid int, s os.Signal) error {
	p, e := os.FindProcess(pid)
	if e != nil {
		return e
	}
	return p.Signal(s)
}

func TestParseStartTimeRejectsInvalid(t *testing.T) {
	for _, input := range []string{"bad", "22 (comm) S", "22 (comm) S" + strings.Repeat(" x", 18) + " nope", "22 (comm) S" + strings.Repeat(" x", 18) + " 0"} {
		if _, err := parseStartTime(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
func TestParseTCPInodesRejectsMalformedAndAcceptsValid(t *testing.T) {
	header := "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
	row := "0: 00000000:1F90 00000000:0000 0A 0 0 00:00000000 0 0 9001\n"
	got, err := parseTCPInodes(header+row, 8080)
	if err != nil || len(got) != 1 || got[0] != "9001" {
		t.Fatalf("got %v err=%v", got, err)
	}
	for _, bad := range []string{"garbage\n", header + "bad\n"} {
		if _, err := parseTCPInodes(bad, 80); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseNativeTCPTables(t *testing.T) {
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseTCPInodes(string(data), 8080); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}
func TestFinderAllowsMissingIPv6Table(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net/tcp"), []byte("sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owners, err := NewProcessFinder(root).Find(context.Background(), process.ProcessTarget{Kind: process.TargetTCPPort, Port: 80})
	if err != nil || len(owners) != 0 {
		t.Fatalf("owners=%v err=%v", owners, err)
	}
}
func join(v []string, sep string) string {
	out := ""
	for i, s := range v {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
