//go:build !linux

package disk

import "errors"

type unsupportedPlatform struct{}

func newScanner() Scanner { return &scanner{platform: unsupportedPlatform{}} }
func (unsupportedPlatform) Mounts() ([]Mount, error) {
	return nil, errors.New("disk profiling is unsupported on this operating system")
}
func (unsupportedPlatform) Capacity(string) (MountCapacity, error) {
	return MountCapacity{}, errors.New("disk capacity is unsupported on this operating system")
}
