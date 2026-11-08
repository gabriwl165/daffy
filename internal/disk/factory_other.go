//go:build !linux && !darwin

package disk

func newScanner() Scanner { return &scanner{platform: unsupportedPlatform{}} }
