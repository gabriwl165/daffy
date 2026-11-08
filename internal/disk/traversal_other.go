//go:build !linux && !darwin

package disk

import "os"

type unsupportedDirectory struct{}

func openScanRoot(string) (Directory, error) { return nil, ErrUnsupportedPlatform }
func entryMetadata(os.FileInfo) (EntryMetadata, error) {
	return EntryMetadata{}, ErrUnsupportedPlatform
}
