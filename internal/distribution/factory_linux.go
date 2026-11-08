//go:build linux

package distribution

import (
	"github.com/gabriwl165/daffy/internal/distribution/linux"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
	"syscall"
)

type processSignal struct{}

func (processSignal) Signal(pid int, s os.Signal) error {
	if pid <= 1 {
		return os.ErrPermission
	}
	p, e := os.FindProcess(pid)
	if e != nil {
		return e
	}
	return p.Signal(s)
}
func NewProcessController() ProcessController {
	return process.NewController(linux.NewProcessFinder("/proc"), processSignal{})
}

var _ = syscall.SIGTERM
