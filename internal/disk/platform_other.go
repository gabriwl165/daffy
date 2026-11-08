//go:build !linux && !darwin

package disk

type unsupportedPlatform struct{}

func (unsupportedPlatform) Mounts() ([]Mount, error) {
	return nil, ErrUnsupportedPlatform
}
func (unsupportedPlatform) Capacity(string) (MountCapacity, error) {
	return MountCapacity{}, ErrUnsupportedPlatform
}
