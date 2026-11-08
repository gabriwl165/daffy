//go:build darwin

package macos

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gabriwl165/daffy/internal/distribution/process"
)

func TestOwnedListenerHelper(t *testing.T) {
	if os.Getenv("DAFFY_OWNED_LISTENER_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	_, _ = fmt.Fprintln(os.Stdout, listener.Addr().(*net.TCPAddr).Port)
	<-signals
	_ = listener.Close()
	os.Exit(0)
}

type darwinOwnedChildSignaller struct{}

func (darwinOwnedChildSignaller) Signal(pid int, s os.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(s)
}

func TestDarwinFindsAndTerminatesOwnedListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedListenerHelper$")
	cmd.Env = append(os.Environ(), "DAFFY_OWNED_LISTENER_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
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
				t.Error("owned child cleanup timed out")
			}
		}
	}()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	var line string
	select {
	case line = <-ready:
	case <-ctx.Done():
		t.Fatal("listener helper readiness timed out")
	}
	port, err := strconv.Atoi(line)
	if err != nil || port < 1 {
		t.Fatalf("invalid helper port %q", line)
	}
	finder := NewProcessFinder()
	pidOwners, err := finder.Find(ctx, process.ProcessTarget{Kind: process.TargetPID, PID: cmd.Process.Pid})
	if err != nil || len(pidOwners) != 1 || pidOwners[0].Identity == "" {
		t.Fatalf("PID lookup owners=%v err=%v", pidOwners, err)
	}
	portOwners, err := finder.Find(ctx, process.ProcessTarget{Kind: process.TargetTCPPort, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, owner := range portOwners {
		if owner.PID == cmd.Process.Pid && owner.Identity == pidOwners[0].Identity {
			found = true
		}
	}
	if !found {
		t.Fatalf("owned child %d absent from port owners %v", cmd.Process.Pid, portOwners)
	}
	result, err := process.NewController(finder, darwinOwnedChildSignaller{}).Terminate(ctx, process.ProcessTarget{Kind: process.TargetTCPPort, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if result.PID != cmd.Process.Pid {
		t.Fatalf("terminated unexpected PID %d", result.PID)
	}
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatalf("child termination: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("owned child did not exit after SIGTERM")
	}
}
