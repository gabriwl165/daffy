//go:build darwin

package distribution

import (
	"github.com/gabriwl165/daffy/internal/distribution/macos"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
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
	return process.NewController(macos.NewProcessFinder(), processSignal{})
}
