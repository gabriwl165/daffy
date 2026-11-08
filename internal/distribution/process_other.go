//go:build !linux && !darwin

package distribution

import (
	"context"
	"errors"
)

type unsupportedController struct{}

func NewProcessController() ProcessController { return unsupportedController{} }
func (unsupportedController) Find(context.Context, ProcessTarget) ([]int, error) {
	return nil, errors.New("process control is unsupported on this platform")
}
func (unsupportedController) Terminate(context.Context, ProcessTarget) (ProcessResult, error) {
	return ProcessResult{}, errors.New("process control is unsupported on this platform")
}
