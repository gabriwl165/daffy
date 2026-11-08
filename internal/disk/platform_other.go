//go:build !linux

package disk

type unsupportedPlatform struct{}

func (unsupportedPlatform) Mounts() ([]Mount, error) {
	return nil, ErrUnsupportedPlatform
}
func (unsupportedPlatform) Capacity(string) (MountCapacity, error) {
	return MountCapacity{}, ErrUnsupportedPlatform
}
